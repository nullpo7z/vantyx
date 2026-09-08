package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/auth"
)

func TestApp_RenameAndDisableUser(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	for _, u := range []string{"bob", "carol"} {
		if _, err := app.UserStore.CreateUser(u, u, "Password1!", "user"); err != nil {
			t.Fatal(err)
		}
	}
	adminSess, _ := app.SessionStore.Create("admin")
	patch := func(target string, body map[string]interface{}) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPatch, "/api/users/"+target, body, adminSess.ID))
		return w
	}

	// Rename: validation, conflict, success (id unchanged, login by new name).
	if w := patch("bob", map[string]interface{}{"username": ""}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty username: %d", w.Code)
	}
	if w := patch("bob", map[string]interface{}{"username": "carol"}); w.Code != http.StatusConflict {
		t.Fatalf("duplicate username: %d", w.Code)
	}
	w := patch("bob", map[string]interface{}{"username": "robert"})
	if w.Code != http.StatusOK || decodeJSON(t, w)["username"] != "robert" || decodeJSON(t, w)["id"] != "bob" {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "robert", "password": "Password1!"}, ""))
	if w.Code != http.StatusOK || sessionCookie(w) == "" {
		t.Fatalf("login with new name: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "bob", "password": "Password1!"}, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("login with old name: %d", w.Code)
	}

	// Disable: guards.
	if w := patch("admin", map[string]interface{}{"disabled": true}); w.Code != http.StatusBadRequest {
		t.Fatalf("disable self: %d", w.Code)
	}
	if _, err := app.UserStore.CreateUser("admin2", "admin2", "Password1!", "admin"); err != nil {
		t.Fatal(err)
	}
	if w := patch("admin2", map[string]interface{}{"disabled": true}); w.Code != http.StatusOK {
		t.Fatalf("disable second admin: %d %s", w.Code, w.Body.String())
	}
	// admin is now the only enabled admin: cannot be demoted either.
	admin2Sess, _ := app.SessionStore.Create("admin2")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me", nil, admin2Sess.ID))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled admin's session still valid: %d", w.Code)
	}

	// Disable bob: session, API token, public-key and password logins all stop.
	bobSess, _ := app.SessionStore.Create("bob")
	_, bobToken, _ := app.APITokens.Create(ctx, "bob", "t", auth.APITokenScopeRead, nil)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	if _, err := app.UserStore.AddPublicKey("bob", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))); err != nil {
		t.Fatal(err)
	}
	w = patch("bob", map[string]interface{}{"disabled": true})
	if w.Code != http.StatusOK || decodeJSON(t, w)["disabled"] != true {
		t.Fatalf("disable bob: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me", nil, bobSess.ID))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user's session: %d", w.Code)
	}
	req := jsonReq(t, http.MethodGet, "/api/me", nil, "")
	req.Header.Set("Authorization", "Bearer "+bobToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user's API token: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "robert", "password": "Password1!"}, ""))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "disabled") {
		t.Fatalf("disabled login: %d %s", w.Code, w.Body.String())
	}
	if _, err := app.UserStore.AuthenticateByPublicKey("robert", sshPub); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("public key auth for disabled user: %v", err)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/users", nil, adminSess.ID))
	if !strings.Contains(w.Body.String(), `"id":"bob"`) || !strings.Contains(w.Body.String(), `"disabled":true`) {
		t.Fatalf("list lacks disabled flag: %s", w.Body.String())
	}

	// Re-enable: logins work again.
	if w := patch("bob", map[string]interface{}{"disabled": false}); w.Code != http.StatusOK {
		t.Fatalf("enable: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "robert", "password": "Password1!"}, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("login after enable: %d %s", w.Code, w.Body.String())
	}
	if _, err := app.UserStore.AuthenticateByPublicKey("robert", sshPub); err != nil {
		t.Fatalf("public key auth after enable: %v", err)
	}
}
