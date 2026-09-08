package vncproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
)

// TestRunBridgeDetachable_SecondWriterDemotesFirst guards against the
// regression this fix addresses: a second client attaching with
// Mode: AttachModeWriter (e.g. a resumed/second browser tab on the same
// shared VNC session) must demote the first writer to read-only, not
// let both drive the target concurrently. Mirrors the equivalent test
// already added for the SSH/Telnet detachable bridges.
func TestRunBridgeDetachable_SecondWriterDemotesFirst(t *testing.T) {
	echoAddr := tcpEchoServer(t)
	attachCh := make(chan session.AttachReq, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	var bridgeStarted sync.WaitGroup
	bridgeStarted.Add(1)
	mux.HandleFunc("/first", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade first: %v", err)
			return
		}
		go func() {
			_ = RunBridgeDetachable(ctx, echoAddr, attachCh,
				session.AttachReq{Conn: conn, UserID: "alice", Mode: session.AttachModeWriter},
				nil, nil)
		}()
		bridgeStarted.Done()
	})
	mux.HandleFunc("/second", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade second: %v", err)
			return
		}
		attachCh <- session.AttachReq{Conn: conn, UserID: "bob", Mode: session.AttachModeWriter}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	u1 := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/first"}
	first, _, err := websocket.DefaultDialer.Dial(u1.String(), nil)
	if err != nil {
		t.Fatalf("dial first: %v", err)
	}
	defer first.Close()
	bridgeStarted.Wait()

	u2 := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/second"}
	second, _, err := websocket.DefaultDialer.Dial(u2.String(), nil)
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer second.Close()

	// Give the bridge goroutine a moment to process the attach off attachCh.
	time.Sleep(100 * time.Millisecond)

	// The (now demoted) first writer's input must never reach its own
	// upstream TCP connection, so nothing should echo back to it.
	if err := first.WriteMessage(websocket.BinaryMessage, []byte("FROM-FIRST-MUST-BE-DROPPED")); err != nil {
		t.Fatalf("write first: %v", err)
	}
	// The current writer's input must reach the target and echo back.
	if err := second.WriteMessage(websocket.BinaryMessage, []byte("from-second")); err != nil {
		t.Fatalf("write second: %v", err)
	}

	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, got, err := second.ReadMessage()
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if string(got) != "from-second" {
		t.Fatalf("got %q, want %q", got, "from-second")
	}

	_ = first.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, msg, err := first.ReadMessage()
	if err == nil && strings.Contains(string(msg), "FROM-FIRST-MUST-BE-DROPPED") {
		t.Fatalf("demoted writer's input reached the target: %q", msg)
	}
}

// TestRunBridgeDetachable_ClosingWSFirstReleasesTCP guards against the
// leak this fix addresses: pumpWSToTCP previously had no cleanup on its
// own exit, so a client closing its WebSocket connection (the normal
// "closed the browser tab" exit) left pumpTCPToWS blocked forever on
// the now-orphaned upstream TCP read, leaking the goroutine and socket.
// This is verified indirectly: after the client closes its WS side, the
// bridge must remove the entry from its client set (proving both pumps
// tore down), rather than leaving it registered.
func TestRunBridgeDetachable_ClosingWSFirstReleasesTCP(t *testing.T) {
	echoAddr := tcpEchoServer(t)
	attachCh := make(chan session.AttachReq)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	bridgeCh := make(chan *detachableVNCBBridge, 1)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		b := newDetachableVNCBBridge(ctx, echoAddr, nil, attachCh)
		bridgeCh <- b
		b.doAttach(session.AttachReq{Conn: conn, UserID: "alice", Mode: session.AttachModeWriter})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	u := url.URL{Scheme: "ws", Host: srv.Listener.Addr().String(), Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	b := <-bridgeCh

	deadline := time.Now().Add(2 * time.Second)
	for {
		b.clientMu.Lock()
		n := len(b.clients)
		b.clientMu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client never registered on the bridge")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Client closes its side first (the common "closed the tab" exit).
	_ = conn.Close()

	deadline = time.Now().Add(2 * time.Second)
	for {
		b.clientMu.Lock()
		n := len(b.clients)
		b.clientMu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("entry still registered %v after WS close -- pumpWSToTCP leaked without cleaning up", 2*time.Second)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
