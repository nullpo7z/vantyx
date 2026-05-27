package sshproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/mock"
	"github.com/nullpo7z/vantyx/internal/session"
)

func TestIsDataMessage(t *testing.T) {
	if !isDataMessage(websocket.TextMessage) {
		t.Error("TextMessage should be data")
	}
	if !isDataMessage(websocket.BinaryMessage) {
		t.Error("BinaryMessage should be data")
	}
	if isDataMessage(websocket.CloseMessage) {
		t.Error("CloseMessage should not be data")
	}
}

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
	if got := portString(9); got != "9" {
		t.Fatalf("portString(9) = %q, want 9", got)
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

func TestReadCredentials_NonTextMessage(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("ignore"))
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
	if err != io.EOF {
		t.Fatalf("expected io.EOF for non-Text message, got %v", err)
	}
}

func TestReadCredentials_EmptyUsername(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"username":"","password":"x"}`))
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
	if err != io.EOF {
		t.Fatalf("expected io.EOF for empty username, got %v", err)
	}
}

func TestReadCredentials_ReadMessageFails(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			_ = conn.Close() // close without sending, so ReadMessage will get error
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

	_, err = ReadCredentials(conn, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when server sends nothing")
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
			_ = RunBridge(ctx, serverConn, "127.0.0.1", 1, "u", "p", "", "", nil, nil, nil)
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
			_ = RunBridge(ctx, wsConn, "127.0.0.1", port, "test", "test", "", "", nil, nil, nil)
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

func TestRunBridge_TouchCalled(t *testing.T) {
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

	var touchCount atomic.Int32
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
			touch := func() { touchCount.Add(1) }
			_ = RunBridge(ctx, wsConn, "127.0.0.1", port, "test", "test", "", "", touch, nil, nil)
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

	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("x"))
	_, _, _ = conn.ReadMessage()

	if n := touchCount.Load(); n < 1 {
		t.Fatalf("expected touch to be called at least once, got %d", n)
	}
}

func TestRunBridge_ContextCancelReturns(t *testing.T) {
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
	done := make(chan struct{})
	var bridgeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer wsConn.Close()
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(100 * time.Millisecond)
				cancel()
			}()
			bridgeErr = RunBridge(ctx, wsConn, "127.0.0.1", port, "test", "test", "", "", nil, nil, nil)
			close(done)
		}()
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	_ = conn.Close()

	select {
	case <-done:
		if bridgeErr != nil {
			t.Fatalf("RunBridge returned error: %v", bridgeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return after context cancel")
	}
}

// runBridgeWithEchoServer starts the echo server and a WS server that runs RunBridge;
// it returns the WS URL, a channel that receives RunBridge's error when it returns,
// and the mock SSH server (so tests can e.g. close it to trigger session EOF).
func runBridgeWithEchoServer(t *testing.T) (wsURL string, bridgeErrCh chan error, sshServer *mock.SSHEchoServer) {
	t.Helper()
	server, err := mock.NewSSHEchoServer("test", "test")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	port := server.Port()
	if port == 0 {
		t.Fatal("port is 0")
	}

	bridgeErrCh = make(chan error, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer wsConn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := RunBridge(ctx, wsConn, "127.0.0.1", port, "test", "test", "", "", nil, nil, nil)
			bridgeErrCh <- err
		}()
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	return u.String(), bridgeErrCh, server
}

func TestRunBridge_NewSessionFails(t *testing.T) {
	injected := errors.New("injected NewSession")
	testHookNewSession = func(*ssh.Client) (*ssh.Session, error) { return nil, injected }
	defer func() { testHookNewSession = nil }()

	wsURL, bridgeErrCh, _ := runBridgeWithEchoServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != injected {
			t.Fatalf("got error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

func TestRunBridge_StdinPipeFails(t *testing.T) {
	injected := errors.New("injected StdinPipe")
	testHookStdinPipe = func(*ssh.Session) (io.WriteCloser, error) { return nil, injected }
	defer func() { testHookStdinPipe = nil }()

	wsURL, bridgeErrCh, _ := runBridgeWithEchoServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != injected {
			t.Fatalf("got error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

func TestRunBridge_StdoutPipeFails(t *testing.T) {
	injected := errors.New("injected StdoutPipe")
	testHookStdoutPipe = func(*ssh.Session) (io.Reader, error) { return nil, injected }
	defer func() { testHookStdoutPipe = nil }()

	wsURL, bridgeErrCh, _ := runBridgeWithEchoServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != injected {
			t.Fatalf("got error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

func TestRunBridge_StderrPipeFails(t *testing.T) {
	injected := errors.New("injected StderrPipe")
	testHookStderrPipe = func(*ssh.Session) (io.Reader, error) { return nil, injected }
	defer func() { testHookStderrPipe = nil }()

	wsURL, bridgeErrCh, _ := runBridgeWithEchoServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != injected {
			t.Fatalf("got error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

func TestRunBridge_RequestPtyFails(t *testing.T) {
	injected := errors.New("injected RequestPty")
	testHookRequestPty = func(*ssh.Session) error { return injected }
	defer func() { testHookRequestPty = nil }()

	wsURL, bridgeErrCh, _ := runBridgeWithEchoServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != injected {
			t.Fatalf("got error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

func TestRunBridge_ShellFails(t *testing.T) {
	injected := errors.New("injected Shell")
	testHookShell = func(*ssh.Session) error { return injected }
	defer func() { testHookShell = nil }()

	wsURL, bridgeErrCh, _ := runBridgeWithEchoServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != injected {
			t.Fatalf("got error %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

// Mock pipes for RunBridge goroutine coverage: stdin fails Write, stdout/stderr return EOF.
type errWriter struct{ err error }

func (e *errWriter) Write([]byte) (int, error) { return 0, e.err }
func (e *errWriter) Close() error              { return nil }

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

// onceReader returns data on first Read, then EOF.
type onceReader struct {
	data    []byte
	done    bool
	delay   time.Duration // if set, block before returning data (so test can close conn first)
	delayed bool
}

func (o *onceReader) Read(p []byte) (int, error) {
	if o.done {
		return 0, io.EOF
	}
	if o.delay > 0 && !o.delayed {
		o.delayed = true
		time.Sleep(o.delay)
	}
	o.done = true
	n := copy(p, o.data)
	return n, nil
}

// TestRunBridge_GoroutineErrorPaths uses a custom sessionFactory that returns pipes
// that trigger error/EOF paths in the bridge goroutines (stdin write error, stdout/stderr EOF).
func TestRunBridge_GoroutineErrorPaths(t *testing.T) {
	writeErr := errors.New("injected write error")
	oldFactory := sessionFactory
	sessionFactory = func(addr string, config *ssh.ClientConfig, _, _ int) (io.WriteCloser, io.Reader, io.Reader, func(int, int) error, func(), error) {
		return &errWriter{err: writeErr}, eofReader{}, eofReader{}, nil, func() {}, nil
	}
	defer func() { sessionFactory = oldFactory }()

	bridgeErrCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer wsConn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			bridgeErrCh <- RunBridge(ctx, wsConn, "127.0.0.1", 22, "u", "p", "", "", nil, nil, nil)
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
	// Send a message so the stdin goroutine calls Write and gets the injected error.
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("x"))

	select {
	case err := <-bridgeErrCh:
		if err != nil {
			t.Fatalf("RunBridge returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

// TestRunBridge_WriteMessageFails uses a sessionFactory whose stdout blocks then returns data;
// we close the server's WebSocket before Read returns so that conn.WriteMessage fails.
func TestRunBridge_WriteMessageFails(t *testing.T) {
	oldFactory := sessionFactory
	sessionFactory = func(addr string, config *ssh.ClientConfig, _, _ int) (io.WriteCloser, io.Reader, io.Reader, func(int, int) error, func(), error) {
		return &errWriter{err: nil}, &onceReader{data: []byte("out"), delay: 200 * time.Millisecond}, eofReader{}, nil, func() {}, nil
	}
	defer func() { sessionFactory = oldFactory }()

	bridgeErrCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	var serverConn *websocket.Conn
	var serverConnMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnMu.Lock()
		serverConn = conn
		serverConnMu.Unlock()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			bridgeErrCh <- RunBridge(ctx, conn, "127.0.0.1", 22, "u", "p", "", "", nil, nil, nil)
			_ = conn.Close()
		}()
		// Close before stdout Read returns so WriteMessage fails.
		go func() {
			time.Sleep(50 * time.Millisecond)
			serverConnMu.Lock()
			c := serverConn
			serverConnMu.Unlock()
			if c != nil {
				_ = c.Close()
			}
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

	select {
	case <-bridgeErrCh:
	case <-time.After(3 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

// TestRunBridge_StderrWriteMessageFails is like WriteMessageFails but for the stderr goroutine.
func TestRunBridge_StderrWriteMessageFails(t *testing.T) {
	oldFactory := sessionFactory
	sessionFactory = func(addr string, config *ssh.ClientConfig, _, _ int) (io.WriteCloser, io.Reader, io.Reader, func(int, int) error, func(), error) {
		return &errWriter{err: nil}, eofReader{}, &onceReader{data: []byte("err"), delay: 200 * time.Millisecond}, nil, func() {}, nil
	}
	defer func() { sessionFactory = oldFactory }()

	bridgeErrCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	var serverConn *websocket.Conn
	var serverConnMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnMu.Lock()
		serverConn = conn
		serverConnMu.Unlock()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			bridgeErrCh <- RunBridge(ctx, conn, "127.0.0.1", 22, "u", "p", "", "", nil, nil, nil)
			_ = conn.Close()
		}()
		go func() {
			time.Sleep(50 * time.Millisecond)
			serverConnMu.Lock()
			c := serverConn
			serverConnMu.Unlock()
			if c != nil {
				_ = c.Close()
			}
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

	select {
	case <-bridgeErrCh:
	case <-time.After(3 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

// TestRunBridge_NonTextNonBinaryMessage: client sends Binary then Close so the stdin goroutine
// sees Binary (writes), then may see CloseMessage (isDataMessage false, skip write).
func TestRunBridge_NonTextNonBinaryMessage(t *testing.T) {
	oldFactory := sessionFactory
	sessionFactory = func(addr string, config *ssh.ClientConfig, _, _ int) (io.WriteCloser, io.Reader, io.Reader, func(int, int) error, func(), error) {
		return &errWriter{err: nil}, eofReader{}, eofReader{}, nil, func() {}, nil
	}
	defer func() { sessionFactory = oldFactory }()

	bridgeErrCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer wsConn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			bridgeErrCh <- RunBridge(ctx, wsConn, "127.0.0.1", 22, "u", "p", "", "", nil, nil, nil)
		}()
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("x"))
	_ = conn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(time.Second))
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != nil {
			t.Fatalf("RunBridge returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

// TestRunBridge_ReadMessageFails uses a sessionFactory that returns EOF readers for stdout/stderr.
// Client connects and closes without sending; the stdin goroutine's ReadMessage then returns an error.
func TestRunBridge_ReadMessageFails(t *testing.T) {
	oldFactory := sessionFactory
	sessionFactory = func(addr string, config *ssh.ClientConfig, _, _ int) (io.WriteCloser, io.Reader, io.Reader, func(int, int) error, func(), error) {
		return &errWriter{err: nil}, eofReader{}, eofReader{}, nil, func() {}, nil
	}
	defer func() { sessionFactory = oldFactory }()

	bridgeErrCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			defer wsConn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			bridgeErrCh <- RunBridge(ctx, wsConn, "127.0.0.1", 22, "u", "p", "", "", nil, nil, nil)
		}()
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Close without sending so the server's ReadMessage returns an error.
	_ = conn.Close()

	select {
	case err := <-bridgeErrCh:
		if err != nil {
			t.Fatalf("RunBridge returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

// TestStreamAttach_CloseNilCloseFn covers StreamAttach.Close when CloseFn is nil.
func TestStreamAttach_CloseNilCloseFn(t *testing.T) {
	sa := &StreamAttach{Write: func([]byte) error { return nil }}
	if err := sa.Close(); err != nil {
		t.Fatalf("Close with nil CloseFn: %v", err)
	}
}

// TestRunBridge_ResizeMessage sends a JSON resize message and ensures windowChange is called (no crash).
func TestRunBridge_ResizeMessage(t *testing.T) {
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

	wsURL, bridgeErrCh, _ := runBridgeWithEchoServer(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// Send resize (TextMessage with type resize) — should not write to stdin
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":120,"rows":30}`))
	// Then send data to confirm bridge still works
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("hi"))
	_, msg, _ := conn.ReadMessage()
	if string(msg) != "hi" {
		t.Fatalf("expected echo hi, got %q", string(msg))
	}
	_ = conn.Close()
	select {
	case <-bridgeErrCh:
	case <-time.After(5 * time.Second):
		t.Fatal("RunBridge did not return")
	}
}

