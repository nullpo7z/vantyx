package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/session"
)

// The owner can pin/unpin their session; a non-owner cannot; the pin is
// reflected in the session listing and suppresses the idle flag.
func TestSessionKeep_OwnerOnlyAndSuppressesIdle(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	mgr, ok := app.TerminalSessionManager.(*session.Manager)
	if !ok {
		t.Skip("terminal manager is not *session.Manager")
	}
	mgr.SetIdleWarnAfter(time.Minute)
	if _, err := mgr.Start("s1", session.StartOptions{UserID: "admin"}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() }); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer mgr.Stop("s1")
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create("bob")

	keep := func(sessID string, body map[string]interface{}) int {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPut, "/api/terminal/sessions/s1/keep", body, sessID))
		return w.Code
	}
	// A non-owner, non-admin cannot pin it.
	if code := keep(bobSess.ID, map[string]interface{}{"keep": true}); code != http.StatusNotFound {
		t.Fatalf("bob keep: %d, want 404", code)
	}
	if s, _ := mgr.Get("s1"); s != nil && s.Keep() {
		t.Fatal("session pinned by a non-owner")
	}
	// The owner pins it.
	if code := keep(adminSess.ID, map[string]interface{}{"keep": true}); code != http.StatusOK {
		t.Fatalf("owner keep: %d, want 200", code)
	}
	s, _ := mgr.Get("s1")
	if s == nil || !s.Keep() || mgr.IsIdle(s) {
		t.Fatalf("expected pinned, non-idle session: keep=%v", s != nil && s.Keep())
	}
	// Unpin.
	if code := keep(adminSess.ID, map[string]interface{}{"keep": false}); code != http.StatusOK {
		t.Fatalf("owner unpin: %d", code)
	}
	if s, _ := mgr.Get("s1"); s == nil || s.Keep() {
		t.Fatal("expected unpinned session")
	}
}
