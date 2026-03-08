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
	return NewSQLiteAccessGroupStore(db, nil), NewSQLiteTargetStore(db, nil, nil)
}

func TestSQLiteTargetStore_CreateWithPath_Validation(t *testing.T) {
	ctx := context.Background()
	_, targets := newTestSQLiteStores(t)

	if _, err := targets.CreateWithPath(ctx, "", "n", "h", 22, ProtocolSSH, GroupID("p"), "p", "", ""); err == nil {
		t.Fatalf("expected error for empty id, got nil")
	}
	if _, err := targets.CreateWithPath(ctx, "id", "", "h", 22, ProtocolSSH, GroupID("p"), "p", "", ""); err == nil {
		t.Fatalf("expected error for empty name, got nil")
	}
	if _, err := targets.CreateWithPath(ctx, "id", "n", "", 22, ProtocolSSH, GroupID("p"), "p", "", ""); err == nil {
		t.Fatalf("expected error for empty host, got nil")
	}
}

func TestSQLiteTargetStore_Update(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "old", "10.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")

	updated, err := targets.Update(ctx, "t1", "new", "10.0.0.2", 2222, ProtocolTelnet, "g1/sub", "user", "")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "new" || updated.Host != "10.0.0.2" || updated.Port != 2222 || updated.Protocol != ProtocolTelnet || updated.Path != "g1/sub" || updated.SSHUsername != "user" {
		t.Fatalf("unexpected updated target: %+v", updated)
	}
	got, _ := targets.Get(ctx, "t1")
	if got.Name != "new" || got.Host != "10.0.0.2" {
		t.Fatalf("Get after update: %+v", got)
	}
	if _, err := targets.Update(ctx, "missing", "n", "h", 22, ProtocolSSH, "", "", ""); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestSQLiteTargetStore_Delete(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "n", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")

	if err := targets.Delete(ctx, "t1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := targets.Get(ctx, "t1"); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound after delete, got %v", err)
	}
	if err := targets.Delete(ctx, "missing"); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound for delete missing, got %v", err)
	}
}

func TestSQLiteTargetStore_Get_NotFoundAndPortBounds(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)

	// no such id
	if _, err := targets.Get(ctx, "missing"); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound for unknown id, got %v", err)
	}

	// happy path within bounds
	_, _ = groups.Create(ctx, "g1", "G1")
	if _, err := targets.CreateWithPath(ctx, "t1", "ok", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", ""); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	got, err := targets.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Port != 22 {
		t.Fatalf("expected port 22, got %d", got.Port)
	}
}

func TestSQLiteTargetStore_SSHPassword_EncryptionRequiredWithoutKey(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, err := targets.CreateWithPath(ctx, "t1", "n", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "user", "secret")
	if err != ErrEncryptionKeyRequired {
		t.Fatalf("expected ErrEncryptionKeyRequired when storing password without encKey, got %v", err)
	}
}

func TestSQLiteTargetStore_SSHPassword_EncryptDecryptWithKey(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 10)
	}
	storeWithKey := NewSQLiteTargetStore(targets.db, nil, key)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, err := storeWithKey.CreateWithPath(ctx, "t1", "n", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "root", "mypass")
	if err != nil {
		t.Fatalf("CreateWithPath with encKey: %v", err)
	}
	got, err := storeWithKey.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SSHUsername != "root" || got.SSHPassword != "mypass" {
		t.Fatalf("expected root/mypass, got %q/%q", got.SSHUsername, got.SSHPassword)
	}
	// Without key, same row returns ciphertext (legacy decrypt returns as-is for v1: prefix when key missing we don't decrypt - we do decrypt only when key is set)
	gotNoKey, _ := targets.Get(ctx, "t1")
	if gotNoKey != nil && gotNoKey.SSHPassword == "mypass" {
		t.Fatal("store without key should not decrypt password")
	}
}

