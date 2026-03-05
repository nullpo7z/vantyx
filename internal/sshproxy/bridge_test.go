package sshproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/mock"
)

func TestPortString(t *testing.T) {
	if got := portString(22); got != "22" {
		t.Fatalf("portString(22) = %q, want 22", got)
	}
	if got := portString(0); got != "22" {
		t.Fatalf("portString(0) = %q, want 22", got)
	}
	if got := portString(8080); got != "8080" {
		t.Fatalf("portString(8080) = %q, want 8080", got)
	}
}

func TestReadCredentials_Valid(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var srvConn *websocket.Conn
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		srvConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = srvConn.WriteMessage(websocket.TextMessage, []byte(`{"username":"alice","password":"secret"}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	creds, err := ReadCredentials(conn, 2*time.Second)
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}
	if creds.Username != "alice" || creds.Password != "secret" {
		t.Fatalf("got %+v", creds)
	}
	if srvConn != nil {
		_ = srvConn.Close()
	}
}

func TestReadCredentials_InvalidJSON(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("not json"))
			_ = conn.Close()
		}
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, err = ReadCredentials(conn, 2*time.Second)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRunBridge_DialFails(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var serverConn *websocket.Conn
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		serverConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer serverConn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = RunBridge(ctx, serverConn, "127.0.0.1", 1, "u", "p", nil)
		}()
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	time.Sleep(500 * time.Millisecond)
}

func TestRunBridge_WithEchoSSHServer(t *testing.T) {
	server, err := mock.NewSSHEchoServer("test", "test")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()

	port := server.Port()
	if port == 0 {
		t.Fatal("port is 0")
	}

	upgrader := websocket.Upgrader{}
	var wsConn *websocket.Conn
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		wsConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer wsConn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = RunBridge(ctx, wsConn, "127.0.0.1", port, "test", "test", nil)
		}()
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "hi" {
		t.Fatalf("expected hi, got %q", string(msg))
	}
}
