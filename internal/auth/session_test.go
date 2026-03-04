package auth

import (
	"testing"
	"time"
)

func TestInMemorySessionStore_CreateAndGet(t *testing.T) {
	store := NewInMemorySessionStore(5 * time.Minute)

	sess, err := store.Create("user1")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if sess.UserID != "user1" {
		t.Fatalf("expected userID %q, got %q", "user1", sess.UserID)
	}

	got, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("expected session ID %q, got %q", sess.ID, got.ID)
	}
}

func TestInMemorySessionStore_Expired(t *testing.T) {
	store := NewInMemorySessionStore(1 * time.Second)
	store.now = func() time.Time { return time.Unix(0, 0) }

	sess, err := store.Create("user1")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	// advance time beyond ttl
	store.now = func() time.Time { return time.Unix(10, 0) }

	if _, err := store.Get(sess.ID); err == nil {
		t.Fatalf("expected error for expired session, got nil")
	}
}
