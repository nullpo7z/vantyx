package access

import (
	"context"
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
	return NewSQLiteAccessGroupStore(db, nil)
}

// newTestSQLiteAccessGroupStoreWithUser ensures a user exists in the same DB (for FK).
func newTestSQLiteAccessGroupStoreWithUser(t *testing.T, userID string) *SQLiteAccessGroupStore {
	t.Helper()
	store := newTestSQLiteAccessGroupStore(t)
	userStore := auth.NewSQLiteUserStore(store.db)
	if _, err := userStore.CreateUser(userID, userID, "Passw0rd!", ""); err != nil && err != auth.ErrUserExists {
		t.Fatalf("create user: %v", err)
	}
	return store
}

func TestSQLiteAccessGroupStore_CreateAndMembership(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteAccessGroupStoreWithUser(t, "user1")

	g, err := store.Create(ctx, "g1", "ops")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if g.ID != "g1" || g.Name != "ops" {
		t.Fatalf("unexpected group: %+v", g)
	}

	if err := store.AddUserToGroup(ctx, "user1", "g1"); err != nil {
		t.Fatalf("AddUserToGroup returned error: %v", err)
	}
	// target1 must exist for group_targets FK; create via TargetStore on same DB
	targetStore := NewSQLiteTargetStore(store.db, nil, nil)
	if _, err := targetStore.CreateWithPath(ctx, "target1", "r1", "192.168.1.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "", "", ""); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := store.AddTargetToGroup(ctx, "g1", "target1"); err != nil {
		t.Fatalf("AddTargetToGroup returned error: %v", err)
	}

	ids, err := store.TargetIDsForUser(ctx, "user1", nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	if len(ids) != 1 || ids[0] != "target1" {
		t.Fatalf("expected [target1], got %v", ids)
	}

	ids, err = store.TargetIDsForUser(ctx, "unknown", nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty for unknown user, got %v", ids)
	}

	userIDs, err := store.UserIDsForGroup(ctx, "g1", nil)
	if err != nil {
		t.Fatalf("UserIDsForGroup: %v", err)
	}
	if len(userIDs) != 1 || string(userIDs[0]) != "user1" {
		t.Fatalf("expected [user1], got %v", userIDs)
	}
	if err := store.RemoveUserFromGroup(ctx, "user1", "g1"); err != nil {
		t.Fatalf("RemoveUserFromGroup: %v", err)
	}
	userIDs2, _ := store.UserIDsForGroup(ctx, "g1", nil)
	if len(userIDs2) != 0 {
		t.Fatalf("expected empty after remove, got %v", userIDs2)
	}
}

func TestSQLiteAccessGroupStore_RemoveUserFromGroup_UnknownGroup(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteAccessGroupStore(t)
	if err := store.RemoveUserFromGroup(ctx, "u1", "missing"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}

func TestSQLiteAccessGroupStore_TargetIDsForUser_Dedup(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteAccessGroupStoreWithUser(t, "u1")

	if _, err := store.Create(ctx, "g1", "ops"); err != nil {
		t.Fatalf("Create g1: %v", err)
	}
	if _, err := store.Create(ctx, "g2", "dev"); err != nil {
		t.Fatalf("Create g2: %v", err)
	}
	targetStore := NewSQLiteTargetStore(store.db, nil, nil)
	if _, err := targetStore.CreateWithPath(ctx, "t1", "r1", "h1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "", "", ""); err != nil {
		t.Fatalf("create target t1: %v", err)
	}
	if _, err := targetStore.CreateWithPath(ctx, "t2", "r2", "h2", 23, ProtocolTelnet, GroupID("g2"), "g2", "", "", "", ""); err != nil {
		t.Fatalf("create target t2: %v", err)
	}
	_ = store.AddUserToGroup(ctx, "u1", "g1")
	_ = store.AddUserToGroup(ctx, "u1", "g2")
	_ = store.AddTargetToGroup(ctx, "g1", "t1")
	_ = store.AddTargetToGroup(ctx, "g2", "t1")
	_ = store.AddTargetToGroup(ctx, "g2", "t2")

	ids, err := store.TargetIDsForUser(ctx, "u1", nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique targets, got %v", ids)
	}
}

func TestSQLiteAccessGroupStore_AddUserToGroup_UnknownGroup(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteAccessGroupStore(t)

	if err := store.AddUserToGroup(ctx, "u1", "missing"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}
