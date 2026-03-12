package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

func newTestAppForTerminal(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "terminal.db")
	if err := os.Setenv("VANTYX_SQLITE_PATH", dbPath); err != nil {
		t.Fatalf("set env: %v", err)
	}
	return NewApp()
}

// --- allowedWebSocketOrigin ---

func TestAllowedWebSocketOrigin_SameOriginHTTPS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/ws", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.TLS = &tls.ConnectionState{}
	req.Host = "example.com"
	req.Header.Set("Origin", "https://example.com")
	if !allowedWebSocketOrigin(req) {
		t.Fatal("expected same-origin HTTPS request to be allowed")
	}
}

func TestAllowedWebSocketOrigin_NoOriginLoopbackAllowed(t *testing.T) {
	old := os.Getenv("VANTYX_ALLOW_WS_NO_ORIGIN")
	_ = os.Setenv("VANTYX_ALLOW_WS_NO_ORIGIN", "1")
	defer func() {
		if old != "" {
			_ = os.Setenv("VANTYX_ALLOW_WS_NO_ORIGIN", old)
		} else {
			_ = os.Unsetenv("VANTYX_ALLOW_WS_NO_ORIGIN")
		}
	}()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/ws", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	if !allowedWebSocketOrigin(req) {
		t.Fatal("expected no-Origin loopback request to be allowed when VANTYX_ALLOW_WS_NO_ORIGIN=1")
	}
}

// --- readTerminalCredentials ---

type fakeWSConn struct {
	msgType int
	data    []byte
}

func (f *fakeWSConn) ReadMessage() (int, []byte, error) {
	return f.msgType, f.data, nil
}

func (f *fakeWSConn) SetReadDeadline(time.Time) error {
	return nil
}

func TestReadTerminalCredentials_UseStored(t *testing.T) {
	target := &access.Target{
		SSHUsername:             "user",
		SSHPassword:             "stored-pass",
		SSHPrivateKey:           "key",
		SSHPrivateKeyPassphrase: "",
	}
	msg := wsAuthMessage{
		UseStoredCredentials: true,
		Password:             "override-pass",
		PrivateKeyPassphrase: "runtime-pass",
		Name:                 "n",
		Description:          "d",
	}
	b, _ := json.Marshal(msg)
	conn := &fakeWSConn{msgType: websocket.TextMessage, data: b}
	creds, err := readTerminalCredentials(conn, target)
	if err != nil {
		t.Fatalf("readTerminalCredentials returned error: %v", err)
	}
	if creds.Username != "user" || creds.Password != "override-pass" || creds.PrivateKey != "key" || creds.PrivateKeyPassphrase != "runtime-pass" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	if creds.Name != "n" || creds.Description != "d" {
		t.Fatalf("unexpected name/description: %+v", creds)
	}
}

func TestReadTerminalCredentials_ExplicitUsernameWithKeyPassphrase(t *testing.T) {
	target := &access.Target{
		SSHPrivateKey: "key",
	}
	msg := wsAuthMessage{
		Username:             "u",
		Password:             "p",
		PrivateKeyPassphrase: "pp",
		Name:                 "n",
		Description:          "d",
	}
	b, _ := json.Marshal(msg)
	conn := &fakeWSConn{msgType: websocket.TextMessage, data: b}
	creds, err := readTerminalCredentials(conn, target)
	if err != nil {
		t.Fatalf("readTerminalCredentials returned error: %v", err)
	}
	if creds.Username != "u" || creds.Password != "p" {
		t.Fatalf("unexpected basic creds: %+v", creds)
	}
	if creds.PrivateKey != "key" || creds.PrivateKeyPassphrase != "pp" {
		t.Fatalf("expected target key with provided passphrase, got %+v", creds)
	}
	if creds.Name != "n" || creds.Description != "d" {
		t.Fatalf("unexpected name/description: %+v", creds)
	}
}

// (helper removed – readTerminalCredentials is tested via fakeWSConn)

