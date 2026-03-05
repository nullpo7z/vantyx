package auth

import (
	"path/filepath"
	"testing"
	"time"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestSQLiteSessionStore(t *testing.T) *SQLiteSessionStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// ensure user exists for FK constraint
	userStore := NewSQLiteUserStore(db)
	if _, err := userStore.CreateUser("user1", "user1", "password"); err != nil && err != ErrUserExists {
		t.Fatalf("create user: %v", err)
	}
	return NewSQLiteSessionStore(db, 5*time.Minute)
}

func TestSQLiteSessionStore_CreateAndGet(t *testing.T) {
	store := newTestSQLiteSessionStore(t)

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
