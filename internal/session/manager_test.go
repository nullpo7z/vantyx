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

	sess, err := m.Start("s1", StartOptions{}, func(ctx context.Context, _ *Session) {
		ran.Store(true)
		<-ctx.Done()
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if sess == nil {
		t.Fatalf("expected non-nil session")
	}

	// Cover Get, ID, CreatedAt
	if got := sess.ID(); got != "s1" {
		t.Fatalf("sess.ID() = %q, want s1", got)
	}
	if sess.CreatedAt().IsZero() {
		t.Fatalf("expected CreatedAt to be set")
	}
	gotSess, ok := m.Get("s1")
	if !ok || gotSess != sess {
		t.Fatalf("Get(s1) = %v, %v; want sess, true", gotSess, ok)
	}
	_, ok = m.Get("nonexistent")
	if ok {
		t.Fatalf("Get(nonexistent) should return false")
	}

	if got := len(m.ActiveIDs()); got != 1 {
		t.Fatalf("expected 1 active session, got %d", got)
	}
	active := m.ActiveSessionsForUser("")
	if len(active) != 1 {
		t.Fatalf("ActiveSessionsForUser(\"\") = %d, want 1", len(active))
	}
	active = m.ActiveSessionsForUser("alice")
	if len(active) != 0 {
		t.Fatalf("ActiveSessionsForUser(alice) = %d, want 0", len(active))
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

	_, err := m.Start("dup", StartOptions{}, func(ctx context.Context, _ *Session) {})
	if err != nil {
		t.Fatalf("first Start returned error: %v", err)
	}

	if _, err := m.Start("dup", StartOptions{}, func(ctx context.Context, _ *Session) {}); err == nil {
		t.Fatalf("expected error for duplicate session ID, got nil")
	}
}

func TestManager_TouchUpdatesLastSeen(t *testing.T) {
	m := NewManager()
	now := time.Now()
	m.now = func() time.Time { return now }

	sess, err := m.Start("touch", StartOptions{}, func(ctx context.Context, _ *Session) {
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

func TestManager_StopUnknownIDNoOp(t *testing.T) {
	m := NewManager()
	// Stop with non-existent ID must not block or panic.
	m.Stop("nonexistent")
	if n := len(m.ActiveIDs()); n != 0 {
		t.Fatalf("expected 0 active sessions, got %d", n)
	}
}

func TestManager_IsIdle(t *testing.T) {
	m := NewManager()
	m.SetIdleWarnAfter(5 * time.Minute)
	now := time.Now()
	m.now = func() time.Time { return now }

	sess, err := m.Start("idle", StartOptions{}, func(ctx context.Context, _ *Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop("idle")

	if m.IsIdle(sess) {
		t.Fatal("expected not idle immediately after start")
	}

	m.now = func() time.Time { return now.Add(6 * time.Minute) }
	if !m.IsIdle(sess) {
		t.Fatal("expected idle after threshold")
	}

	m.Touch("idle")
	if m.IsIdle(sess) {
		t.Fatal("expected not idle after Touch")
	}
}

func TestManager_IdleWarnDisabled(t *testing.T) {
	m := NewManager()
	now := time.Now()
	m.now = func() time.Time { return now.Add(24 * time.Hour) }

	sess, err := m.Start("x", StartOptions{}, func(ctx context.Context, _ *Session) { <-ctx.Done() })
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop("x")

	if m.IsIdle(sess) {
		t.Fatal("expected IsIdle false when idleWarnAfter is 0")
	}
}