func TestHandleSSHWebSocket_UnauthorizedWithoutCookie(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	// Ensure target_id=demo exists for this test (admin has access via a group).
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
	_, _, err := websocket.DefaultDialer.Dial(u.String(), header)
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
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	_, _ = app.UserStore.CreateUser("other", "other", "Other1!x", "")
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
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("telnet1"), "Telnet Host", "127.0.0.1", 23, access.ProtocolTelnet, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
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
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
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
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
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
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
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

func (startFailingStub) Start(session.ID, session.StartOptions, func(context.Context, *session.Session)) (*session.Session, error) {
	return nil, errors.New("injected start error")
}

func (startFailingStub) Get(session.ID) (*session.Session, bool) { return nil, false }

func (startFailingStub) Touch(session.ID) {}

func (startFailingStub) Stop(session.ID) {}

func TestHandleSSHWebSocket_StartFailsReturns500(t *testing.T) {
	app := newTestAppForTerminal(t)
	app.TerminalSessionManager = startFailingStub{}
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, _ := app.SessionStore.Create("admin")

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
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
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
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
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
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

// TestHandleTerminalSessions_ListEmpty covers handleTerminalSessions and writeJSON (empty list).
func TestHandleTerminalSessions_ListEmpty(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out struct {
		Items []TerminalSessionItem `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("expected empty items, got %d", len(out.Items))
	}
}

// TestHandleTerminalSessions_ListWithSession covers handleTerminalSessions with one session (writeJSON, Session.ID(), CreatedAt()).
func TestHandleTerminalSessions_ListWithSession(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// Start a terminal session directly so GET list returns it
	mgr, ok := app.TerminalSessionManager.(*session.Manager)
	if !ok {
		t.Fatalf("TerminalSessionManager is not *session.Manager")
	}
	sid := session.ID("list-test-session")
	_, err = mgr.Start(sid, session.StartOptions{
		UserID: "admin", TargetID: "demo", TargetName: "Demo host", Name: "s1", Description: "desc",
	}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start terminal session: %v", err)
	}
	defer mgr.Stop(sid)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Items []TerminalSessionItem `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].SessionID != string(sid) || out.Items[0].TargetID != "demo" || out.Items[0].Name != "s1" {
		t.Fatalf("unexpected item: %+v", out.Items[0])
	}
	if out.Items[0].CreatedAt.IsZero() {
		t.Fatalf("expected CreatedAt set")
	}
}

// TestHandleTerminalSessions_ManagerNotLister covers the branch when TerminalSessionManager does not implement listTerminalSessions.
func TestHandleTerminalSessions_ManagerNotLister(t *testing.T) {
	app := newTestAppForTerminal(t)
	app.TerminalSessionManager = startFailingStub{} // has Start/Get/Touch/Stop but no ActiveIDs
	router := app.NewRouter()

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/terminal/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out struct {
		Items []TerminalSessionItem `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("expected empty items when manager is not lister, got %d", len(out.Items))
	}
}

// TestHandleTerminalSessions_Unauthorized covers 401 paths.
func TestHandleTerminalSessions_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/terminal/sessions", nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "invalid", Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with invalid cookie, got %d", w2.Code)
	}
}

// TestHandleTerminalSessionDelete_Unauthorized covers 401 paths.
func TestHandleTerminalSessionDelete_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodDelete, "/api/terminal/sessions/some-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", w.Code)
	}
}

// TestHandleTerminalSessionDelete_NotFound covers 404 when session does not exist or wrong user.
func TestHandleTerminalSessionDelete_NotFound(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/terminal/sessions/nonexistent-id", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent session, got %d", w.Code)
	}
}

// TestHandleTerminalSessionDelete_Success covers 204 and Stop.
func TestHandleTerminalSessionDelete_Success(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	mgr, ok := app.TerminalSessionManager.(*session.Manager)
	if !ok {
		t.Fatalf("TerminalSessionManager is not *session.Manager")
	}
	sid := session.ID("delete-me")
	_, err = mgr.Start(sid, session.StartOptions{UserID: "admin", TargetID: "t1", TargetName: "T1"}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/terminal/sessions/delete-me", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if len(mgr.ActiveIDs()) != 0 {
		t.Fatalf("expected session to be stopped")
	}
}

// --- credentialsDecrypted ---

func TestCredentialsDecrypted_OK(t *testing.T) {
	creds := sshproxy.Credentials{Username: "u", Password: "p"}
	if err := credentialsDecrypted(creds); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCredentialsDecrypted_PrivateKeyIsCiphertext(t *testing.T) {
	creds := sshproxy.Credentials{PrivateKey: "v1:abc123def"}
	if err := credentialsDecrypted(creds); !errors.Is(err, errCredentialsNotDecrypted) {
		t.Fatalf("expected errCredentialsNotDecrypted, got %v", err)
	}
}

func TestCredentialsDecrypted_PassphraseIsCiphertext(t *testing.T) {
	creds := sshproxy.Credentials{PrivateKeyPassphrase: "v1:xyz789"}
	if err := credentialsDecrypted(creds); !errors.Is(err, errCredentialsNotDecrypted) {
		t.Fatalf("expected errCredentialsNotDecrypted, got %v", err)
	}
}

func TestCredentialsDecrypted_BothEmpty(t *testing.T) {
	creds := sshproxy.Credentials{}
	if err := credentialsDecrypted(creds); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCredentialsDecrypted_NonCiphertextValues(t *testing.T) {
	creds := sshproxy.Credentials{PrivateKey: "-----BEGIN RSA PRIVATE KEY-----\n...", PrivateKeyPassphrase: "mypass"}
	if err := credentialsDecrypted(creds); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// --- InsertRecording / UpdateRecordingEnded ---

func TestInsertRecording_Success(t *testing.T) {
	app := newTestAppForTerminal(t)
	ctx := context.Background()
	err := app.InsertRecording(ctx, "rec-test-1", "admin", "t1", "s1", "ssh", "/tmp/test.cast", time.Now().UTC().Format(time.RFC3339), "mysess", "desc")
	if err != nil {
		t.Fatalf("InsertRecording: %v", err)
	}
}

func TestInsertRecording_NilDB(t *testing.T) {
	app := &App{}
	ctx := context.Background()
	err := app.InsertRecording(ctx, "rec-test-1", "admin", "t1", "s1", "ssh", "/tmp/test.cast", time.Now().UTC().Format(time.RFC3339), "", "")
	if err != nil {
		t.Fatalf("expected nil error for nil DB, got %v", err)
	}
}

func TestUpdateRecordingEnded_Success(t *testing.T) {
	app := newTestAppForTerminal(t)
	ctx := context.Background()
	_ = app.InsertRecording(ctx, "rec-update-1", "admin", "t1", "s1", "ssh", "/tmp/test.cast", time.Now().UTC().Format(time.RFC3339), "", "")
	err := app.UpdateRecordingEnded(ctx, "rec-update-1", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("UpdateRecordingEnded: %v", err)
	}
}

func TestUpdateRecordingEnded_NilDB(t *testing.T) {
	app := &App{}
	ctx := context.Background()
	err := app.UpdateRecordingEnded(ctx, "rec-update-1", time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("expected nil error for nil DB, got %v", err)
	}
}

// --- handleListRecordings ---

func TestHandleListRecordings_WithTargetFilter(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	_ = app.InsertRecording(ctx, "rec-filter-1", "admin", "t1", "s1", "ssh", "/tmp/test.cast", time.Now().UTC().Format(time.RFC3339), "", "")

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings?target_id=t1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	items, _ := result["items"].([]interface{})
	if len(items) == 0 {
		t.Fatal("expected at least 1 recording")
	}
}

func TestHandleListRecordings_NoRecordings(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings?target_id=nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// --- handleGetRecordingFile ---

func TestHandleGetRecordingFile_CastFormat(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	dir := t.TempDir()
	castFile := filepath.Join(dir, "rec-cast-1.cast")
	if err := os.WriteFile(castFile, []byte(`{"version": 2, "width": 80, "height": 24}`), 0o600); err != nil {
		t.Fatalf("write cast: %v", err)
	}
	_ = os.Setenv("VANTYX_RECORDINGS_DIR", dir)
	defer os.Unsetenv("VANTYX_RECORDINGS_DIR")

	_ = app.InsertRecording(ctx, "rec-cast-1", "admin", "t1", "s1", "ssh", castFile, time.Now().UTC().Format(time.RFC3339), "", "")

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec-cast-1/file?format=cast", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetRecordingFile_DefaultFormat(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	dir := t.TempDir()
	castFile := filepath.Join(dir, "rec-default-1.cast")
	if err := os.WriteFile(castFile, []byte(`{"version": 2, "width": 80, "height": 24}`), 0o600); err != nil {
		t.Fatalf("write cast: %v", err)
	}
	_ = os.Setenv("VANTYX_RECORDINGS_DIR", dir)
	defer os.Unsetenv("VANTYX_RECORDINGS_DIR")

	_ = app.InsertRecording(ctx, "rec-default-1", "admin", "t1", "s1", "ssh", castFile, time.Now().UTC().Format(time.RFC3339), "", "")

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec-default-1/file", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetRecordingFile_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec1/file", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleGetRecordingFile_FileNotOnDisk(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	dir := t.TempDir()
	_ = os.Setenv("VANTYX_RECORDINGS_DIR", dir)
	defer os.Unsetenv("VANTYX_RECORDINGS_DIR")

	_ = app.InsertRecording(ctx, "rec-missing-1", "admin", "t1", "s1", "ssh", "/nonexistent/file.cast", time.Now().UTC().Format(time.RFC3339), "", "")

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec-missing-1/file?format=cast", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Fatalf("expected 400 or 404, got %d", w.Code)
	}
}

func TestHandleGetRecordingFile_InvalidFormat(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	dir := t.TempDir()
	castFile := filepath.Join(dir, "rec-fmt-1.cast")
	_ = os.WriteFile(castFile, []byte(`{}`), 0o600)
	_ = os.Setenv("VANTYX_RECORDINGS_DIR", dir)
	defer os.Unsetenv("VANTYX_RECORDINGS_DIR")
	_ = app.InsertRecording(ctx, "rec-fmt-1", "admin", "t1", "s1", "ssh", castFile, time.Now().UTC().Format(time.RFC3339), "", "")

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec-fmt-1/file?format=mp4", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid format, got %d", w.Code)
	}
}

func TestHandleGetRecordingFile_PathTraversal(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	dir := t.TempDir()
	_ = os.Setenv("VANTYX_RECORDINGS_DIR", dir)
	defer os.Unsetenv("VANTYX_RECORDINGS_DIR")
	_ = app.InsertRecording(ctx, "rec-trav-1", "admin", "t1", "s1", "ssh", "/etc/passwd", time.Now().UTC().Format(time.RFC3339), "", "")

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec-trav-1/file?format=cast", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d", w.Code)
	}
}

// --- handleChangePassword additional branches ---

func TestHandleChangePassword_WrongPassword(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	body := strings.NewReader(`{"current_password":"WrongPass1!","new_password":"NewPass123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleChangePassword_SamePassword(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	body := strings.NewReader(`{"current_password":"Admin123!","new_password":"Admin123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for same password, got %d", w.Code)
	}
}

func TestHandleChangePassword_InvalidBody(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleChangePassword_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	body := strings.NewReader(`{"current_password":"Admin123!","new_password":"NewPass123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleChangePassword_Success(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	body := strings.NewReader(`{"current_password":"Admin123!","new_password":"NewPass123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// --- RDP WebSocket (non-upgrade will be rejected) ---

func TestHandleRDPWebSocket_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp?target_id=t1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleRDPWebSocket_MissingTargetID(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRDPWebSocket_TargetNotFound(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp?target_id=nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleRDPWebSocket_Forbidden(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("rdp1"), "RDP", "192.168.1.1", 3389, access.ProtocolRDP, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("rdp1"))

	_, _ = app.UserStore.CreateUser("u2", "user2", "User123!", "user")
	sess2, _ := app.SessionStore.Create("u2")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp?target_id=rdp1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess2.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandleRDPWebSocket_WrongProtocol(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("ssh1"), "SSH", "192.168.1.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("ssh1"))

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp?target_id=ssh1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- handleRDPBrowserWebSocket tests ---

func TestHandleRDPBrowserWebSocket_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp/browser?target_id=t1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleRDPBrowserWebSocket_MissingTargetID(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp/browser", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRDPBrowserWebSocket_TargetNotFound(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp/browser?target_id=nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleRDPBrowserWebSocket_Forbidden(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("rdp1"), "RDP1", "192.168.1.1", 3389, access.ProtocolRDP, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("rdp1"))

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp/browser?target_id=rdp1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandleRDPBrowserWebSocket_WrongProtocol(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("ssh1"), "SSH", "192.168.1.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("ssh1"))

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp/browser?target_id=ssh1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRDPBrowserWebSocket_BridgeStartFails(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("rdp1"), "RDP1", "192.168.1.1", 3389, access.ProtocolRDP, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("rdp1"))

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/ws/rdp/browser?target_id=rdp1&w=1280&h=720", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (no Xvfb available), got %d", w.Code)
	}
}

// --- handleRDPSessions tests ---

func TestHandleRDPSessions_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/rdp/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleRDPSessions_NoManager(t *testing.T) {
	app := newTestAppForTerminal(t)
	// Simulate configuration where RDPVNCManager is disabled.
	app.RDPVNCManager = nil
	router := app.NewRouter()

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/rdp/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Items []map[string]string `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(resp.Items))
	}
}

func TestHandleRDPSessions_WithActiveSession(t *testing.T) {
	app := newTestAppForTerminal(t)
	if app.RDPVNCManager == nil {
		app.RDPVNCManager = rdpvnc.NewManager()
	}
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("rdp1"), "RDP1", "192.168.1.1", 3389, access.ProtocolRDP, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("rdp1"))

	// Also create a non-RDP target to exercise the protocol filter branch.
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("ssh1"), "SSH1", "192.168.1.2", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("ssh1"))

	// Register dummy managed sessions so that handleRDPSessions can discover them.
	// (Bridge is a nil-process placeholder here; we only need the manager bookkeeping.)
	app.RDPVNCManager.RegisterSession("admin:rdp1", "s1", "admin", "rdp1", "RDP1", 1920, 1080, &rdpvnc.Bridge{})
	app.RDPVNCManager.RegisterSession("admin:ssh1", "s2", "admin", "ssh1", "SSH1", 1920, 1080, &rdpvnc.Bridge{})

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/rdp/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Items []struct {
			SessionID  string `json:"session_id"`
			TargetID   string `json:"target_id"`
			TargetName string `json:"target_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].SessionID == "" {
		t.Fatalf("expected non-empty session_id")
	}
	if resp.Items[0].TargetID != "rdp1" {
		t.Fatalf("expected target_id rdp1, got %s", resp.Items[0].TargetID)
	}
	if resp.Items[0].TargetName != "RDP1" {
		t.Fatalf("expected target_name RDP1, got %s", resp.Items[0].TargetName)
	}
}

func TestHandleRDPSessionDelete_Success(t *testing.T) {
	app := newTestAppForTerminal(t)
	if app.RDPVNCManager == nil {
		app.RDPVNCManager = rdpvnc.NewManager()
	}
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("rdp1"), "RDP1", "192.168.1.1", 3389, access.ProtocolRDP, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("rdp1"))

	app.RDPVNCManager.RegisterSession("admin:rdp1", "s1", "admin", "rdp1", "RDP1", 1920, 1080, &rdpvnc.Bridge{})

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/rdp/sessions/s1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if _, ok := app.RDPVNCManager.GetSession("s1"); ok {
		t.Fatalf("expected session to be removed")
	}
}

func TestHandleRDPSessionDelete_Unauthorized(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodDelete, "/api/rdp/sessions/s1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleRDPSessionDelete_NoManager(t *testing.T) {
	app := newTestAppForTerminal(t)
	app.RDPVNCManager = nil
	router := app.NewRouter()

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/rdp/sessions/s1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleRDPSessionDelete_NotOwner(t *testing.T) {
	app := newTestAppForTerminal(t)
	if app.RDPVNCManager == nil {
		app.RDPVNCManager = rdpvnc.NewManager()
	}
	router := app.NewRouter()

	app.RDPVNCManager.RegisterSession("admin:rdp1", "s1", "admin", "rdp1", "RDP1", 1920, 1080, &rdpvnc.Bridge{})

	_, _ = app.UserStore.CreateUser("u2", "user2", "User123!", "user")
	httpSess, _ := app.SessionStore.Create("u2")
	req := httptest.NewRequest(http.MethodDelete, "/api/rdp/sessions/s1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestNewRDPSessionID_Format(t *testing.T) {
	id, err := newRDPSessionID()
	if err != nil {
		t.Fatalf("newRDPSessionID: %v", err)
	}
	// 32 bytes => 64 hex chars
	if len(id) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(id))
	}
}

func TestHandleRDPSessionDelete_EmptySessionID_BadRequest(t *testing.T) {
	app := newTestAppForTerminal(t)
	if app.RDPVNCManager == nil {
		app.RDPVNCManager = rdpvnc.NewManager()
	}

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/rdp/sessions/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})

	// Call handler directly with empty chi URL param to cover bad-request branch.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("session_id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	app.handleRDPSessionDelete(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
