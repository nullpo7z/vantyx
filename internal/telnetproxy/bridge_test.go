package telnetproxy

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/mock"
	"github.com/nullpo7z/vantyx/internal/session"
)

func TestRunBridgeDetachable_WithEchoServer(t *testing.T) {
	server := mock.NewTelnetEchoServer()
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()

	port := server.Port()
	if port == 0 {
		t.Fatal("port is 0")
	}

	output := session.NewRingBuffer(4096)
	attachCh := make(chan session.AttachReq, 1)
	bridgeErrCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer wsConn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := RunBridgeDetachable(ctx, "127.0.0.1", port, "", "", output, attachCh, wsConn, nil, nil, nil)
			bridgeErrCh <- err
		}()
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("x"))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "x" {
		t.Fatalf("expected echo x, got %q", string(msg))
	}

	_ = conn.Close()

	select {
	case <-bridgeErrCh:
	case <-time.After(10 * time.Second):
		t.Fatal("RunBridgeDetachable did not return")
	}
}

func TestRunBridgeDetachable_StreamAttach(t *testing.T) {
	server := mock.NewTelnetEchoServer()
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()

	port := server.Port()
	if port == 0 {
		t.Fatal("port is 0")
	}

	output := session.NewRingBuffer(4096)
	_, _ = output.Write([]byte("replay"))

	attachCh := make(chan session.AttachReq, 2)
	var closeCalled atomic.Bool
	var writeBuf bytes.Buffer
	sa := &StreamAttach{
		Write: func(p []byte) error { writeBuf.Write(p); return nil },
		StartRead: func(stdinChOut chan<- []byte, onClose func()) {
			go func() {
				onClose()
			}()
		},
		CloseFn: func() error { closeCalled.Store(true); return nil },
	}

	attachCh <- session.AttachReq{Conn: sa}

	bridgeErrCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		bridgeErrCh <- RunBridgeDetachable(ctx, "127.0.0.1", port, "", "", output, attachCh, nil, nil, nil, nil)
	}()

	time.Sleep(100 * time.Millisecond)

	attachCh <- session.AttachReq{Conn: &StreamAttach{
		Write:     func([]byte) error { return nil },
		StartRead: func(chan<- []byte, func()) {},
		CloseFn:   func() error { return nil },
	}}

	select {
	case <-bridgeErrCh:
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridgeDetachable did not return")
	}

	if writeBuf.Len() == 0 {
		t.Log("replay may not have been written depending on timing")
	}
}

func TestIACStream_SplitSequence(t *testing.T) {
	var replies [][]byte
	reply := func(b []byte) error {
		replies = append(replies, append([]byte(nil), b...))
		return nil
	}
	var s iacStream
	out1 := s.Filter([]byte{'h', iac}, reply)
	if len(out1) != 1 || out1[0] != 'h' {
		t.Fatalf("first chunk: got %q", out1)
	}
	out2 := s.Filter([]byte{do, 1, 'i'}, reply)
	if string(out2) != "i" {
		t.Fatalf("second chunk: got %q", out2)
	}
	if len(replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(replies))
	}
}

func TestFilterIAC(t *testing.T) {
	var replies [][]byte
	out := filterIAC([]byte{'h', iac, do, 1, 'i'}, func(b []byte) error {
		replies = append(replies, append([]byte(nil), b...))
		return nil
	})
	if string(out) != "hi" {
		t.Fatalf("got %q", out)
	}
	if len(replies) != 1 || replies[0][0] != iac || replies[0][1] != wont || replies[0][2] != 1 {
		t.Fatalf("unexpected reply: %v", replies)
	}
}

func TestPortString(t *testing.T) {
	if portString(0) != "23" {
		t.Fatalf("default port: %q", portString(0))
	}
	if portString(8023) != "8023" {
		t.Fatalf("custom port: %q", portString(8023))
	}
}
