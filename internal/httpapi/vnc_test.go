package httpapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
)

func newTestAppForVNC(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vnc.db")
	if err := os.Setenv("VANTYX_SQLITE_PATH", dbPath); err != nil {
		t.Fatalf("set env: %v", err)
	}
	return NewApp()
}

func TestHandleVNCWebSocket_UnauthorizedWithoutCookie(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("vnc1"), "VNC Host", "127.0.0.1", 5900, access.ProtocolVNC, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("vnc1"))

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/vnc", RawQuery: "target_id=vnc1"}
	_, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err == nil {
		t.Fatal("expected WebSocket dial to fail without cookie")
	}
}

func TestHandleVNCWebSocket_MissingTargetID(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/vnc", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleVNCWebSocket_TargetNotFound(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/vnc?target_id=nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleVNCWebSocket_ForbiddenTarget(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("vnc1"), "VNC Host", "127.0.0.1", 5900, access.ProtocolVNC, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("vnc1"))

	_, _ = app.UserStore.CreateUser("other", "other", "Other1!x", "")
	httpSess, err := app.SessionStore.Create("other")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/vnc?target_id=vnc1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandleVNCWebSocket_NotVNCTargetReturns400(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("ssh1"), "SSH Host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("ssh1"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/vnc?target_id=ssh1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-VNC target, got %d", w.Code)
	}
}

func TestHandleVNCWebSocket_UpgradeFailsWithoutWebSocketRequest(t *testing.T) {
	app := newTestAppForVNC(t)
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

	req := httptest.NewRequest(http.MethodGet, "/ws/vnc?target_id=vnc1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when upgrade fails (no WS headers), got %d", w.Code)
	}
}

func TestHandleVNCWebSocket_SuccessBridgesToTarget(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()

	// Echo server as fake VNC backend
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			_, _ = io.Copy(conn, conn)
			conn.Close()
		}
	}()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("vnc1"), "VNC Host", "127.0.0.1", uint16(port&0xffff), access.ProtocolVNC, access.GroupID("g1"), "g1", "", "", "", "", false, false, false) // #nosec G115
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("vnc1"))

	httpSess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws/vnc", RawQuery: "target_id=vnc1"}
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
	header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"}).String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}
	defer conn.Close()

	// Proxy should bridge to echo server
	want := []byte("rfb")
	if err := conn.WriteMessage(websocket.BinaryMessage, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
