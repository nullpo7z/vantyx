package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

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

	// Proxy should bridge to echo server (first message may be session_meta JSON).
	want := []byte("rfb")
	if err := conn.WriteMessage(websocket.BinaryMessage, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, got, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) == string(want) {
			break
		}
		if strings.HasPrefix(string(got), `{"type":"session_meta"`) {
			if time.Now().After(deadline) {
				t.Fatal("timeout waiting for rfb echo after session_meta")
			}
			continue
		}
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHandleVNCSessions_Unauthorized(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/vnc/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleVNCSessions_WithActiveSession(t *testing.T) {
	app := newTestAppForVNC(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("vnc1"), "VNC Host", "127.0.0.1", 5900, access.ProtocolVNC, access.GroupID("g1"), "g1", "", "", "", "", false, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("vnc1"))

	sid := session.ID("vnc-list-test")
	_, err := app.VNCSessionManager.Start(sid, session.StartOptions{
		UserID: "admin", TargetID: "vnc1", TargetName: "VNC Host",
	}, func(ctx context.Context, _ *session.Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { app.VNCSessionManager.Stop(sid) })

	httpSess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/vnc/sessions", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: httpSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []struct {
			SessionID string `json:"session_id"`
			TargetID  string `json:"target_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].SessionID != string(sid) || resp.Items[0].TargetID != "vnc1" {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
}
