package auth

import (
	"path/filepath"
	"testing"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestSQLiteUserStore(t *testing.T) *SQLiteUserStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "users.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewSQLiteUserStore(db)
}

func TestSQLiteUserStore_CreateAndAuthenticate(t *testing.T) {
	store := newTestSQLiteUserStore(t)

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

func TestSQLiteUserStore_DuplicateUser(t *testing.T) {
	store := newTestSQLiteUserStore(t)

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

func TestSQLiteUserStore_GetByID(t *testing.T) {
	store := newTestSQLiteUserStore(t)

	u, err := store.CreateUser("u1", "alice", "password123")
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}

	got, err := store.GetByID("u1")
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if got.ID != u.ID || got.Username != u.Username {
		t.Fatalf("unexpected user from GetByID: %+v", got)
	}
}
