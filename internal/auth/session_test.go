package auth

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestSQLiteSessionStore(t *testing.T) *SQLiteSessionStore {
	t.Helper()
	_, store := newTestSQLiteSessionStoreWithDB(t)
	return store
}

// newTestSQLiteSessionStoreWithDB returns db (for raw SQL in tests) and store.
func newTestSQLiteSessionStoreWithDB(t *testing.T) (*sql.DB, *SQLiteSessionStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	userStore := NewSQLiteUserStore(db)
	if _, err := userStore.CreateUser("user1", "user1", "Password1!", ""); err != nil && err != ErrUserExists {
		t.Fatalf("create user: %v", err)
	}
	return db, NewSQLiteSessionStore(db, 5*time.Minute)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("injected read error") }

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

func TestSQLiteSessionStore_Create_EmptyUserID(t *testing.T) {
	store := newTestSQLiteSessionStore(t)
	_, err := store.Create("")
	if err == nil {
		t.Fatal("expected error for empty userID")
	}
}

func TestSQLiteSessionStore_Create_RandomIDError(t *testing.T) {
	store := newTestSQLiteSessionStore(t)
	old := randReader
	defer func() { randReader = old }()
	randReader = errReader{}
	_, err := store.Create("user1")
	if err == nil {
		t.Fatal("expected error when randomID fails")
	}
}

func TestSQLiteSessionStore_Create_InvalidUserID(t *testing.T) {
	store := newTestSQLiteSessionStore(t)
	_, err := store.Create("nonexistent-user-id")
	if err == nil {
		t.Fatal("expected error for invalid user ID (FK)")
	}
}

func TestSQLiteSessionStore_Get_NotFound(t *testing.T) {
	store := newTestSQLiteSessionStore(t)
	_, err := store.Get("nonexistent-session-id")
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSQLiteSessionStore_Delete(t *testing.T) {
	store := newTestSQLiteSessionStore(t)
	sess, err := store.Create("user1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store.Delete(sess.ID)
	_, err = store.Get(sess.ID)
	if err != ErrSessionNotFound {
		t.Fatalf("after Delete expected ErrSessionNotFound, got %v", err)
	}
}

func TestSQLiteSessionStore_Get_ExpiredSession(t *testing.T) {
	db, store := newTestSQLiteSessionStoreWithDB(t)
	sess, err := store.Create("user1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	// sess.ID is the raw token; the DB row is keyed by its SHA-256
	// hash (CWE-312). Hash here so the test can target it directly.
	_, err = db.Exec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, past, hashSessionToken(sess.ID))
	if err != nil {
		t.Fatalf("update expires_at: %v", err)
	}
	_, err = store.Get(sess.ID)
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound for expired session, got %v", err)
	}
}

func TestSQLiteSessionStore_Get_DBError(t *testing.T) {
	db, store := newTestSQLiteSessionStoreWithDB(t)
	sess, err := store.Create("user1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = db.Close()
	_, err = store.Get(sess.ID)
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}
