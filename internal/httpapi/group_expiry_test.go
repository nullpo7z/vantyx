package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
)

func TestApp_GroupMemberExpiry(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("net"), "net"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.TargetStore.CreateWithPath(ctx, "t1", "t1", "127.0.0.1", 22, access.ProtocolSSH, "net", "net", "", "", "", "", true, false, false); err != nil {
		t.Fatal(err)
	}
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, "net", "t1")
	sess, _ := app.SessionStore.Create("admin")

	post := func(body map[string]string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/groups/net/members", body, sess.ID))
		return w
	}
	if w := post(map[string]string{"user_id": "bob", "expires_at": "yesterday"}); w.Code != http.StatusBadRequest {
		t.Fatalf("bad timestamp: %d", w.Code)
	}
	if w := post(map[string]string{"user_id": "bob", "expires_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}); w.Code != http.StatusBadRequest {
		t.Fatalf("past expiry: %d", w.Code)
	}
	until := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if w := post(map[string]string{"user_id": "bob", "expires_at": until.Format(time.RFC3339)}); w.Code != http.StatusNoContent {
		t.Fatalf("add with expiry: %d %s", w.Code, w.Body.String())
	}
	if ok, _ := app.userCanAccessTarget(ctx, "bob", "t1"); !ok {
		t.Fatal("bob should reach t1 before expiry")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/groups/net/members", nil, sess.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"expires_at":"`) || !strings.Contains(w.Body.String(), `"expired":false`) {
		t.Fatalf("members: %d %s", w.Code, w.Body.String())
	}

	// Expire it directly in the store and confirm access is gone while
	// the listing still shows the (expired) row.
	past := time.Now().Add(-time.Minute)
	_ = app.AccessGroupStore.AddUserToGroupUntil(ctx, "bob", "net", &past)
	if ok, _ := app.userCanAccessTarget(ctx, "bob", "t1"); ok {
		t.Fatal("bob still reaches t1 after expiry")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/groups/net/members", nil, sess.ID))
	if !strings.Contains(w.Body.String(), `"expired":true`) {
		t.Fatalf("expired row missing: %s", w.Body.String())
	}
	// /api/me for bob no longer lists the group.
	bobSess, _ := app.SessionStore.Create("bob")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me", nil, bobSess.ID))
	if strings.Contains(w.Body.String(), `"id":"net"`) {
		t.Fatalf("expired group still in /api/me: %s", w.Body.String())
	}
}
