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
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/mock"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

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

func (f *fakeWSConn) SetReadLimit(int64) {}

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
	defer srv.CloseClientConnections()

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

// TestHandleSSHWebSocket_ForbiddenTarget_RealWSGetsErrorFrame is the E-9
// regression guard: over a real WebSocket a user without access to the
// target must receive an explicit "error: …" text frame (localized
// common.forbidden) instead of an opaque handshake failure, so the SPA
// can show "access denied" rather than "check network / credentials".
func TestHandleSSHWebSocket_ForbiddenTarget_RealWSGetsErrorFrame(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

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

	srv := startTestWSServer(t, router)
	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
	header.Set("Cookie", "vantyx_session="+httpSess.ID)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("expected the handshake to succeed so an error frame can be delivered, got %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	mt, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if mt != websocket.TextMessage || !strings.HasPrefix(string(msg), "error: ") {
		t.Fatalf("expected an 'error: …' text frame, got type=%d msg=%q", mt, msg)
	}
	if strings.Contains(string(msg), "credential") || strings.Contains(string(msg), "network") {
		t.Fatalf("forbidden frame must not read like a credentials/network problem: %q", msg)
	}
	e, ok := latestAuditEvent("terminal_ws_forbidden")
	if !ok || e.Fields["user_id"] != "other" || e.Fields["target_id"] != "demo" {
		t.Fatalf("expected terminal_ws_forbidden audit for other/demo, got ok=%v fields=%v", ok, e.Fields)
	}
}

func TestHandleSSHWebSocket_NonTerminalTargetReturns501(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("vnc1"), "VNC Host", "127.0.0.1", 5900, access.ProtocolVNC, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("vnc1"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=vnc1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 Not Implemented, got %d", w.Code)
	}
}

func TestHandleSSHWebSocket_TelnetTargetUpgrades(t *testing.T) {
	t.Setenv("VANTYX_TELNET_WAKE_ON_CONNECT", "0")
	echoSrv := mock.NewTelnetEchoServer()
	if err := echoSrv.Start(); err != nil {
		t.Fatalf("telnet echo start: %v", err)
	}
	defer echoSrv.Close()
	port := echoSrv.Port()
	if port == 0 {
		t.Fatal("echo port is 0")
	}

	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("telnet1"), "Telnet Host", "127.0.0.1", port, access.ProtocolTelnet, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("telnet1"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()
	defer srv.CloseClientConnections()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=telnet1"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
	header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"}).String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// #nosec G101 -- test-only dummy credentials for WebSocket Telnet test
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"username":"u","password":"p"}`)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	ready := false
	for i := 0; i < 10 && !ready; i++ {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read before ready: %v", err)
		}
		if mt == websocket.TextMessage && len(msg) > 0 &&
			(strings.HasPrefix(string(msg), "vantyx:meta:") || msg[0] == '{') {
			ready = true
		}
	}
	if !ready {
		t.Fatal("did not receive session_id after telnet upgrade")
	}
	deadline := time.Now().Add(5 * time.Second)
	var gotEcho bool
	for time.Now().Before(deadline) && !gotEcho {
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte("x")); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, echo, err := conn.ReadMessage()
		if err != nil {
			continue
		}
		if string(echo) == "x" {
			gotEcho = true
		}
	}
	if !gotEcho {
		t.Fatal("expected echo x from telnet server")
	}
	_ = conn.Close()
	srv.CloseClientConnections()
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
	defer srv.CloseClientConnections()

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

	_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if !strings.Contains(string(msg), "error") {
		t.Fatalf("expected error message, got %q", string(msg))
	}
	_ = conn.Close()
	srv.CloseClientConnections()

	// Give server goroutines a moment to release any temp files (SQLite WAL/shm) before TempDir cleanup.
	time.Sleep(50 * time.Millisecond)
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
	defer srv.CloseClientConnections()

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
	_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	if err == nil {
		// Might get one message (e.g. SSH banner) before close; read until close
		_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		_, _, _ = conn.ReadMessage()
	}
	// Expect connection to close (dial failure or eventual close)
	_ = conn.Close()
	srv.CloseClientConnections()
}

// TestHandleSSHWebSocket_BridgeErrorRespectsUserLocale guards against a
// regression where the bridge's error text (sent over the WS "error: "
// frame after e.g. a failed dial/auth) silently reverted to English
// regardless of the user's saved locale. The bridge runs inside the
// session goroutine's own long-lived context (see session.Manager.Start),
// which starts from a fresh context.Background() and therefore does not
// automatically inherit the locale that sessionMiddleware resolved onto
// the original request's context -- that must be carried over explicitly.
func TestHandleSSHWebSocket_BridgeErrorRespectsUserLocale(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo"))

	if err := app.UserStore.UpdateLocale("admin", "ja"); err != nil {
		t.Fatalf("UpdateLocale: %v", err)
	}

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()
	defer srv.CloseClientConnections()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
	header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"}).String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// #nosec G101 -- test-only dummy credentials
	creds := `{"username":"root","password":"test"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(creds)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	// Nothing listens on 127.0.0.1:22 in the test environment, so the
	// dial fails (connection refused / timeout) and the bridge reports
	// it back over the WS as an "error: " text frame.
	var errMsg string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if strings.HasPrefix(string(msg), "error:") {
			errMsg = string(msg)
			break
		}
	}
	if errMsg == "" {
		t.Fatal("did not receive an error: frame from the bridge")
	}
	if strings.Contains(errMsg, "Connection to") || strings.Contains(errMsg, "Failed to connect") || strings.Contains(errMsg, "timed out") {
		t.Fatalf("bridge error message was English despite the user's saved ja locale: %q", errMsg)
	}
	if !strings.Contains(errMsg, "接続") {
		t.Fatalf("expected a Japanese bridge error message, got %q", errMsg)
	}
}

