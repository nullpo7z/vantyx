package auth

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

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

	u, err := store.CreateUser("u1", "alice", "Password1!", "")
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

	if _, err := store.CreateUser("u1", "bob", "Password1!", ""); err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if _, err := store.CreateUser("u1", "bob2", "Password2!", ""); err == nil {
		t.Fatalf("expected error for duplicate ID, got nil")
	} else if !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected ErrUserExists for duplicate ID, got %v", err)
	}
	if _, err := store.CreateUser("u2", "bob", "Password2!", ""); err == nil {
		t.Fatalf("expected error for duplicate username, got nil")
	} else if !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected ErrUserExists for duplicate username, got %v", err)
	}
}

func TestSQLiteUserStore_GetByID(t *testing.T) {
	store := newTestSQLiteUserStore(t)

	u, err := store.CreateUser("u1", "alice", "Password1!", "")
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

func TestSQLiteUserStore_CreateUser_RoleAdmin(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	u, err := store.CreateUser("admin2", "admin2", "Password1!", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	if u.Role != RoleAdmin {
		t.Fatalf("expected role admin, got %q", u.Role)
	}
}

func TestSQLiteUserStore_CreateUser_InvalidRoleDefaultsToUser(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	u, err := store.CreateUser("u1", "custom", "Password1!", "superuser")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Role != RoleUser {
		t.Fatalf("expected role user for invalid role, got %q", u.Role)
	}
}

func TestSQLiteUserStore_CreateUser_EmptyRoleDefaultsToUser(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	u, err := store.CreateUser("u1", "norole", "Password1!", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Role != RoleUser {
		t.Fatalf("expected role user, got %q", u.Role)
	}
}

func TestSQLiteUserStore_CreateUser_EmptyIDOrUsername(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	if _, err := store.CreateUser("", "u", "Password1!", ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if _, err := store.CreateUser("id", "", "Password1!", ""); err == nil {
		t.Fatal("expected error for empty username")
	}
}

func TestSQLiteUserStore_CreateUser_HashError(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	old := bcryptCost
	defer func() { bcryptCost = old }()
	bcryptCost = 32
	_, err := store.CreateUser("u1", "alice", "Alice1!x", "")
	if err == nil {
		t.Fatal("expected error when hashing fails")
	}
}

func TestSQLiteUserStore_ListUsers(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	if _, err := store.CreateUser("u1", "alice", "Password1!", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.CreateUser("u2", "bob", "Password1!", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	list, err := store.ListUsers(10, 0)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 users, got %d", len(list))
	}
	// ordered by username: alice, bob
	if list[0].Username != "alice" || list[1].Username != "bob" {
		t.Fatalf("unexpected order: %+v", list)
	}

	list2, err := store.ListUsers(1, 1)
	if err != nil {
		t.Fatalf("ListUsers(1,1): %v", err)
	}
	if len(list2) != 1 || list2[0].Username != "bob" {
		t.Fatalf("expected [bob], got %+v", list2)
	}

	// limit <= 0 -> 100, offset < 0 -> 0
	list3, err := store.ListUsers(0, -1)
	if err != nil {
		t.Fatalf("ListUsers(0,-1): %v", err)
	}
	if len(list3) != 2 {
		t.Fatalf("expected 2 users with default limit, got %d", len(list3))
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
	if _, err := store.CreateUser("u1", "alice", "Alice1!x", ""); err != nil {
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
	if _, err := store.CreateUser("u1", "alice", "Alice1!x", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = db.Close()
	_, err := store.GetByID("u1")
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_ListUsers_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_ = db.Close()
	_, err := store.ListUsers(10, 0)
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_TagsForUser_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_ = db.Close()
	_, err := store.TagsForUser("u1")
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_SetUserTags_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_ = db.Close()
	err := store.SetUserTags("u1", []string{"tag1"})
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_UpdatePassword_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Alice1!x", "")
	_ = db.Close()
	err := store.UpdatePassword("u1", "Alice1!x", "NewPass1!")
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_AddPublicKey_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_ = db.Close()
	_, err := store.AddPublicKey("u1", testAuthorizedKey)
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_ListPublicKeys_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_ = db.Close()
	_, err := store.ListPublicKeys("u1")
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_DeletePublicKey_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	id, _ := store.AddPublicKey("u1", testAuthorizedKey)
	_ = db.Close()
	err := store.DeletePublicKey("u1", id)
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_AuthenticateByPublicKey_DBError(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_, _ = store.AddPublicKey("u1", testAuthorizedKey)
	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(testAuthorizedKey))
	_ = db.Close()
	_, err := store.AuthenticateByPublicKey("alice", pub)
	if err == nil {
		t.Fatal("expected error when db is closed")
	}
}

func TestSQLiteUserStore_UpdatePassword(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	if _, err := store.CreateUser("u1", "alice", "Alice1!x", ""); err != nil {
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

func TestSQLiteUserStore_TagsForUser_SetUserTags(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")

	tags, err := store.TagsForUser("u1")
	if err != nil || len(tags) != 0 {
		t.Fatalf("TagsForUser empty: err=%v tags=%v", err, tags)
	}

	if err := store.SetUserTags("u1", []string{"prod", "ops"}); err != nil {
		t.Fatalf("SetUserTags: %v", err)
	}
	tags, err = store.TagsForUser("u1")
	if err != nil || len(tags) != 2 || tags[0] != "ops" || tags[1] != "prod" {
		t.Fatalf("TagsForUser after set: err=%v tags=%v", err, tags)
	}

	if err := store.SetUserTags("u1", nil); err != nil {
		t.Fatalf("SetUserTags clear: %v", err)
	}
	tags, _ = store.TagsForUser("u1")
	if len(tags) != 0 {
		t.Fatalf("expected empty after clear, got %v", tags)
	}

	if err := store.SetUserTags("", []string{"x"}); err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for empty userID, got %v", err)
	}
	if err := store.SetUserTags("missing", []string{"x"}); err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound for missing user, got %v", err)
	}
	if err := store.SetUserTags("u1", []string{"valid", "bad!"}); err == nil {
		t.Fatal("expected error for invalid tag")
	}
	if err := store.SetUserTags("u1", []string{""}); err == nil {
		t.Fatal("expected error for empty tag")
	}
	if err := store.SetUserTags("u1", []string{strings.Repeat("a", 65)}); err == nil {
		t.Fatal("expected error for tag too long")
	}
}

func TestSQLiteUserStore_AddPublicKey_UserNotFound(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	_, err := store.AddPublicKey("nonexistent", testAuthorizedKey)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// Valid SSH public key line (authorized_keys format) for tests.
const testAuthorizedKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl user@host"

func TestSQLiteUserStore_AddPublicKey_ListPublicKeys_DeletePublicKey(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")

	if _, err := store.AddPublicKey("u1", ""); err != ErrInvalidPublicKey {
		t.Fatalf("empty key: expected ErrInvalidPublicKey, got %v", err)
	}
	if _, err := store.AddPublicKey("u1", "not-a-valid-key"); err != ErrInvalidPublicKey {
		t.Fatalf("invalid key: expected ErrInvalidPublicKey, got %v", err)
	}

	id1, err := store.AddPublicKey("u1", testAuthorizedKey)
	if err != nil {
		t.Fatalf("AddPublicKey: %v", err)
	}
	if id1 <= 0 {
		t.Fatalf("expected positive id, got %d", id1)
	}
	keys, err := store.ListPublicKeys("u1")
	if err != nil || len(keys) != 1 || keys[0].KeyLine != testAuthorizedKey {
		t.Fatalf("ListPublicKeys: err=%v keys=%v", err, keys)
	}
	if err := store.DeletePublicKey("u1", id1); err != nil {
		t.Fatalf("DeletePublicKey: %v", err)
	}
	keys, _ = store.ListPublicKeys("u1")
	if len(keys) != 0 {
		t.Fatalf("expected no keys after delete, got %v", keys)
	}
	if err := store.DeletePublicKey("u1", 999); err != ErrUserNotFound {
		t.Fatalf("delete missing key: expected ErrUserNotFound, got %v", err)
	}
}

func TestSQLiteUserStore_AuthenticateByPublicKey(t *testing.T) {
	store := newTestSQLiteUserStore(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_, _ = store.AddPublicKey("u1", testAuthorizedKey)

	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(testAuthorizedKey))
	if pub == nil {
		t.Fatal("parse test key failed")
	}
	u, err := store.AuthenticateByPublicKey("alice", pub)
	if err != nil || u == nil || u.ID != "u1" {
		t.Fatalf("AuthenticateByPublicKey: err=%v user=%v", err, u)
	}
	if _, err := store.AuthenticateByPublicKey("alice", nil); err != ErrInvalidSecret {
		t.Fatalf("nil key: expected ErrInvalidSecret, got %v", err)
	}
	if _, err := store.AuthenticateByPublicKey("bob", pub); err != ErrInvalidSecret {
		t.Fatalf("wrong user: expected ErrInvalidSecret, got %v", err)
	}
}

func TestSQLiteUserStore_AuthenticateByPublicKey_SkipsInvalidStoredKey(t *testing.T) {
	db, store := newTestSQLiteUserStoreWithDB(t)
	_, _ = store.CreateUser("u1", "alice", "Password1!", "")
	_, _ = store.AddPublicKey("u1", testAuthorizedKey)
	// Insert a corrupt key line that will fail to parse; AuthenticateByPublicKey should skip it and still match the valid key.
	_, err := db.Exec(`INSERT INTO user_ssh_keys (user_id, key_line) VALUES ('u1', 'corrupt-key-line')`)
	if err != nil {
		t.Fatalf("insert corrupt key: %v", err)
	}
	pub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(testAuthorizedKey))
	if pub == nil {
		t.Fatal("parse test key failed")
	}
	u, err := store.AuthenticateByPublicKey("alice", pub)
	if err != nil || u == nil || u.ID != "u1" {
		t.Fatalf("AuthenticateByPublicKey: err=%v user=%v", err, u)
	}
}
