package rdpvnc

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNextDisplay(t *testing.T) {
	d1 := nextDisplay()
	d2 := nextDisplay()
	if d2 <= d1 {
		t.Fatalf("expected d2 > d1, got d1=%d d2=%d", d1, d2)
	}
}

func TestFreePort(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if port <= 0 {
		t.Fatalf("expected positive port, got %d", port)
	}
}

func TestManagerRegisterRemove(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	m.Remove("nonexistent")
	m.StopAll()
}

func TestManagerRegisterAndAutoCleanup(t *testing.T) {
	m := NewManager()
	done := make(chan struct{})
	b := &Bridge{done: done, vncPort: 1234}
	m.Register("key1", b)
	if b.VNCPort() != 1234 {
		t.Fatalf("expected VNCPort=1234, got %d", b.VNCPort())
	}
	close(done)
	time.Sleep(50 * time.Millisecond)
	m.mu.Lock()
	_, exists := m.bridges["key1"]
	m.mu.Unlock()
	if exists {
		t.Fatal("expected bridge to be auto-cleaned after done closed")
	}
}

func TestManagerRegisterReplaces(t *testing.T) {
	m := NewManager()
	done1 := make(chan struct{})
	close(done1)
	_, cancel1 := context.WithCancel(context.Background())
	b1 := &Bridge{done: done1, vncPort: 1, cancel: cancel1}
	m.Register("k", b1)

	done2 := make(chan struct{})
	_, cancel2 := context.WithCancel(context.Background())
	b2 := &Bridge{done: done2, vncPort: 2, cancel: cancel2}
	m.Register("k", b2)

	m.mu.Lock()
	cur := m.bridges["k"]
	m.mu.Unlock()
	if cur != b2 {
		t.Fatal("expected replacement bridge")
	}
	close(done2)
	time.Sleep(50 * time.Millisecond)
}

func TestManagerStopAll(t *testing.T) {
	m := NewManager()
	done1 := make(chan struct{})
	close(done1)
	_, c1 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	close(done2)
	_, c2 := context.WithCancel(context.Background())
	m.Register("a", &Bridge{done: done1, cancel: c1})
	m.Register("b", &Bridge{done: done2, cancel: c2})
	m.StopAll()
	m.mu.Lock()
	n := len(m.bridges)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected 0 bridges after StopAll, got %d", n)
	}
}

func TestManagerRemoveExisting(t *testing.T) {
	m := NewManager()
	done := make(chan struct{})
	close(done)
	_, c := context.WithCancel(context.Background())
	m.Register("x", &Bridge{done: done, cancel: c})
	m.Remove("x")
	m.mu.Lock()
	_, exists := m.bridges["x"]
	m.mu.Unlock()
	if exists {
		t.Fatal("expected bridge to be removed")
	}
}

func TestBridgeDone(t *testing.T) {
	done := make(chan struct{})
	b := &Bridge{done: done}
	select {
	case <-b.Done():
		t.Fatal("expected Done not to fire yet")
	default:
	}
	close(done)
	select {
	case <-b.Done():
	case <-time.After(time.Second):
		t.Fatal("expected Done to fire")
	}
}

func TestBridgeStopNilProcesses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	close(done)
	b := &Bridge{cancel: cancel, done: done}
	b.Stop()
	_ = ctx
}

func TestKillProcNil(t *testing.T) {
	killProc(nil)
}

func TestWaitForPortSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	ctx := context.Background()
	if err := waitForPort(ctx, port, 2*time.Second); err != nil {
		t.Fatalf("waitForPort: %v", err)
	}
}

func TestWaitForPortTimeout(t *testing.T) {
	err := waitForPort(context.Background(), 1, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForPortContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForPort(ctx, 1, 5*time.Second)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestWaitForDisplayTimeout(t *testing.T) {
	err := waitForDisplay(context.Background(), ":99999", 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForDisplayContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForDisplay(ctx, ":99999", 5*time.Second)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestStartFailsWithoutXvfb(t *testing.T) {
	if _, err := Start(context.Background(), "127.0.0.1", 3389, "", "", 800, 600); err == nil {
		t.Fatal("expected Start to fail when Xvfb is not available")
	}
}
