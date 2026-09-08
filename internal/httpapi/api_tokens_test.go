package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
)

func TestAPITokens_Lifecycle(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	seedAdminDemoSSHTarget(t, app)
	adminSess, _ := app.SessionStore.Create("admin")

	bearer := func(method, path, token string, body interface{}) *httptest.ResponseRecorder {
		req := jsonReq(t, method, path, body, "")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Create (session only): read token + write token; bad inputs.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/tokens", map[string]interface{}{"name": "ci", "scope": "read", "expires_in_days": 30}, adminSess.ID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decodeJSON(t, w)
	readTok, _ := created["token"].(string)
	readID, _ := created["id"].(string)
	if !strings.HasPrefix(readTok, auth.APITokenPrefix) || created["prefix"] != readTok[:12] || created["expires_at"] == nil {
		t.Fatalf("created = %v", created)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/tokens", map[string]interface{}{"name": "deploy", "scope": "write"}, adminSess.ID))
	writeTok, _ := decodeJSON(t, w)["token"].(string)
	for _, bad := range []map[string]interface{}{{"name": "", "scope": "read"}, {"name": "x", "scope": "root"}, {"name": "x", "scope": "read", "expires_in_days": 99999}} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/tokens", bad, adminSess.ID))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad create %v: %d", bad, w.Code)
		}
	}
	// Listing never returns the plain token.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me/tokens", nil, adminSess.ID))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), readTok) || !strings.Contains(w.Body.String(), `"name":"ci"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}

	// Bearer auth works for reads; the token identity is the owner.
	w = bearer(http.MethodGet, "/api/me", readTok, nil)
	if w.Code != http.StatusOK || decodeJSON(t, w)["user_id"] != "admin" {
		t.Fatalf("bearer /api/me: %d %s", w.Code, w.Body.String())
	}
	w = bearer(http.MethodGet, "/api/targets", readTok, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"demo"`) {
		t.Fatalf("bearer targets: %d %s", w.Code, w.Body.String())
	}
	// Read-only token cannot mutate; write token can (and bypasses CSRF:
	// no Origin header is sent here).
	w = bearer(http.MethodPost, "/api/groups", readTok, map[string]string{"name": "viaToken"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("read token mutation: %d %s", w.Code, w.Body.String())
	}
	w = bearer(http.MethodPost, "/api/groups", writeTok, map[string]string{"name": "viaToken"})
	if w.Code != http.StatusCreated {
		t.Fatalf("write token create group: %d %s", w.Code, w.Body.String())
	}
	if _, err := app.AccessGroupStore.Get(ctx, access.GroupID("viatoken")); err != nil {
		t.Fatalf("group not created via token: %v", err)
	}
	// Tokens cannot manage tokens, passwords, TOTP, or log in/out.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/me/tokens"},
		{http.MethodPost, "/api/me/tokens"},
		{http.MethodPost, "/api/me/password"},
		{http.MethodPost, "/api/me/totp/setup"},
		{http.MethodPost, "/api/logout"},
	} {
		if w := bearer(tc.method, tc.path, writeTok, map[string]string{}); w.Code != http.StatusForbidden {
			t.Fatalf("%s %s with token: %d, want 403", tc.method, tc.path, w.Code)
		}
	}
	// Garbage / unknown tokens are 401 and audited; last_used is recorded.
	if w := bearer(http.MethodGet, "/api/me", "vtx_doesnotexist000000000000", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me/tokens", nil, adminSess.ID))
	if !strings.Contains(w.Body.String(), `"last_used_at":"`) {
		t.Fatalf("last_used_at missing: %s", w.Body.String())
	}

	// Revoke: token stops working immediately; revoking twice is 404.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/me/tokens/"+readID, nil, adminSess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", w.Code)
	}
	if w := bearer(http.MethodGet, "/api/me", readTok, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token still works: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/me/tokens/"+readID, nil, adminSess.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("revoke twice: %d", w.Code)
	}

	// Expiry: a token expiring in the past is rejected.
	past := time.Now().Add(-time.Minute)
	_, expiredTok, err := app.APITokens.Create(ctx, "admin", "old", auth.APITokenScopeRead, &past)
	if err != nil {
		t.Fatal(err)
	}
	if w := bearer(http.MethodGet, "/api/me", expiredTok, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: %d", w.Code)
	}

	// Admin oversight: list another user's tokens and revoke one.
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	bobTok, bobPlain, _ := app.APITokens.Create(ctx, "bob", "bobs", auth.APITokenScopeWrite, nil)
	bobSess, _ := app.SessionStore.Create("bob")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/users/bob/tokens", nil, bobSess.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin listing another user's tokens: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/users/bob/tokens", nil, adminSess.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"bobs"`) {
		t.Fatalf("admin list: %d %s", w.Code, w.Body.String())
	}
	// Bob's write token cannot reach admin endpoints (role still applies).
	if w := bearer(http.MethodGet, "/api/users", bobPlain, nil); w.Code != http.StatusForbidden {
		t.Fatalf("user token on admin endpoint: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/users/bob/tokens/"+bobTok.ID, nil, adminSess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin revoke: %d", w.Code)
	}
	if w := bearer(http.MethodGet, "/api/me", bobPlain, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("admin-revoked token still works: %d", w.Code)
	}
}

// A token must never reach anything that mints or changes credentials:
// those stay session-only whatever the scope, and user administration /
// stored SSH keys are read-only for tokens.
func TestAPITokens_CredentialEndpointsAreSessionOnly(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/me/tokens", map[string]interface{}{"name": "deploy", "scope": "write"}, adminSess.ID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	writeTok, _ := decodeJSON(t, w)["token"].(string)
	bearer := func(method, path string, body interface{}) int {
		req := jsonReq(t, method, path, body, "")
		req.Header.Set("Authorization", "Bearer "+writeTok)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	denied := []struct{ method, path string }{
		{http.MethodPost, "/api/me/ssh-keys"},
		{http.MethodGet, "/api/me/ssh-keys"},
		{http.MethodGet, "/api/settings/backups"},
		{http.MethodPost, "/api/settings/backups"},
		{http.MethodGet, "/api/settings/backups/vantyx-20260101-000000.db"},
		{http.MethodPut, "/api/settings/webhooks"},
		{http.MethodGet, "/api/settings/webhooks"},
		{http.MethodPost, "/api/users"},
		{http.MethodPatch, "/api/users/admin"},
		{http.MethodDelete, "/api/users/admin/totp"},
		{http.MethodPost, "/api/users/admin/ssh-keys"},
		{http.MethodPost, "/api/ssh-keys"},
		{http.MethodDelete, "/api/users/admin/tokens/x"},
	}
	for _, d := range denied {
		if code := bearer(d.method, d.path, map[string]interface{}{}); code != http.StatusForbidden {
			t.Errorf("%s %s with write token: %d, want 403", d.method, d.path, code)
		}
	}
	// Reads of user administration and ordinary APIs stay available.
	for _, p := range []string{"/api/users", "/api/users/admin/tokens", "/api/ssh-keys", "/api/me", "/api/targets"} {
		if code := bearer(http.MethodGet, p, nil); code != http.StatusOK {
			t.Errorf("GET %s with write token: %d, want 200", p, code)
		}
	}
	if !apiTokenPathAllowed(http.MethodPost, "/api/access-requests") || apiTokenPathAllowed(http.MethodGet, "/api/me/tokens") || apiTokenPathAllowed(http.MethodPost, "/api/usersx") == false {
		t.Fatal("apiTokenPathAllowed prefix rules")
	}
}