func TestSQLiteTargetStore_ListByIDs_SkipsInvalidAndDuplicates(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)

	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "A", "10.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_, _ = targets.CreateWithPath(ctx, "t2", "B", "10.0.0.2", 23, ProtocolTelnet, GroupID("g1"), "g1", "", "")

	list, err := targets.ListByIDs(ctx, []TargetID{"", "t1", "t1", "missing", "t2"}, nil)
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(list))
	}
}

func TestSQLiteAccessGroupStore_Create_Duplicate(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)

	if _, err := groups.Create(ctx, "g1", "G1"); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	if _, err := groups.Create(ctx, "g1", "G1"); err != ErrGroupExists {
		t.Fatalf("expected ErrGroupExists, got %v", err)
	}
}

func TestSQLiteAccessGroupStore_AddUserAndTargetToUnknownGroup(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)

	if err := groups.AddUserToGroup(ctx, "u1", "missing"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound for AddUserToGroup, got %v", err)
	}
	if err := groups.AddTargetToGroup(ctx, "missing", "t1"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound for AddTargetToGroup, got %v", err)
	}
}

func TestSQLiteAccessGroupStore_EmptyRelationsReturnNil(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)

	got, err := groups.GroupIDsForUser(ctx, "nouser", nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected empty group IDs for unknown user, got %v err=%v", got, err)
	}
	got2, err := groups.TargetIDsForGroup(ctx, "nogroup", nil)
	if err != nil || len(got2) != 0 {
		t.Fatalf("expected empty target IDs for unknown group, got %v err=%v", got2, err)
	}
	got3, err := groups.TargetIDsForUser(ctx, "nouser", nil)
	if err != nil || len(got3) != 0 {
		t.Fatalf("expected empty target IDs for unknown user, got %v err=%v", got3, err)
	}
}

