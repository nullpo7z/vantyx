package rdpvnc

import (
	"testing"
	"time"
)

func TestManager_TouchUpdatesLastSeen(t *testing.T) {
	m := NewManager()
	now := time.Now()
	m.SetNowForTest(func() time.Time { return now })

	b := &Bridge{done: make(chan struct{})}
	sess := m.RegisterSession("u:t", "s1", "u", "t", "T", 1920, 1080, b)

	if !sess.lastSeen.Equal(now) {
		t.Fatalf("expected lastSeen %v, got %v", now, sess.lastSeen)
	}

	later := now.Add(10 * time.Second)
	m.SetNowForTest(func() time.Time { return later })
	m.Touch("s1")

	got, ok := m.GetSession("s1")
	if !ok {
		t.Fatal("session not found")
	}
	if !got.lastSeen.Equal(later) {
		t.Fatalf("expected lastSeen %v after Touch, got %v", later, got.lastSeen)
	}
}

func TestManager_IsIdle(t *testing.T) {
	m := NewManager()
	m.SetIdleWarnAfter(5 * time.Minute)
	now := time.Now()
	m.SetNowForTest(func() time.Time { return now })

	b := &Bridge{done: make(chan struct{})}
	sess := m.RegisterSession("u:t", "idle", "u", "t", "T", 1920, 1080, b)

	if m.IsIdle(sess) {
		t.Fatal("expected not idle immediately after start")
	}

	m.SetNowForTest(func() time.Time { return now.Add(6 * time.Minute) })
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
	m.SetNowForTest(func() time.Time { return now.Add(24 * time.Hour) })

	b := &Bridge{done: make(chan struct{})}
	sess := m.RegisterSession("u:x", "x", "u", "t", "T", 1920, 1080, b)

	if m.IsIdle(sess) {
		t.Fatal("expected IsIdle false when idleWarnAfter is 0")
	}
}
