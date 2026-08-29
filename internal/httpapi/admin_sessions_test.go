package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

func TestApp_AdminSessionsListWatchTerminate(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	seedAdminDemoSSHTarget(t, app)
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	_ = app.AccessGroupStore.AddUserToGroup(ctx, "bob", "g1")

	mgr, ok := app.TerminalSessionManager.(*session.Manager)
	if !ok {
		t.Skip("terminal session manager is not *session.Manager")
	}
	sid := session.ID("adm-term-1")
	if _, err := mgr.Start(sid, session.StartOptions{UserID: "bob", TargetID: "demo", TargetName: "Demo host", Name: "bob shell"}, func(ctx context.Context, _ *session.Session) {
		<-ctx.Done()
	}); err != nil {
		t.Fatalf("start terminal session: %v", err)
	}
	vid := session.ID("adm-vnc-1")
	if _, err := app.VNCSessionManager.Start(vid, session.StartOptions{UserID: "bob", TargetID: "demo", TargetName: "Demo host"}, func(ctx context.Context, _ *session.Session) {
		<-ctx.Done()
	}); err != nil {
		t.Fatalf("start vnc session: %v", err)
	}
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create("bob")

	// Non-admins are refused everywhere.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/sessions"},
		{http.MethodPost, "/api/admin/sessions/terminal/adm-term-1/watch"},
		{http.MethodDelete, "/api/admin/sessions/terminal/adm-term-1"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, tc.method, tc.path, nil, bobSess.ID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s as bob: %d", tc.method, tc.path, w.Code)
		}
	}

	// List shows both sessions with owner and target details.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/admin/sessions", nil, adminSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"kind":"terminal"`, `"session_id":"adm-term-1"`, `"owner_username":"bob"`, `"kind":"vnc"`, `"session_id":"adm-vnc-1"`, `"target_name":"Demo host"`, `"watching":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("list lacks %s: %s", want, body)
		}
	}

	// Watch: admin becomes a viewer in the room; url points at the viewer page.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/admin/sessions/terminal/adm-term-1/watch", nil, adminSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("watch: %d %s", w.Code, w.Body.String())
	}
	res := decodeJSON(t, w)
	if u, _ := res["url"].(string); !strings.HasPrefix(u, "/terminal?") || !strings.Contains(u, "mode=viewer") || !strings.Contains(u, "session_id=adm-term-1") {
		t.Fatalf("watch url = %v", res["url"])
	}
	room, ok := app.SharingRegistry.Get("adm-term-1")
	if !ok || !room.IsParticipant("admin") {
		t.Fatal("admin is not a participant after watch")
	}
	// The attach path accepts the admin as a viewer.
	if _, _, err := app.terminalSessionByOwnerOrParticipant(ctx, "adm-term-1", "admin", true); err != nil {
		t.Fatalf("viewer attach check: %v", err)
	}
	// Idempotent, and the list now flags "watching".
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/admin/sessions/terminal/adm-term-1/watch", nil, adminSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("watch twice: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/admin/sessions", nil, adminSess.ID))
	if !strings.Contains(w.Body.String(), `"watching":true`) {
		t.Fatalf("watching flag missing: %s", w.Body.String())
	}
	if w := httptest.NewRecorder(); true {
		router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/admin/sessions/vnc/adm-vnc-1/watch", nil, adminSess.ID))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"/vnc?`) {
			t.Fatalf("vnc watch: %d %s", w.Code, w.Body.String())
		}
	}

	// Unknown session / kind -> 404.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/admin/sessions/terminal/nope", nil, adminSess.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("terminate unknown: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/admin/sessions/bogus/adm-term-1", nil, adminSess.ID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("terminate bogus kind: %d", w.Code)
	}

	// Terminate: session gone, room removed.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/admin/sessions/terminal/adm-term-1", map[string]string{"reason": "policy"}, adminSess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("terminate: %d %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, still := mgr.Get(sid); !still || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, still := mgr.Get(sid); still {
		t.Fatal("terminal session still registered after terminate")
	}
	if _, ok := app.SharingRegistry.Get("adm-term-1"); ok {
		t.Fatal("room still registered after terminate")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/admin/sessions/vnc/adm-vnc-1", nil, adminSess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("terminate vnc: %d", w.Code)
	}
	_ = access.GroupID("g1")
}
