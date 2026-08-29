package tftp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func TestCurrent_NilWhenNotRunning(t *testing.T) {
	// No test in this package starts the process-wide controller (that
	// would bind a real UDP listener), so it should still report "not
	// running" here.
	if srv := Current(); srv != nil {
		t.Fatalf("expected Current() to be nil when the controller was never started, got %v", srv)
	}
}

func TestIsEmbeddedCompanionTarget(t *testing.T) {
	ssh := &access.Target{ID: "s1", Host: "10.0.0.1", Protocol: access.ProtocolSSH, TFTPEnabled: true}
	standalone := &access.Target{ID: "t1", Host: "192.168.1.50", Protocol: access.ProtocolTFTP}
	companion := &access.Target{ID: "t2", Host: "10.0.0.1", Protocol: access.ProtocolTFTP}

	if !isEmbeddedCompanionTarget(companion, []*access.Target{ssh}) {
		t.Fatal("expected companion")
	}
	if isEmbeddedCompanionTarget(standalone, []*access.Target{ssh}) {
		t.Fatal("expected standalone external TFTP not companion")
	}
}

func TestDisableAtStartup_KeepsStandaloneTFTP(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tftp.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatal(err)
	}
	groups := access.NewSQLiteAccessGroupStore(db, nil)
	store := access.NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()
	if _, err := groups.Create(ctx, "g1", "G1"); err != nil {
		t.Fatal(err)
	}

	mustCreate := func(id, name, host string, port uint16, proto access.Protocol, tftpEnabled bool) {
		t.Helper()
		if _, err := store.CreateWithPath(ctx, access.TargetID(id), name, host, port, proto, "g1", "g1", "admin", "", "", "", true, false, tftpEnabled); err != nil {
			t.Fatalf("CreateWithPath %s: %v", id, err)
		}
	}
	mustCreate("ssh1", "Switch", "10.0.0.1", 22, access.ProtocolSSH, true)
	mustCreate("tftp-comp", "Switch (TFTP)", "10.0.0.1", 69, access.ProtocolTFTP, false)
	mustCreate("tftp-ext", "Remote TFTP", "192.168.99.1", 69, access.ProtocolTFTP, false)

	DisableAtStartup(ctx, store)

	if _, err := store.Get(ctx, "tftp-ext"); err != nil {
		t.Fatalf("standalone TFTP should remain: %v", err)
	}
	if _, err := store.Get(ctx, "tftp-comp"); err == nil {
		t.Fatal("companion TFTP should be removed at startup")
	}
}
