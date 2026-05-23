package mock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

func TestSSHEchoServer_EchoViaRunBridge(t *testing.T) {
	server, err := NewSSHEchoServer("test", "test")
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
			_ = sshproxy.RunBridge(ctx, wsConn, "127.0.0.1", port, "test", "test", "", "", nil, nil, nil)
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

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "hello" {
		t.Fatalf("expected hello, got %q", string(msg))
	}
}

func TestSSHEchoServer_AddrAndPortBeforeStart(t *testing.T) {
	srv, err := NewSSHEchoServer("u", "p")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if a := srv.Addr(); a != "" {
		t.Fatalf("Addr before Start expected empty, got %q", a)
	}
	if p := srv.Port(); p != 0 {
		t.Fatalf("Port before Start expected 0, got %d", p)
	}
}

func TestSSHEchoServer_AddrAndPortAfterStart(t *testing.T) {
	srv, err := NewSSHEchoServer("u", "p")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()
	if a := srv.Addr(); a == "" {
		t.Fatal("Addr after Start expected non-empty")
	}
	if p := srv.Port(); p == 0 {
		t.Fatal("Port after Start expected non-zero")
	}
}

func TestSSHEchoServer_CloseUnstarted(t *testing.T) {
	srv, err := NewSSHEchoServer("u", "p")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close on unstarted server: %v", err)
	}
}
