package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestManager_StartAndStopSession(t *testing.T) {
	m := NewManager()

	var ran atomic.Bool

	sess, err := m.Start("s1", func(ctx context.Context) {
		ran.Store(true)
		<-ctx.Done()
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if sess == nil {
		t.Fatalf("expected non-nil session")
	}

	if got := len(m.ActiveIDs()); got != 1 {
		t.Fatalf("expected 1 active session, got %d", got)
	}

	m.Stop("s1")

	if got := len(m.ActiveIDs()); got != 0 {
		t.Fatalf("expected 0 active sessions after stop, got %d", got)
	}
	if !ran.Load() {
		t.Fatalf("expected session function to run")
	}
}

func TestManager_StartDuplicateSessionFails(t *testing.T) {
	m := NewManager()

	_, err := m.Start("dup", func(ctx context.Context) {})
	if err != nil {
		t.Fatalf("first Start returned error: %v", err)
	}

	if _, err := m.Start("dup", func(ctx context.Context) {}); err == nil {
		t.Fatalf("expected error for duplicate session ID, got nil")
	}
}

func TestManager_TouchUpdatesLastSeen(t *testing.T) {
	m := NewManager()
	now := time.Now()
	m.now = func() time.Time { return now }

	sess, err := m.Start("touch", func(ctx context.Context) {
		<-ctx.Done()
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if !sess.lastSeen.Equal(now) {
		t.Fatalf("expected lastSeen to be %v, got %v", now, sess.lastSeen)
	}

	later := now.Add(10 * time.Second)
	m.now = func() time.Time { return later }

	m.Touch("touch")

	if !sess.lastSeen.Equal(later) {
		t.Fatalf("expected lastSeen to be %v after Touch, got %v", later, sess.lastSeen)
	}

	m.Stop("touch")
}
