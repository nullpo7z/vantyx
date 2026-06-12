package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

type mockBridgeController struct {
	detached []string
	writer   string
}

func (m *mockBridgeController) SetWriter(userID string) { m.writer = userID }
func (m *mockBridgeController) DetachUser(userID string) {
	m.detached = append(m.detached, userID)
}

func setupSharingTerminalFixtures(t *testing.T) (*App, session.ID, string, string) {
	t.Helper()
	app := newTestAppForTerminal(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.UserStore.CreateUser("bob", "bob", "Bob12345!", "")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("bob"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	mgr, ok := app.TerminalSessionManager.(*session.Manager)
	if !ok {
		t.Fatalf("TerminalSessionManager is not *session.Manager")
	}
	sid := session.ID("sharing-test-session")
	_, err := mgr.Start(sid, session.StartOptions{
		UserID: "admin", TargetID: "demo", TargetName: "Demo",
	}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(sid) })
	app.SharingRegistry.EnsureRoom(string(sid), "demo", "admin", "admin")
	return app, sid, "admin", "bob"
}

func TestHandleKickParticipant_DetachesBridge(t *testing.T) {
	app, sid, _, bob := setupSharingTerminalFixtures(t)
	mock := &mockBridgeController{}
	app.SharingBridges.Register(sid, mock)
	app.SharingRegistry.EnsureRoom(string(sid), "demo", "admin", "admin").AddViewer(bob, "bob", "inv-1", time.Now().UTC())

	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/terminal/sessions/"+string(sid)+"/participants/"+bob, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("kick: status=%d body=%s", w.Code, w.Body.String())
	}
	if len(mock.detached) != 1 || mock.detached[0] != bob {
		t.Fatalf("DetachUser not called for bob: %+v", mock.detached)
	}
	room, ok := app.SharingRegistry.Get(string(sid))
	if !ok || room.IsParticipant(bob) {
		t.Fatalf("bob should be removed from room")
	}
}

func TestHandleJoinSession_AddsParticipant(t *testing.T) {
	app, sid, _, bob := setupSharingTerminalFixtures(t)
	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")

	createBody, _ := json.Marshal(map[string]interface{}{
		"mode":            "viewer",
		"invitee_user_id": bob,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/sessions/"+string(sid)+"/invitations", bytes.NewReader(createBody))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create invitation: status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	bobSess, _ := app.SessionStore.Create("bob")
	joinBody, _ := json.Marshal(map[string]string{"invitation_id": created.ID})
	req = httptest.NewRequest(http.MethodPost, "/api/terminal/sessions/"+string(sid)+"/join", bytes.NewReader(joinBody))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: bobSess.ID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("join: status=%d body=%s", w.Code, w.Body.String())
	}
	room, _ := app.SharingRegistry.Get(string(sid))
	if !room.IsParticipant(bob) {
		t.Fatalf("bob not in room after join")
	}
}

func TestRegistry_Remove(t *testing.T) {
	app, sid, _, _ := setupSharingTerminalFixtures(t)
	app.SharingRegistry.Remove(string(sid))
	if _, ok := app.SharingRegistry.Get(string(sid)); ok {
		t.Fatalf("room should be removed")
	}
}

func TestHandleCreateInvitation_GroupInviteOwnerNotInGroup(t *testing.T) {
	app, sid, _, _ := setupSharingTerminalFixtures(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g2"), access.TargetID("demo"))

	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	body, _ := json.Marshal(map[string]interface{}{
		"mode":            "viewer",
		"invite_group_id": "g2",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/terminal/sessions/"+string(sid)+"/invitations", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("group invite without membership: status=%d body=%s", w.Code, w.Body.String())
	}
}
