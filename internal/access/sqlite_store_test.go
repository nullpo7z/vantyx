package access

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestSQLiteStores(t *testing.T) (*SQLiteAccessGroupStore, *SQLiteTargetStore) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "access.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewSQLiteAccessGroupStore(db), NewSQLiteTargetStore(db)
}

func TestSQLiteTargetStore_CreateWithPath_Validation(t *testing.T) {
	_, targets := newTestSQLiteStores(t)

	if _, err := targets.CreateWithPath("", "n", "h", 22, ProtocolSSH, "p"); err == nil {
		t.Fatalf("expected error for empty id, got nil")
	}
	if _, err := targets.CreateWithPath("id", "", "h", 22, ProtocolSSH, "p"); err == nil {
		t.Fatalf("expected error for empty name, got nil")
	}
	if _, err := targets.CreateWithPath("id", "n", "", 22, ProtocolSSH, "p"); err == nil {
		t.Fatalf("expected error for empty host, got nil")
	}
}

func TestSQLiteTargetStore_Get_NotFoundAndPortBounds(t *testing.T) {
	groups, targets := newTestSQLiteStores(t)

	// no such id
	if _, err := targets.Get("missing"); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound for unknown id, got %v", err)
	}

	// happy path within bounds
	_, _ = groups.Create("g1", "G1")
	if _, err := targets.CreateWithPath("t1", "ok", "127.0.0.1", 22, ProtocolSSH, "g1"); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	got, err := targets.Get("t1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Port != 22 {
		t.Fatalf("expected port 22, got %d", got.Port)
	}
}

func TestSQLiteTargetStore_ListByIDs_SkipsInvalidAndDuplicates(t *testing.T) {
	groups, targets := newTestSQLiteStores(t)

	_, _ = groups.Create("g1", "G1")
	_, _ = targets.CreateWithPath("t1", "A", "10.0.0.1", 22, ProtocolSSH, "g1")
	_, _ = targets.CreateWithPath("t2", "B", "10.0.0.2", 23, ProtocolTelnet, "g1")

	list := targets.ListByIDs([]string{"", "t1", "t1", "missing", "t2"})
	if len(list) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(list))
	}
}

func TestSQLiteAccessGroupStore_Create_Duplicate(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)

	if _, err := groups.Create("g1", "G1"); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	if _, err := groups.Create("g1", "G1"); err != ErrGroupExists {
		t.Fatalf("expected ErrGroupExists, got %v", err)
	}
}

func TestSQLiteAccessGroupStore_AddUserAndTargetToUnknownGroup(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)

	if err := groups.AddUserToGroup("u1", "missing"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound for AddUserToGroup, got %v", err)
	}
	if err := groups.AddTargetToGroup("missing", "t1"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound for AddTargetToGroup, got %v", err)
	}
}

func TestSQLiteAccessGroupStore_EmptyRelationsReturnNil(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)

	if got := groups.GroupIDsForUser("nouser"); got != nil {
		t.Fatalf("expected nil group IDs for unknown user, got %v", got)
	}
	if got := groups.TargetIDsForGroup("nogroup"); got != nil {
		t.Fatalf("expected nil target IDs for unknown group, got %v", got)
	}
	if got := groups.TargetIDsForUser("nouser"); got != nil {
		t.Fatalf("expected nil target IDs for unknown user, got %v", got)
	}
}

func TestSQLiteTargetStore_GetAndList_InvalidPortRowSkipped(t *testing.T) {
	groups, targets := newTestSQLiteStores(t)

	_, err := groups.Create("g1", "G1")
	if err != nil {
		t.Fatalf("Create group: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert a row with an out-of-range port to exercise the bounds check.
	_, err = targets.db.ExecContext(ctx, `
		INSERT INTO targets (id, name, host, port, protocol, group_id, path)
		VALUES ('bad', 'Bad', '10.0.0.3', 70000, 'ssh', 'g1', 'g1')
	`)
	if err != nil {
		t.Fatalf("insert bad target: %v", err)
	}

	if _, err := targets.Get("bad"); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound for invalid port row, got %v", err)
	}

	if list := targets.ListByIDs([]string{"bad"}); list != nil {
		t.Fatalf("expected nil slice from ListByIDs for invalid port row, got %v", list)
	}
}