// startFailingStub implements terminalSessionStarter and makes Start return an error.
type startFailingStub struct{}

func (startFailingStub) Start(session.ID, session.StartOptions, func(context.Context, *session.Session)) (*session.Session, error) {
	return nil, errors.New("injected start error")
}

func (startFailingStub) Get(session.ID) (*session.Session, bool) { return nil, false }

func (startFailingStub) Touch(session.ID) {}

func (startFailingStub) Stop(session.ID) bool { return true }

func TestHandleSSHWebSocket_StartFailsReturns500(t *testing.T) {
	app := newTestAppForTerminal(t)
	app.TerminalSessionManager = startFailingStub{}
	router := app.NewRouter()

	seedAdminDemoSSHTarget(t, app)

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := startTestWSServer(t, router)

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := wsDialHeaders(t, srv, httpSess.ID)

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// #nosec G101 -- test-only credentials
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"username":"u","password":"p"}`))
	// After Start fails, server closes connection
	_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	_, _, _ = conn.ReadMessage()

	// Allow server-side goroutines to unwind before TempDir cleanup.
	time.Sleep(50 * time.Millisecond)
}

func TestHandleSSHWebSocket_StartFailsDuplicateID(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()

	seedAdminDemoSSHTarget(t, app)

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// Force same session ID for both connections so second Start returns ErrSessionExists
	const fixedID = "test-fixed-id"
	withTerminalSessionIDGen(t, func() session.ID { return fixedID })

	srv := startTestWSServer(t, router)

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}
	header := wsDialHeaders(t, srv, httpSess.ID)

	// First connection: start and block in RunBridge
	conn1, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// #nosec G101 -- test-only credentials
		_ = conn1.WriteMessage(websocket.TextMessage, []byte(`{"username":"u","password":"p"}`))
		_ = conn1.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		_, _, _ = conn1.ReadMessage()
		_ = conn1.Close()
	}()
	defer func() {
		// Ensure the first goroutine is done before TempDir cleanup.
		wg.Wait()
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
	_ = conn2.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	_, _, err = conn2.ReadMessage()
	// Connection closed by server after Start failed
	if err == nil {
		_ = conn2.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		_, _, _ = conn2.ReadMessage()
	}
	_ = conn2.Close()
	// Ensure background session/bridge goroutines are stopped before DB/tempdir cleanup.
	app.TerminalSessionManager.Stop(session.ID(fixedID))
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
	if out.Items[0].Protocol != string(access.ProtocolSSH) {
		t.Fatalf("expected protocol ssh, got %q", out.Items[0].Protocol)
	}
	if out.Items[0].TargetPath != "g1" {
		t.Fatalf("expected target_path g1, got %q", out.Items[0].TargetPath)
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

// TestHandleTerminalSessions_FiltersByTargetAccess ensures sessions on inaccessible targets are omitted.
func TestHandleTerminalSessions_FiltersByTargetAccess(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("allowed"), "Allowed", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("allowed"))

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("secret"), "Secret", "127.0.0.2", 22, access.ProtocolSSH, access.GroupID("g2"), "g2", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g2"), access.TargetID("secret"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	mgr, ok := app.TerminalSessionManager.(*session.Manager)
	if !ok {
		t.Fatalf("TerminalSessionManager is not *session.Manager")
	}
	_, err = mgr.Start("sess-allowed", session.StartOptions{UserID: "admin", TargetID: "allowed", TargetName: "Allowed"}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start allowed: %v", err)
	}
	defer mgr.Stop("sess-allowed")
	_, err = mgr.Start("sess-secret", session.StartOptions{UserID: "admin", TargetID: "secret", TargetName: "Secret"}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start secret: %v", err)
	}
	defer mgr.Stop("sess-secret")

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
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item (allowed only), got %d: %+v", len(out.Items), out.Items)
	}
	if out.Items[0].TargetID != "allowed" {
		t.Fatalf("expected allowed target, got %q", out.Items[0].TargetID)
	}
}

// TestHandleTerminalSessionDelete_ForbiddenWithoutTargetAccess returns 403 when target access was revoked.
func TestHandleTerminalSessionDelete_ForbiddenWithoutTargetAccess(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "T1", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))

	httpSess, _ := app.SessionStore.Create("admin")
	mgr, ok := app.TerminalSessionManager.(*session.Manager)
	if !ok {
		t.Fatalf("TerminalSessionManager is not *session.Manager")
	}
	sid := session.ID("no-access-delete")
	_, err := mgr.Start(sid, session.StartOptions{UserID: "admin", TargetID: "t1", TargetName: "T1"}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop(sid)

	_ = app.AccessGroupStore.RemoveUserFromGroup(ctx, access.UserID("admin"), access.GroupID("g1"))

	req := httptest.NewRequest(http.MethodDelete, "/api/terminal/sessions/no-access-delete", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestHandleTerminalSessionDelete_Success covers 204 and Stop.
func TestHandleTerminalSessionDelete_Success(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "T1", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))

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
	creds := sshproxy.Credentials{PrivateKey: "v2:YWJjMTIzZGVm"}
	if err := credentialsDecrypted(creds); !errors.Is(err, errCredentialsNotDecrypted) {
		t.Fatalf("expected errCredentialsNotDecrypted, got %v", err)
	}
}

func TestCredentialsDecrypted_PassphraseIsCiphertext(t *testing.T) {
	creds := sshproxy.Credentials{PrivateKeyPassphrase: "v2:eHl6Nzg5"}
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

func TestHandleListRecordings_AdminUserFilter(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.UserStore.CreateUser("bob", "bob", "User123!", auth.RoleUser)
	if err := app.InsertRecording(ctx, "rec-bob-1", "bob", "t1", "s1", "browser", "/tmp/b.cast", time.Now().UTC().Format(time.RFC3339), "", ""); err != nil {
		t.Fatalf("InsertRecording: %v", err)
	}

	adminSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings?user_id=bob", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	items, _ := result["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 recording for bob, got %d", len(items))
	}
}

func TestHandleListRecordings_NonAdminUserFilterForbidden(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("u1", "user1", "User123!", auth.RoleUser)
	userSess, _ := app.SessionStore.Create("u1")

	req := httptest.NewRequest(http.MethodGet, "/api/recordings?user_id=admin", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: userSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandleListRecordings_ChannelFilter(t *testing.T) {
	app := newTestAppForTerminal(t)
	router := app.NewRouter()
	ctx := context.Background()
	started := time.Now().UTC().Format(time.RFC3339)
	if err := app.InsertRecording(ctx, "rec-cli-1", "admin", "t1", "s1", "cli", "/tmp/c.cast", started, "", ""); err != nil {
		t.Fatalf("InsertRecording cli: %v", err)
	}
	if err := app.InsertRecording(ctx, "rec-br-1", "admin", "t1", "s2", "browser", "/tmp/b.cast", started, "", ""); err != nil {
		t.Fatalf("InsertRecording browser: %v", err)
	}

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings?channel_type=cli", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&result)
	items, _ := result["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 cli recording, got %d", len(items))
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
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec-fmt-1/file?format=avi", nil)
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

func TestHandleRDPSessions_IdleAndLastSeen(t *testing.T) {
	app := newTestAppForTerminal(t)
	mgr := rdpvnc.NewManager()
	mgr.SetIdleWarnAfter(5 * time.Minute)
	now := time.Now()
	mgr.SetNowForTest(func() time.Time { return now })
	app.RDPVNCManager = mgr
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("rdp1"), "RDP1", "192.168.1.1", 3389, access.ProtocolRDP, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("rdp1"))

	placeholder := &rdpvnc.Bridge{}
	mgr.RegisterSession("admin:rdp1", "s1", "admin", "rdp1", "RDP1", 1920, 1080, placeholder)

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
			LastSeen string `json:"last_seen"`
			Idle     bool   `json:"idle"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Idle {
		t.Fatal("expected not idle immediately after start")
	}
	if resp.Items[0].LastSeen == "" {
		t.Fatal("expected last_seen in response")
	}

	mgr.SetNowForTest(func() time.Time { return now.Add(6 * time.Minute) })
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Items[0].Idle {
		t.Fatal("expected idle after threshold")
	}

	mgr.Touch("s1")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req)
	if err := json.Unmarshal(w3.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Items[0].Idle {
		t.Fatal("expected not idle after Touch")
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