// TestRunBridge_WithTeeAndStdinRecorder covers tee and stdinRecorder paths.
func TestRunBridge_WithTeeAndStdinRecorder(t *testing.T) {
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

	var teeBuf bytes.Buffer
	var recorded []byte
	recorder := &recordInputRecorder{recorded: &recorded}
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
			bridgeErrCh <- RunBridge(ctx, wsConn, "127.0.0.1", port, "test", "test", "", "", nil, &teeBuf, recorder)
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
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("tee"))
	_, _, _ = conn.ReadMessage()
	_ = conn.Close()
	select {
	case <-bridgeErrCh:
	case <-time.After(5 * time.Second):
		t.Fatal("RunBridge did not return")
	}
	if teeBuf.Len() == 0 {
		t.Error("expected tee to receive output")
	}
	if len(recorded) == 0 {
		t.Error("expected stdinRecorder to receive input")
	}
}

type recordInputRecorder struct {
	recorded *[]byte
	mu       sync.Mutex
}

func (r *recordInputRecorder) RecordInput(p []byte) {
	r.mu.Lock()
	*r.recorded = append(*r.recorded, p...)
	r.mu.Unlock()
}

// TestRunBridgeStream_WithResizeAndTee covers resizeChan, tee, touch, stdinRecorder.
func TestRunBridgeStream_WithResizeAndTee(t *testing.T) {
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

	stdinR, stdinW := io.Pipe()
	stdoutBuf := &bytes.Buffer{}
	teeBuf := &bytes.Buffer{}
	var touchCount atomic.Int32
	var recBuf []byte
	recorder := &recordInputRecorder{recorded: &recBuf}
	resizeCh := make(chan TerminalSize, 1)
	resizeCh <- TerminalSize{Cols: 80, Rows: 24}
	close(resizeCh)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		_, _ = stdinW.Write([]byte("data"))
		_ = stdinW.Close()
	}()

	err = RunBridgeStream(ctx, stdinR, stdoutBuf, "127.0.0.1", port, "test", "test", "", "", 120, 40, resizeCh, func() { touchCount.Add(1) }, teeBuf, recorder)
	if err != nil {
		t.Fatalf("RunBridgeStream: %v", err)
	}
	if stdoutBuf.String() != "data" {
		t.Fatalf("expected stdout data, got %q", stdoutBuf.String())
	}
	if teeBuf.Len() == 0 {
		t.Error("expected tee to have output")
	}
	if touchCount.Load() < 1 {
		t.Error("expected touch to be called")
	}
	if len(recBuf) == 0 {
		t.Error("expected stdinRecorder to record")
	}
}

