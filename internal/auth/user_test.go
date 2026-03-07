package auth

import (
	"database/sql"
	"path/filepath"
	"testing"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestSQLiteUserStore(t *testing.T) *SQLiteUserStore {
	t.Helper()
	_, store := newTestSQLiteUserStoreWithDB(t)
	return store
}

func newTestSQLiteUserStoreWithDB(t *testing.T) (*sql.DB, *SQLiteUserStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "users.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, NewSQLiteUserStore(db)
}

func TestSQLiteUserStore_CreateAndAuthenticate(t *testing.T) {
	store := newTestSQLiteUserStore(t)

	u, err := store.CreateUser("u1", "alice", "Password1!")
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if u.ID != "u1" || u.Username != "alice" {
		t.Fatalf("unexpected user: %+v", u)
	}

	authed, err := store.Authenticate("alice", "Password1!")
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

	if _, err := store.CreateUser("u1", "bob", "Password1!"); err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if _, err := store.CreateUser("u1", "bob2", "Password2!"); err == nil {
		t.Fatalf("expected error for duplicate ID, got nil")
	}
	if _, err := store.CreateUser("u2", "bob", "Password2!"); err == nil {
		t.Fatalf("expected error for duplicate username, got nil")
	}
}

func TestSQLiteUserStore_GetByID(t *testing.T) {
	store := newTestSQLiteUserStore(t)

	u, err := store.CreateUser("u1", "alice", "Password1!")
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

func TestSQLiteUserStore_CreateUser_EmptyIDOrUsername(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	if _, err := store.CreateUser("", "u", "Password1!"); err == nil {
		t.Fatal("expected error for empty id")
	}
	if _, err := store.CreateUser("id", "", "Password1!"); err == nil {
		t.Fatal("expected error for empty username")
	}
}

func TestSQLiteUserStore_CreateUser_HashError(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	old := bcryptCost
	defer func() { bcryptCost = old }()
	bcryptCost = 32
	_, err := store.CreateUser("u1", "alice", "Alice1!x")
	if err == nil {
		t.Fatal("expected error when hashing fails")
	}
}

func TestSQLiteUserStore_GetByID_NotFound(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	_, err := store.GetByID("nonexistent")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestSQLiteUserStore_Authenticate_UserNotFound(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	_, err := store.Authenticate("nonexistent", "Password1!")
	if err != ErrInvalidSecret {
		t.Fatalf("expected ErrInvalidSecret, got %v", err)
	}
}

func TestSQLiteUserStore_Authenticate_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	if _, err := store.CreateUser("u1", "alice", "Alice1!x"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = db.Close()
	_, err := store.Authenticate("alice", "Alice1!x")
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_GetByID_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	if _, err := store.CreateUser("u1", "alice", "Alice1!x"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = db.Close()
	_, err := store.GetByID("u1")
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_UpdatePassword(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	if _, err := store.CreateUser("u1", "alice", "Alice1!x"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := store.UpdatePassword("u1", "wrong", "NewPass1!"); err != ErrWrongPassword {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
	if err := store.UpdatePassword("u1", "Alice1!x", "Alice1!x"); err != ErrPasswordUnchanged {
		t.Fatalf("expected ErrPasswordUnchanged, got %v", err)
	}
	if err := store.UpdatePassword("u1", "Alice1!x", "short"); err == nil {
		t.Fatal("expected validation error for short password")
	}
	if err := store.UpdatePassword("u1", "Alice1!x", "NewPass1!"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	if _, err := store.Authenticate("alice", "Alice1!x"); err == nil {
		t.Fatal("old password should not work")
	}
	authed, err := store.Authenticate("alice", "NewPass1!")
	if err != nil {
		t.Fatalf("Authenticate with new password: %v", err)
	}
	if authed.ID != "u1" {
		t.Fatalf("expected u1, got %s", authed.ID)
	}
}
