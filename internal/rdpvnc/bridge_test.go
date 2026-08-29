package rdpvnc

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateRDPInputs(t *testing.T) {
	if err := validateRDPInputs("example.com", 3389, "user", "pass", 1024, 768); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if err := validateRDPInputs("", 3389, "user", "pass", 1024, 768); err == nil {
		t.Fatal("expected error for empty host")
	}
	if err := validateRDPInputs("example.com", 0, "user", "pass", 1024, 768); err == nil {
		t.Fatal("expected error for invalid port")
	}
	if err := validateRDPInputs("example.com", 3389, "u\nser", "pass", 1024, 768); err == nil {
		t.Fatal("expected error for control chars in username")
	}
	if err := validateRDPInputs("example.com", 3389, "user", "pa\rss", 1024, 768); err == nil {
		t.Fatal("expected error for control chars in password")
	}
}

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

func TestBridgeStopZeroValueDoesNotPanic(t *testing.T) {
	// Zero-value Bridge used in some HTTP tests as a placeholder.
	// Stop should be safe even when cancel/done are nil.
	b := &Bridge{}
	b.Stop()
}

func TestManagerRegisterSessionGetListRemove(t *testing.T) {
	m := NewManager()
	done := make(chan struct{})
	_, cancel := context.WithCancel(context.Background())
	b := &Bridge{done: done, cancel: cancel, vncPort: 5901, width: 1920, height: 1080}

	s := m.RegisterSession("u:t1", "sid1", "u", "t1", "T1", 1920, 1080, b)
	if s == nil || s.ID != "sid1" || s.UserID != "u" || s.TargetID != "t1" {
		t.Fatalf("unexpected session: %+v", s)
	}
	if got, ok := m.GetSession("sid1"); !ok || got.ID != "sid1" {
		t.Fatalf("GetSession failed: ok=%v got=%+v", ok, got)
	}
	if got, ok := m.GetSessionByKey("u:t1"); !ok || got.ID != "sid1" {
		t.Fatalf("GetSessionByKey failed: ok=%v got=%+v", ok, got)
	}
	list := m.ActiveSessionsForUser("u")
	if len(list) != 1 || list[0].ID != "sid1" {
		t.Fatalf("expected 1 active session sid1, got %+v", list)
	}

	m.RemoveSession("sid1")
	if _, ok := m.GetSession("sid1"); ok {
		t.Fatal("expected session removed")
	}
	if _, ok := m.GetSessionByKey("u:t1"); ok {
		t.Fatal("expected key mapping removed")
	}
}

func TestSessionStartBridgeProxyOnce(t *testing.T) {
	m := NewManager()
	done := make(chan struct{})
	_, cancel := context.WithCancel(context.Background())
	b := &Bridge{done: done, cancel: cancel, vncPort: 5902}
	s := m.RegisterSession("u:t3", "sid3", "u", "t3", "T3", 1024, 768, b)

	var calls int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.StartBridgeProxyOnce(func() {
				atomic.AddInt32(&calls, 1)
			})
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("expected StartBridgeProxyOnce to run exactly once across concurrent attaches, got %d calls", calls)
	}
}

func TestManagerRegisterSessionAutoCleanupOnDone(t *testing.T) {
	m := NewManager()
	done := make(chan struct{})
	_, cancel := context.WithCancel(context.Background())
	b := &Bridge{done: done, cancel: cancel}

	_ = m.RegisterSession("u:t2", "sid2", "u", "t2", "T2", 800, 600, b)
	close(done)
	time.Sleep(50 * time.Millisecond)

	if _, ok := m.GetSession("sid2"); ok {
		t.Fatal("expected session to be auto-cleaned after done closed")
	}
	if _, ok := m.GetSessionByKey("u:t2"); ok {
		t.Fatal("expected key mapping to be auto-cleaned after done closed")
	}
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