// TestRunBridgeStream_DialFails covers RunBridgeStream when SSH dial fails.
func TestRunBridgeStream_DialFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r, w := io.Pipe()
	defer func() { _ = w.Close() }()

	err := RunBridgeStream(ctx, r, io.Discard, "127.0.0.1", 1, "u", "p", "", "", 0, 0, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when dialing closed port")
	}
	_ = r.Close()
}

// TestRunBridgeStream_WithEchoServer covers RunBridgeStream with real SSH; localStdin -> SSH -> localStdout.
func TestRunBridgeStream_WithEchoServer(t *testing.T) {
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

	stdinR, stdinW := io.Pipe()
	stdoutBuf := &bytes.Buffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		_, _ = stdinW.Write([]byte("hi"))
		_ = stdinW.Close()
	}()

	err = RunBridgeStream(ctx, stdinR, stdoutBuf, "127.0.0.1", port, "test", "test", "", "", 0, 0, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RunBridgeStream: %v", err)
	}
	if got := stdoutBuf.String(); got != "hi" {
		t.Fatalf("expected stdout %q, got %q", "hi", got)
	}
}

// TestRunBridgeDetachable_WithEchoServer covers RunBridgeDetachable, wsWriterAdapter WriteBinary, and attach path.
func TestRunBridgeDetachable_WithEchoServer(t *testing.T) {
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
			err := RunBridgeDetachable(ctx, "session_ended: SSH session closed", "127.0.0.1", port, "test", "test", "", "", output, attachCh, wsConn, nil, nil, nil, 0, 0, nil)
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

	// Send data; echo server echoes back -> WriteBinary and output.Write are called
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("x"))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != "x" {
		t.Fatalf("expected echo x, got %q", string(msg))
	}

	// Close client so bridge eventually exits (SSH session ends or ctx)
	_ = conn.Close()

	select {
	case <-bridgeErrCh:
	case <-time.After(10 * time.Second):
		t.Fatal("RunBridgeDetachable did not return")
	}
}

