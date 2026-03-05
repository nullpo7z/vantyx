package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
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

	// Ensure target_id=demo exists for this test (admin has access via a group).
	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_, _ = app.TargetStore.Create("demo", "Demo host", "127.0.0.1", 22, access.ProtocolSSH)
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "demo")

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/ssh", RawQuery: "target_id=demo"}

	_, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err == nil {
		t.Fatalf("expected WebSocket dial to fail without cookie, got nil error")
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
	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_, _ = app.TargetStore.Create("demo", "Demo host", "127.0.0.1", 22, access.ProtocolSSH)
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "demo")

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

	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_, _ = app.TargetStore.Create("telnet1", "Telnet Host", "127.0.0.1", 23, access.ProtocolTelnet)
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "telnet1")

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
	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_, _ = app.TargetStore.Create("demo", "Demo host", "127.0.0.1", 22, access.ProtocolSSH)
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "demo")

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
	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_, _ = app.TargetStore.Create("demo", "Demo host", "127.0.0.1", 22, access.ProtocolSSH)
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "demo")

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
