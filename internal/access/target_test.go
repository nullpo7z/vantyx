package access

import (
	"context"
	"path/filepath"
	"testing"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestSQLiteTargetStore(t *testing.T) *SQLiteTargetStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "targets.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// FK: targets.group_id references access_groups; create one group.
	groupStore := NewSQLiteAccessGroupStore(db, nil)
	if _, err := groupStore.Create(context.Background(), "g1", "default"); err != nil {
		t.Fatalf("create group: %v", err)
	}
	return NewSQLiteTargetStore(db, nil, nil)
}

func TestSQLiteTargetStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteTargetStore(t)

	target, err := store.CreateWithPath(ctx, "t1", "router1", "192.168.1.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "", "", "")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if target.ID != "t1" || target.Name != "router1" || target.Host != "192.168.1.1" || target.Port != 22 || target.Protocol != ProtocolSSH {
		t.Fatalf("unexpected target: %+v", target)
	}

	got, err := store.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.ID != target.ID {
		t.Fatalf("Get returned wrong target: %+v", got)
	}

	_, err = store.Get(ctx, "missing")
	if err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestSQLiteTargetStore_Create_Duplicate(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteTargetStore(t)

	if _, err := store.CreateWithPath(ctx, "t1", "r1", "host", 22, ProtocolSSH, GroupID("g1"), "g1", "", "", "", ""); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := store.CreateWithPath(ctx, "t1", "r2", "host2", 23, ProtocolTelnet, GroupID("g1"), "g1", "", "", "", ""); err != ErrTargetExists {
		t.Fatalf("expected ErrTargetExists, got %v", err)
	}
}

func TestSQLiteTargetStore_ListByIDs(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteTargetStore(t)

	_, _ = store.CreateWithPath(ctx, "t1", "r1", "h1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "", "", "")
	_, _ = store.CreateWithPath(ctx, "t2", "r2", "h2", 23, ProtocolTelnet, GroupID("g1"), "g1", "", "", "", "")

	list, err := store.ListByIDs(ctx, []TargetID{"t1", "t2", "missing", "t1"}, nil)
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(list))
	}
}

func TestSQLiteTargetStore_CreateWithPath_ProtocolVNC(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteTargetStore(t)

	target, err := store.CreateWithPath(ctx, "vnc1", "VNC Server", "192.168.1.10", 5900, ProtocolVNC, GroupID("g1"), "g1", "", "", "", "")
	if err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	if target.Protocol != ProtocolVNC || target.Port != 5900 {
		t.Fatalf("unexpected target: %+v", target)
	}
	got, err := store.Get(ctx, "vnc1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Protocol != ProtocolVNC {
		t.Fatalf("Get returned protocol %q, want vnc", got.Protocol)
	}
}

func TestSQLiteTargetStore_CreateWithPath_ProtocolTFTP(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteTargetStore(t)

	target, err := store.CreateWithPath(ctx, "tftp1", "TFTP Server", "192.168.1.20", 69, ProtocolTFTP, GroupID("g1"), "g1", "", "", "", "")
	if err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	if target.Protocol != ProtocolTFTP || target.Port != 69 {
		t.Fatalf("unexpected target: %+v", target)
	}
	got, err := store.Get(ctx, "tftp1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Protocol != ProtocolTFTP {
		t.Fatalf("Get returned protocol %q, want tftp", got.Protocol)
	}
}