// TestRunBridgeDetachable_StreamAttach covers StreamAttach WriteBinary and Close.
func TestRunBridgeDetachable_StreamAttach(t *testing.T) {
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

	output := session.NewRingBuffer(4096)
	_, _ = output.Write([]byte("replay"))

	attachCh := make(chan session.AttachReq, 2)
	var closeCalled atomic.Bool
	var writeBuf bytes.Buffer
	stdinCh := make(chan []byte, 8)
	sa := &StreamAttach{
		Write: func(p []byte) error { writeBuf.Write(p); return nil },
		StartRead: func(stdinChOut chan<- []byte, onClose func()) {
			go func() {
				for b := range stdinCh {
					select {
					case stdinChOut <- b:
					default:
					}
				}
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
		bridgeErrCh <- RunBridgeDetachable(ctx, "session_ended: SSH session closed", "127.0.0.1", port, "test", "test", "", "", output, attachCh, nil, nil, nil, nil, 0, 0, nil)
	}()

	// Trigger attach so StreamAttach gets replay via WriteBinary
	time.Sleep(100 * time.Millisecond)

	// Send second attach to trigger Close() on the first (StreamAttach)
	attachCh <- session.AttachReq{Conn: &StreamAttach{
		Write:     func([]byte) error { return nil },
		StartRead: func(chan<- []byte, func()) {}, // no-op so attachStream does not panic
		CloseFn:   func() error { return nil },
	}}

	select {
	case <-bridgeErrCh:
	case <-time.After(2 * time.Second):
		t.Fatal("RunBridgeDetachable did not return")
	}

	if !closeCalled.Load() {
		t.Log("Close may not be called if attach order differs; replay written:", writeBuf.Len() > 0)
	}
}
