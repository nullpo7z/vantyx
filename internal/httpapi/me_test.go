package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

// GET /api/me carries the tags and (inherited) groups the account page shows.
func TestApp_MeIncludesTagsAndGroups(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("net"), "Network"); err != nil {
		t.Fatalf("create net: %v", err)
	}
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("net/tokyo"), "Tokyo"); err != nil {
		t.Fatalf("create net/tokyo: %v", err)
	}
	if err := app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("bob"), access.GroupID("net")); err != nil {
		t.Fatalf("add bob: %v", err)
	}
	if err := app.UserStore.SetUserTags("bob", []string{"ops"}); err != nil {
		t.Fatalf("tags: %v", err)
	}
	sess, _ := app.SessionStore.Create("bob")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me", nil, sess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("/api/me: %d %s", w.Code, w.Body.String())
	}
	me := decodeJSON(t, w)
	tags, _ := me["tags"].([]interface{})
	if len(tags) != 1 || tags[0] != "ops" {
		t.Fatalf("tags = %v", me["tags"])
	}
	groups, _ := me["groups"].([]interface{})
	got := map[string]string{}
	for _, g := range groups {
		m := g.(map[string]interface{})
		got[m["id"].(string)] = m["name"].(string)
	}
	if got["net"] != "Network" || got["net/tokyo"] != "Tokyo" || len(got) != 2 {
		t.Fatalf("groups = %v, want net and net/tokyo (inherited)", got)
	}
}