func TestSQLiteAccessGroupStore_KeysetPagination(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)

	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "A", "10.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_, _ = targets.CreateWithPath(ctx, "t2", "B", "10.0.0.2", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_, _ = targets.CreateWithPath(ctx, "t3", "C", "10.0.0.3", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_, _ = targets.CreateWithPath(ctx, "t4", "D", "10.0.0.4", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_ = groups.AddTargetToGroup(ctx, "g1", "t1")
	_ = groups.AddTargetToGroup(ctx, "g1", "t2")
	_ = groups.AddTargetToGroup(ctx, "g1", "t3")
	_ = groups.AddTargetToGroup(ctx, "g1", "t4")

	// First page: limit 2, no cursor
	page1, err := groups.TargetIDsForGroup(ctx, "g1", &ListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("TargetIDsForGroup page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("expected 2 targets on page1, got %d", len(page1))
	}

	// Second page: after last ID from page1
	after := string(page1[len(page1)-1])
	page2, err := groups.TargetIDsForGroup(ctx, "g1", &ListOpts{Limit: 2, AfterID: after})
	if err != nil {
		t.Fatalf("TargetIDsForGroup page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("expected 2 targets on page2, got %d", len(page2))
	}
	// No overlap
	seen := make(map[TargetID]bool)
	for _, id := range page1 {
		seen[id] = true
	}
	for _, id := range page2 {
		if seen[id] {
			t.Fatalf("page2 contained duplicate from page1: %s", id)
		}
	}
}

func TestSQLiteAccessGroupStore_TagBasedAccess(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	// Create user in same DB for user_tags (auth table)
	db := groups.db
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO users (id, username, password_hash) VALUES ('u1', 'u1', 'hash')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "T1", "h1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_ = groups.AddTargetToGroup(ctx, "g1", "t1")
	// u1 not in any group -> no access yet
	ids, err := groups.TargetIDsForUser(ctx, "u1", nil)
	if err != nil || len(ids) != 0 {
		t.Fatalf("expected no access without group, got %v err=%v", ids, err)
	}
	// Add tag "prod" to user and to target -> u1 should see t1
	_, _ = db.ExecContext(ctx, `INSERT INTO user_tags (user_id, tag) VALUES ('u1', 'prod')`)
	_, _ = db.ExecContext(ctx, `INSERT INTO target_tags (target_id, tag) VALUES ('t1', 'prod')`)
	ids, err = groups.TargetIDsForUser(ctx, "u1", nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	if len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("expected [t1] via tag, got %v", ids)
	}
	// Add tag to group: user with "ops" tag should see targets in group with "ops" tag
	_, _ = db.ExecContext(ctx, `DELETE FROM user_tags WHERE user_id = 'u1'`)
	_, _ = db.ExecContext(ctx, `DELETE FROM target_tags WHERE target_id = 't1'`)
	_, _ = db.ExecContext(ctx, `INSERT INTO user_tags (user_id, tag) VALUES ('u1', 'ops')`)
	_, _ = db.ExecContext(ctx, `INSERT INTO group_tags (group_id, tag) VALUES ('g1', 'ops')`)
	ids, err = groups.TargetIDsForUser(ctx, "u1", nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	if len(ids) != 1 || ids[0] != "t1" {
		t.Fatalf("expected [t1] via group tag, got %v", ids)
	}
}

func TestSQLiteAccessGroupStore_GroupIDsForUser_ViaTag(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	db := groups.db
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO users (id, username, password_hash) VALUES ('u2', 'u2', 'hash')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, _ = groups.Create(ctx, "g2", "G2")
	_, _ = targets.CreateWithPath(ctx, "t2", "T2", "h2", 22, ProtocolSSH, GroupID("g2"), "g2", "", "")
	_ = groups.AddTargetToGroup(ctx, "g2", "t2")
	// u2 is not a member of g2; give u2 and t2 the same tag so u2 has tag-based access
	_, _ = db.ExecContext(ctx, `INSERT INTO user_tags (user_id, tag) VALUES ('u2', 'home')`)
	_, _ = db.ExecContext(ctx, `INSERT INTO target_tags (target_id, tag) VALUES ('t2', 'home')`)
	// GroupIDsForUser should return g2 so the UI can show the group and its (filtered) targets
	gids, err := groups.GroupIDsForUser(ctx, "u2", nil)
	if err != nil {
		t.Fatalf("GroupIDsForUser: %v", err)
	}
	if len(gids) != 1 || gids[0] != "g2" {
		t.Fatalf("expected [g2] via tag (user sees group containing tag-matching target), got %v", gids)
	}
}

func TestSQLiteAccessGroupStore_Get_FoundAndNotFound(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)

	if _, err := groups.Get(ctx, "missing"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound for missing group, got %v", err)
	}
	_, err := groups.Create(ctx, "g1", "G1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := groups.Get(ctx, "g1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "g1" || got.Name != "G1" {
		t.Fatalf("expected g1/G1, got %s/%s", got.ID, got.Name)
	}
}

func TestSQLiteAccessGroupStore_Create_Validation(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)

	for _, tc := range []struct {
		id, name string
	}{
		{"", "x"},
		{"x", ""},
		{string(make([]byte, 513)), "x"},
		{"x", string(make([]byte, 513))},
		{"id", "name\x00"},
		{"bad id!", "x"},
	} {
		if _, err := groups.Create(ctx, GroupID(tc.id), tc.name); err == nil {
			t.Fatalf("expected error for id=%q name=%q", tc.id, tc.name)
		}
	}
}

func TestSQLiteAccessGroupStore_ListLimitAndConfig(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "access.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := &StoreConfig{QueryTimeout: 2 * time.Second, DefaultListLimit: 2}
	groups := NewSQLiteAccessGroupStore(db, cfg)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = groups.Create(ctx, "g2", "G2")
	_, _ = groups.Create(ctx, "g3", "G3")

	all, err := groups.GroupIDsForUser(ctx, "nouser", nil)
	if err != nil || len(all) != 0 {
		t.Fatalf("expected empty for nouser: %v %v", all, err)
	}
	opts := &ListOpts{Limit: 2, Offset: 1}
	page, err := groups.GroupIDsForUser(ctx, "nouser", opts)
	if err != nil || len(page) != 0 {
		t.Fatalf("expected empty page: %v %v", page, err)
	}
}

func TestSQLiteTargetStore_Create_CallsCreateWithPath(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	// Create() passes group_id ""; schema has group_id NOT NULL + FK, so this fails at INSERT (covers Create path).
	_, err := targets.Create(ctx, "t1", "N", "127.0.0.1", 22, ProtocolSSH)
	if err == nil {
		t.Fatal("expected error from Create (group_id empty vs NOT NULL/FK)")
	}
}

func TestSQLiteTargetStore_CreateWithPath_InvalidProtocolAndHost(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")

	if _, err := targets.CreateWithPath(ctx, "t1", "n", "127.0.0.1", 22, "invalid", GroupID("g1"), "g1", "", ""); err == nil {
		t.Fatal("expected error for invalid protocol")
	}
	if _, err := targets.CreateWithPath(ctx, "t1", "n", "", 22, ProtocolSSH, GroupID("g1"), "g1", "", ""); err == nil {
		t.Fatal("expected error for empty host")
	}
	// hostname must not start/end with hyphen or dot per pattern
	if _, err := targets.CreateWithPath(ctx, "t1", "n", "-bad", 22, ProtocolSSH, GroupID("g1"), "g1", "", ""); err == nil {
		t.Fatal("expected error for invalid hostname (-bad)")
	}
	if _, err := targets.CreateWithPath(ctx, "t1", "n", string(make([]byte, 254)), 22, ProtocolSSH, GroupID("g1"), "g1", "", ""); err == nil {
		t.Fatal("expected error for host too long")
	}
}

func TestSQLiteTargetStore_CreateWithPath_InvalidTargetID(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")

	if _, err := targets.CreateWithPath(ctx, "", "n", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", ""); err == nil {
		t.Fatal("expected error for empty target id")
	}
	if _, err := targets.CreateWithPath(ctx, TargetID(string(make([]byte, 513))), "n", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", ""); err == nil {
		t.Fatal("expected error for target id too long")
	}
	if _, err := targets.CreateWithPath(ctx, "bad id!", "n", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", ""); err == nil {
		t.Fatal("expected error for invalid target id characters")
	}
	// invalid group_id when non-empty
	if _, err := targets.CreateWithPath(ctx, "t1", "n", "127.0.0.1", 22, ProtocolSSH, GroupID("bad!"), "p", "", ""); err == nil {
		t.Fatal("expected error for invalid group id")
	}
}

func TestSQLiteTargetStore_ListByIDs_WithLimitOffset(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "A", "10.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_, _ = targets.CreateWithPath(ctx, "t2", "B", "10.0.0.2", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_, _ = targets.CreateWithPath(ctx, "t3", "C", "10.0.0.3", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")

	list, err := targets.ListByIDs(ctx, []TargetID{"t1", "t2", "t3"}, &ListOpts{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 targets (limit=2 offset=1), got %d", len(list))
	}
	if list[0].ID != "t2" || list[1].ID != "t3" {
		t.Fatalf("expected t2,t3 order, got %s %s", list[0].ID, list[1].ID)
	}

	empty, err := targets.ListByIDs(ctx, []TargetID{"t1", "t2"}, &ListOpts{Limit: 10, Offset: 10})
	if err != nil || len(empty) != 0 {
		t.Fatalf("expected empty when offset >= len(ids): %v %v", empty, err)
	}
}

func TestSQLiteTargetStore_NewWithConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = dbsqlite.Migrate(db)
	cfg := &StoreConfig{QueryTimeout: 3 * time.Second, DefaultListLimit: 50}
	store := NewSQLiteTargetStore(db, cfg, nil)
	if store == nil {
		t.Fatal("NewSQLiteTargetStore with cfg returned nil")
	}
}

