package access

import (
	"path/filepath"
	"testing"

	"github.com/nullpo7z/vantyx/internal/auth"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestSQLiteAccessGroupStore(t *testing.T) *SQLiteAccessGroupStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "groups.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewSQLiteAccessGroupStore(db)
}

// newTestSQLiteAccessGroupStoreWithUser ensures a user exists in the same DB (for FK).
func newTestSQLiteAccessGroupStoreWithUser(t *testing.T, userID string) *SQLiteAccessGroupStore {
	t.Helper()
	store := newTestSQLiteAccessGroupStore(t)
	userStore := auth.NewSQLiteUserStore(store.db)
	if _, err := userStore.CreateUser(userID, userID, "pw"); err != nil && err != auth.ErrUserExists {
		t.Fatalf("create user: %v", err)
	}
	return store
}

func TestSQLiteAccessGroupStore_CreateAndMembership(t *testing.T) {
	store := newTestSQLiteAccessGroupStoreWithUser(t, "user1")

	g, err := store.Create("g1", "ops")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if g.ID != "g1" || g.Name != "ops" {
		t.Fatalf("unexpected group: %+v", g)
	}

	if err := store.AddUserToGroup("user1", "g1"); err != nil {
		t.Fatalf("AddUserToGroup returned error: %v", err)
	}
	// target1 must exist for group_targets FK; create via TargetStore on same DB
	targetStore := NewSQLiteTargetStore(store.db)
	if _, err := targetStore.CreateWithPath("target1", "r1", "192.168.1.1", 22, ProtocolSSH, "g1"); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := store.AddTargetToGroup("g1", "target1"); err != nil {
		t.Fatalf("AddTargetToGroup returned error: %v", err)
	}

	ids := store.TargetIDsForUser("user1")
	if len(ids) != 1 || ids[0] != "target1" {
		t.Fatalf("expected [target1], got %v", ids)
	}

	ids = store.TargetIDsForUser("unknown")
	if len(ids) != 0 {
		t.Fatalf("expected nil/empty for unknown user, got %v", ids)
	}
}

func TestSQLiteAccessGroupStore_TargetIDsForUser_Dedup(t *testing.T) {
	store := newTestSQLiteAccessGroupStoreWithUser(t, "u1")

	if _, err := store.Create("g1", "ops"); err != nil {
		t.Fatalf("Create g1: %v", err)
	}
	if _, err := store.Create("g2", "dev"); err != nil {
		t.Fatalf("Create g2: %v", err)
	}
	targetStore := NewSQLiteTargetStore(store.db)
	if _, err := targetStore.CreateWithPath("t1", "r1", "h1", 22, ProtocolSSH, "g1"); err != nil {
		t.Fatalf("create target t1: %v", err)
	}
	if _, err := targetStore.CreateWithPath("t2", "r2", "h2", 23, ProtocolTelnet, "g2"); err != nil {
		t.Fatalf("create target t2: %v", err)
	}
	_ = store.AddUserToGroup("u1", "g1")
	_ = store.AddUserToGroup("u1", "g2")
	_ = store.AddTargetToGroup("g1", "t1")
	_ = store.AddTargetToGroup("g2", "t1")
	_ = store.AddTargetToGroup("g2", "t2")

	ids := store.TargetIDsForUser("u1")
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique targets, got %v", ids)
	}
}

func TestSQLiteAccessGroupStore_AddUserToGroup_UnknownGroup(t *testing.T) {
	store := newTestSQLiteAccessGroupStore(t)

	if err := store.AddUserToGroup("u1", "missing"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}
