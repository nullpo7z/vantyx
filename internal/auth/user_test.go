package auth

import "testing"

func TestInMemoryUserStore_CreateAndAuthenticate(t *testing.T) {
	store := NewInMemoryUserStore()

	u, err := store.CreateUser("u1", "alice", "password123")
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if u.ID != "u1" || u.Username != "alice" {
		t.Fatalf("unexpected user: %+v", u)
	}

	authed, err := store.Authenticate("alice", "password123")
	if err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}
	if authed.ID != u.ID {
		t.Fatalf("expected user ID %q, got %q", u.ID, authed.ID)
	}

	if _, err := store.Authenticate("alice", "wrong"); err == nil {
		t.Fatalf("expected error for wrong password, got nil")
	}
}

func TestInMemoryUserStore_DuplicateUser(t *testing.T) {
	store := NewInMemoryUserStore()

	if _, err := store.CreateUser("u1", "bob", "pw"); err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if _, err := store.CreateUser("u1", "bob2", "pw2"); err == nil {
		t.Fatalf("expected error for duplicate ID, got nil")
	}
	if _, err := store.CreateUser("u2", "bob", "pw2"); err == nil {
		t.Fatalf("expected error for duplicate username, got nil")
	}
}