func TestSQLiteAccessGroupStore_GroupIDsForUser_Keyset(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "access.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	groups := NewSQLiteAccessGroupStore(db, nil)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = groups.Create(ctx, "g2", "G2")
	_, _ = groups.Create(ctx, "g3", "G3")
	// Insert a fake user and user_groups so GroupIDsForUser returns data.
	_, _ = db.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('u1', 'user1', 'x', datetime('now'), datetime('now'))`)
	_, _ = db.ExecContext(ctx, `INSERT INTO user_groups (user_id, group_id) VALUES ('u1', 'g1'), ('u1', 'g2'), ('u1', 'g3')`)

	page1, err := groups.GroupIDsForUser(ctx, "u1", &ListOpts{Limit: 2})
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1: err=%v len=%d", err, len(page1))
	}
	page2, err := groups.GroupIDsForUser(ctx, "u1", &ListOpts{Limit: 2, AfterID: string(page1[1])})
	if err != nil || len(page2) != 1 {
		t.Fatalf("page2: err=%v len=%d", err, len(page2))
	}
}

func TestSQLiteTargetStore_GetAndList_InvalidPortRowSkipped(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)

	_, err := groups.Create(ctx, "g1", "G1")
	if err != nil {
		t.Fatalf("Create group: %v", err)
	}

	ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert a row with an out-of-range port to exercise the bounds check.
	_, err = targets.db.ExecContext(ctx2, `
		INSERT INTO targets (id, name, host, port, protocol, group_id, path)
		VALUES ('bad', 'Bad', '10.0.0.3', 70000, 'ssh', 'g1', 'g1')
	`)
	if err != nil {
		t.Fatalf("insert bad target: %v", err)
	}

	if _, err := targets.Get(ctx, "bad"); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound for invalid port row, got %v", err)
	}

	list, err := targets.ListByIDs(ctx, []TargetID{"bad"}, nil)
	if err != nil || len(list) != 0 {
		t.Fatalf("expected empty slice from ListByIDs for invalid port row, got %v err=%v", list, err)
	}
}

func TestSQLiteAccessGroupStore_TagsForGroup_SetGroupTags(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")

	tags, err := groups.TagsForGroup(ctx, "g1")
	if err != nil || len(tags) != 0 {
		t.Fatalf("TagsForGroup empty: err=%v tags=%v", err, tags)
	}
	tags, _ = groups.TagsForGroup(ctx, "missing")
	if len(tags) != 0 {
		t.Fatalf("expected empty for missing group, got %v", tags)
	}

	if err := groups.SetGroupTags(ctx, "g1", []string{"prod", "env-a"}); err != nil {
		t.Fatalf("SetGroupTags: %v", err)
	}
	tags, err = groups.TagsForGroup(ctx, "g1")
	if err != nil || len(tags) != 2 || tags[0] != "env-a" || tags[1] != "prod" {
		t.Fatalf("TagsForGroup after set: err=%v tags=%v", err, tags)
	}

	if err := groups.SetGroupTags(ctx, "g1", []string{"only"}); err != nil {
		t.Fatalf("SetGroupTags replace: %v", err)
	}
	tags, _ = groups.TagsForGroup(ctx, "g1")
	if len(tags) != 1 || tags[0] != "only" {
		t.Fatalf("expected [only], got %v", tags)
	}

	if err := groups.SetGroupTags(ctx, "g1", nil); err != nil {
		t.Fatalf("SetGroupTags clear: %v", err)
	}
	tags, _ = groups.TagsForGroup(ctx, "g1")
	if len(tags) != 0 {
		t.Fatalf("expected empty after clear, got %v", tags)
	}

	if err := groups.SetGroupTags(ctx, "missing", []string{"x"}); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound for missing group, got %v", err)
	}
	if err := groups.SetGroupTags(ctx, "g1", []string{""}); err == nil {
		t.Fatal("expected error for empty tag")
	}
	if err := groups.SetGroupTags(ctx, "g1", []string{"valid", "bad tag!"}); err == nil {
		t.Fatal("expected error for invalid tag character")
	}
}

func TestSQLiteTargetStore_TagsForTarget_SetTargetTags(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "H1", "10.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")

	tags, err := targets.TagsForTarget(ctx, "t1")
	if err != nil || len(tags) != 0 {
		t.Fatalf("TagsForTarget empty: err=%v tags=%v", err, tags)
	}

	if err := targets.SetTargetTags(ctx, "t1", []string{"prod", "web"}); err != nil {
		t.Fatalf("SetTargetTags: %v", err)
	}
	tags, err = targets.TagsForTarget(ctx, "t1")
	if err != nil || len(tags) != 2 || tags[0] != "prod" || tags[1] != "web" {
		t.Fatalf("TagsForTarget after set: err=%v tags=%v", err, tags)
	}

	if err := targets.SetTargetTags(ctx, "t1", nil); err != nil {
		t.Fatalf("SetTargetTags clear: %v", err)
	}
	tags, _ = targets.TagsForTarget(ctx, "t1")
	if len(tags) != 0 {
		t.Fatalf("expected empty after clear, got %v", tags)
	}

	if err := targets.SetTargetTags(ctx, "missing", []string{"x"}); err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
	if err := targets.SetTargetTags(ctx, "t1", []string{"a", "bad space"}); err == nil {
		t.Fatal("expected error for invalid tag")
	}
}

// TestSQLiteAccessGroupStore_Get_CanceledContext covers Get returning non-ErrNoRows error.
func TestSQLiteAccessGroupStore_Get_CanceledContext(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := groups.Get(ctx, "g1")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteAccessGroupStore_AddUserToGroup_CanceledContext covers tx/commit error path.
func TestSQLiteAccessGroupStore_AddUserToGroup_CanceledContext(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := groups.AddUserToGroup(ctx, "u1", "g1")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteAccessGroupStore_RemoveUserFromGroup_NotFoundAndCanceled covers ErrGroupNotFound and Exec error.
func TestSQLiteAccessGroupStore_RemoveUserFromGroup_NotFoundAndCanceled(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)
	if err := groups.RemoveUserFromGroup(context.Background(), "u1", "missing"); err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
	_, _ = groups.Create(context.Background(), "g1", "G1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := groups.RemoveUserFromGroup(ctx, "u1", "g1")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteAccessGroupStore_UserIDsForGroup_WithAfterID covers keyset pagination branch.
func TestSQLiteAccessGroupStore_UserIDsForGroup_WithAfterID(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	for _, u := range []string{"u1", "u2", "u3"} {
		_, _ = groups.db.ExecContext(ctx, `INSERT OR IGNORE INTO users (id, username, password_hash) VALUES (?, ?, 'hash')`, u, u)
		_ = groups.AddUserToGroup(ctx, UserID(u), "g1")
	}
	ids, err := groups.UserIDsForGroup(ctx, "g1", &ListOpts{AfterID: "u1", Limit: 2})
	if err != nil {
		t.Fatalf("UserIDsForGroup: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids after u1, got %d", len(ids))
	}
}

// TestSQLiteAccessGroupStore_SetGroupTags_CanceledContext covers DELETE/INSERT error path.
func TestSQLiteAccessGroupStore_SetGroupTags_CanceledContext(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := groups.SetGroupTags(ctx, "g1", []string{"a"})
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteAccessGroupStore_AddTargetToGroup_CanceledContext covers BeginTx/Commit error path.
func TestSQLiteAccessGroupStore_AddTargetToGroup_CanceledContext(t *testing.T) {
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	_, _ = targets.CreateWithPath(context.Background(), "t1", "T", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := groups.AddTargetToGroup(ctx, "g1", "t1")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteAccessGroupStore_GroupIDsForUser_OffsetExceedsLength covers offset >= len(all) return nil.
func TestSQLiteAccessGroupStore_GroupIDsForUser_OffsetExceedsLength(t *testing.T) {
	ctx := context.Background()
	groups, _ := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = groups.db.ExecContext(ctx, `INSERT INTO users (id, username, password_hash) VALUES ('u1','u1','x')`)
	_ = groups.AddUserToGroup(ctx, "u1", "g1")
	page, err := groups.GroupIDsForUser(ctx, "u1", &ListOpts{Limit: 10, Offset: 1})
	if err != nil {
		t.Fatalf("GroupIDsForUser: %v", err)
	}
	if len(page) != 0 {
		t.Fatalf("expected empty when offset >= len(all), got %d", len(page))
	}
}

// TestSQLiteAccessGroupStore_TargetIDsForUser_AfterIDAndOffset covers keyset and offset.
func TestSQLiteAccessGroupStore_TargetIDsForUser_AfterIDAndOffset(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "A", "10.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_, _ = targets.CreateWithPath(ctx, "t2", "B", "10.0.0.2", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	_ = groups.AddTargetToGroup(ctx, "g1", "t1")
	_ = groups.AddTargetToGroup(ctx, "g1", "t2")
	_, _ = groups.db.ExecContext(ctx, `INSERT INTO users (id, username, password_hash) VALUES ('u1','u1','x')`)
	_ = groups.AddUserToGroup(ctx, "u1", "g1")
	page, err := groups.TargetIDsForUser(ctx, "u1", &ListOpts{AfterID: "t1", Limit: 5})
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	if len(page) != 1 || page[0] != "t2" {
		t.Fatalf("expected [t2] after t1, got %v", page)
	}
	page2, err := groups.TargetIDsForUser(ctx, "u1", &ListOpts{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("TargetIDsForUser offset: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("expected 1 target with offset 1, got %d", len(page2))
	}
	page3, err := groups.TargetIDsForUser(ctx, "u1", &ListOpts{Limit: 10, Offset: 10})
	if err != nil || len(page3) != 0 {
		t.Fatalf("expected empty when offset >= len, got %v err=%v", page3, err)
	}
}

// TestSQLiteTargetStore_Update_Validation covers validation and ErrEncryptionKeyRequired.
func TestSQLiteTargetStore_Update_Validation(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = targets.CreateWithPath(ctx, "t1", "N", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")

	if _, err := targets.Update(ctx, "", "n", "h", 22, ProtocolSSH, "", "", ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if _, err := targets.Update(ctx, "t1", "", "h", 22, ProtocolSSH, "", "", ""); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := targets.Update(ctx, "t1", "n", "", 22, ProtocolSSH, "", "", ""); err == nil {
		t.Fatal("expected error for empty host")
	}
	if _, err := targets.Update(ctx, "t1", "n", "h", 22, "invalid", "", "", ""); err == nil {
		t.Fatal("expected error for invalid protocol")
	}
	if _, err := targets.Update(ctx, "t1", "n", "h", 22, ProtocolSSH, "", "", "secret"); err != ErrEncryptionKeyRequired {
		t.Fatalf("expected ErrEncryptionKeyRequired when updating with password and no key, got %v", err)
	}
}

// TestSQLiteTargetStore_Update_CanceledContext covers ExecContext error path.
func TestSQLiteTargetStore_Update_CanceledContext(t *testing.T) {
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	_, _ = targets.CreateWithPath(context.Background(), "t1", "N", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := targets.Update(ctx, "t1", "n", "127.0.0.1", 22, ProtocolSSH, "", "", "")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteTargetStore_Delete_EmptyID covers validateTargetID error.
func TestSQLiteTargetStore_Delete_EmptyID(t *testing.T) {
	_, targets := newTestSQLiteStores(t)
	if err := targets.Delete(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty id")
	}
}

// TestSQLiteTargetStore_Get_DecryptFailure covers Get when stored password exists but Decrypt fails.
func TestSQLiteTargetStore_Get_DecryptFailure(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	storeWithKey := NewSQLiteTargetStore(targets.db, nil, key)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, err := storeWithKey.CreateWithPath(ctx, "t1", "N", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "u", "pass")
	if err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	_, err = targets.db.ExecContext(ctx, `UPDATE targets SET ssh_password = 'v1:!!!' WHERE id = 't1'`)
	if err != nil {
		t.Fatalf("corrupt password: %v", err)
	}
	got, err := storeWithKey.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SSHPassword != "v1:!!!" {
		t.Fatalf("expected stored ciphertext when decrypt fails, got %q", got.SSHPassword)
	}
}

// TestSQLiteTargetStore_ListByIDs_DecryptFailure covers ListByIDs when one row has decrypt failure.
func TestSQLiteTargetStore_ListByIDs_DecryptFailure(t *testing.T) {
	ctx := context.Background()
	groups, targets := newTestSQLiteStores(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 2)
	}
	storeWithKey := NewSQLiteTargetStore(targets.db, nil, key)
	_, _ = groups.Create(ctx, "g1", "G1")
	_, _ = storeWithKey.CreateWithPath(ctx, "t1", "A", "10.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "u", "p1")
	_, _ = storeWithKey.CreateWithPath(ctx, "t2", "B", "10.0.0.2", 22, ProtocolSSH, GroupID("g1"), "g1", "u", "p2")
	_, _ = targets.db.ExecContext(ctx, `UPDATE targets SET ssh_password = 'v1:invalid' WHERE id = 't2'`)
	list, err := storeWithKey.ListByIDs(ctx, []TargetID{"t1", "t2"}, nil)
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(list))
	}
	if list[1].SSHPassword != "v1:invalid" {
		t.Fatalf("expected ciphertext when decrypt fails for t2, got %q", list[1].SSHPassword)
	}
}

// TestSQLiteTargetStore_SetTargetTags_CanceledContext covers error path.
func TestSQLiteTargetStore_SetTargetTags_CanceledContext(t *testing.T) {
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	_, _ = targets.CreateWithPath(context.Background(), "t1", "N", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := targets.SetTargetTags(ctx, "t1", []string{"a"})
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteTargetStore_TagsForGroup_QueryError covers TagsForGroup when query fails (e.g. canceled context).
func TestSQLiteAccessGroupStore_TagsForGroup_QueryError(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := groups.TagsForGroup(ctx, "g1")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteTargetStore_TagsForTarget_QueryError covers TagsForTarget error path.
func TestSQLiteTargetStore_TagsForTarget_QueryError(t *testing.T) {
	groups, targets := newTestSQLiteStores(t)
	_, _ = groups.Create(context.Background(), "g1", "G1")
	_, _ = targets.CreateWithPath(context.Background(), "t1", "N", "127.0.0.1", 22, ProtocolSSH, GroupID("g1"), "g1", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := targets.TagsForTarget(ctx, "t1")
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteAccessGroupStore_SetGroupTags_QueryRowError covers SetGroupTags when SELECT fails (non-ErrNoRows).
func TestSQLiteAccessGroupStore_SetGroupTags_QueryRowError(t *testing.T) {
	groups, _ := newTestSQLiteStores(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := groups.SetGroupTags(ctx, "g1", []string{"a"})
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

// TestSQLiteTargetStore_SetTargetTags_QueryRowError covers SetTargetTags when SELECT fails.
func TestSQLiteTargetStore_SetTargetTags_QueryRowError(t *testing.T) {
	_, targets := newTestSQLiteStores(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := targets.SetTargetTags(ctx, "t1", []string{"a"})
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}
