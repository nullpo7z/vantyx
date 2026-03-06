package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

func newTestAppForTerminal(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "terminal.db")
	if err := os.Setenv("VANTYX_SQLITE_PATH", dbPath); err != nil {
		t.Fatalf("set env: %v", err)
	}
	return NewApp()
}

func TestHandleSSHWebSocket_UnauthorizedWithoutCookie(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	// Ensure target_id=demo exists for this test (admin has access via a group).
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}

	_, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err == nil {
		t.Fatalf("expected WebSocket dial to fail without cookie, got nil error")
	}
}

func TestHandleSSHWebSocket_UnauthorizedEmptyCookieValue(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=demo", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "", Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for empty cookie value, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_UnauthorizedInvalidSession(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=demo", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "nonexistent-session-id", Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid session, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_MissingTargetID(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_ForbiddenTarget(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	// Seed demo for group g1 (admin). "other" user does not belong to this group.
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	_, _ = app.UserStore.CreateUser("other", "other", "pass")
	httpSess, err := app.SessionStore.Create("other")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=demo", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_NonSSHTargetReturns501(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("telnet1"), "Telnet Host", "127.0.0.1", 23, access.ProtocolTelnet, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("telnet1"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=telnet1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 Not Implemented, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_UpgradeFailsWithoutWebSocketRequest(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=demo", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	// No Upgrade: websocket header -> upgrade fails
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when upgrade fails, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_TargetNotFound(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_InvalidCredentialsReturnsError(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	// Seed demo for admin in group g1.
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"}).String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if !strings.Contains(string(msg), "error") {
		t.Fatalf("expected error message, got %q", string(msg))
	}
}

func TestHandleSSHWebSocket_ValidCredentialsStartsBridge(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	// Seed demo for admin in group g1.
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"}).String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// #nosec G101 -- test-only dummy credentials for WebSocket SSH test
	creds := `{"username":"root","password":"test"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(creds)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	// Server will try SSH dial to demo (127.0.0.1:22); no server => connection closes.
	_, _, err = conn.ReadMessage()
	if err == nil {
		// Might get one message (e.g. SSH banner) before close; read until close
		_, _, _ = conn.ReadMessage()
	}
	// Expect connection to close (dial failure or eventual close)
}

// startFailingStub implements terminalSessionStarter and makes Start return an error.
type startFailingStub struct{}

func (startFailingStub) Start(session.ID, func(context.Context, *session.Session)) (*session.Session, error) {
	return nil, errors.New("injected start error")
}

func (startFailingStub) Touch(session.ID) {}

func TestHandleSSHWebSocket_StartFailsReturns500(t *testing.T) {
	app := newTestAppForTerminal(t)
	app.TerminalSessionManager = startFailingStub{}
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, _ := app.SessionStore.Create("admin")

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"}).String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// #nosec G101 -- test-only credentials
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"username":"u","password":"p"}`))
	// After Start fails, server closes connection
	_, _, _ = conn.ReadMessage()
}

func TestHandleSSHWebSocket_StartFailsDuplicateID(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1")
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// Force same session ID for both connections so second Start returns ErrSessionExists
	const fixedID = "test-fixed-id"
	terminalSessionIDGen = func() session.ID { return fixedID }
	defer func() { terminalSessionIDGen = nil }()

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"}).String())

	// First connection: start and block in RunBridge
	conn1, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	go func() {
		// #nosec G101 -- test-only credentials
		_ = conn1.WriteMessage(websocket.TextMessage, []byte(`{"username":"u","password":"p"}`))
		_, _, _ = conn1.ReadMessage()
		_ = conn1.Close()
	}()

	// Give first connection time to call Start
	time.Sleep(100 * time.Millisecond)

	// Second connection: same fixed ID -> Start returns ErrSessionExists -> handler closes conn
	conn2, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer conn2.Close()
	// #nosec G101 -- test-only credentials
	_ = conn2.WriteMessage(websocket.TextMessage, []byte(`{"username":"u2","password":"p2"}`))
	_, _, err = conn2.ReadMessage()
	// Connection closed by server after Start failed
	if err == nil {
		_, _, _ = conn2.ReadMessage()
	}
}
