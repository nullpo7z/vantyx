package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

// setupVNCSharingFixtures starts a VNC session owned by admin on a target
// that both admin and bob can access, so invitations can be issued to bob.
func setupVNCSharingFixtures(t *testing.T) (*App, session.ID) {
	t.Helper()
	t.Setenv("VANTYX_RECORDINGS_DIR", t.TempDir())
	app := newTestAppForVNC(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("vnc1"), "VNC Host", "127.0.0.1", 5900, access.ProtocolVNC, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("vnc1"))
	_, _ = app.UserStore.CreateUser("bob", "bob", "Bob12345!", "")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("bob"), access.GroupID("g1"))

	sid := session.ID("vnc-audit-test")
	_, err := app.VNCSessionManager.Start(sid, session.StartOptions{
		UserID: "admin", TargetID: "vnc1", TargetName: "VNC Host",
	}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { app.VNCSessionManager.Stop(sid) })
	return app, sid
}

func postVNCInvitation(t *testing.T, app *App, sid session.ID, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPost, "/api/vnc/sessions/"+string(sid)+"/invitations", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestVNCCreateInvitation_AuditsNamedInvite is the E-12 regression guard
// for the shared VNC/RDP invitation path, which previously emitted no
// audit event at all (the terminal path already did).
func TestVNCCreateInvitation_AuditsNamedInvite(t *testing.T) {
	app, sid := setupVNCSharingFixtures(t)
	w := postVNCInvitation(t, app, sid, `{"mode":"viewer","invitee_user_id":"bob"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create invitation: status=%d body=%s", w.Code, w.Body.String())
	}

	e, ok := latestAuditEvent("session_invitation_created")
	if !ok {
		t.Fatal("expected a session_invitation_created audit entry for a VNC invitation")
	}
	want := map[string]interface{}{
		"user_id":    "admin",
		"session_id": string(sid),
		"target_id":  "vnc1",
		"kind":       "vnc",
		"invitee":    "bob",
		"is_link":    false,
	}
	for k, v := range want {
		if got := e.Fields[k]; got != v {
			t.Errorf("session_invitation_created %s = %v, want %v", k, got, v)
		}
	}
	if got, _ := e.Fields["inv_id"].(string); got == "" {
		t.Errorf("session_invitation_created inv_id missing: %v", e.Fields)
	}
}

// TestVNCCreateInvitation_AuditsLinkInvite covers the shareable-link
// branch of the same path (no invitee, is_link=true).
func TestVNCCreateInvitation_AuditsLinkInvite(t *testing.T) {
	app, sid := setupVNCSharingFixtures(t)
	w := postVNCInvitation(t, app, sid, `{"mode":"viewer"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create link invitation: status=%d body=%s", w.Code, w.Body.String())
	}

	e, ok := latestAuditEvent("session_invitation_created")
	if !ok {
		t.Fatal("expected a session_invitation_created audit entry for a VNC link invitation")
	}
	if got := e.Fields["is_link"]; got != true {
		t.Errorf("is_link = %v, want true", got)
	}
	if got := e.Fields["kind"]; got != "vnc" {
		t.Errorf("kind = %v, want vnc", got)
	}
	if got := e.Fields["invitee"]; got != "" {
		t.Errorf("invitee = %v, want empty for a link invitation", got)
	}
}
