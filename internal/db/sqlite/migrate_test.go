package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")
	db, err := Open(Config{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify tables exist
	tables := []string{"users", "access_groups", "targets", "user_groups", "group_targets", "sessions", "user_ssh_keys", "file_transfer_jobs"}
	for _, name := range tables {
		var n int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n)
		if err != nil {
			t.Fatalf("check table %s: %v", name, err)
		}
		if n != 1 {
			t.Errorf("table %s: expected 1, got %d", name, n)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")
	db, err := Open(Config{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate first: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate second: %v", err)
	}
}

// TestMigrate_AddsSSHHostKeyInsecureSkipVerifyColumn pins the
// migration that previously slipped through review: the
// ssh_host_key_insecure_skip_verify column is referenced by
// internal/access/sqlite_store.go (SetSSHHostKeyInsecureSkipVerify
// and the SELECT in Get / GetByID), so without the migration a brand-new
// SQLite database would fail at the first call.
func TestMigrate_AddsSSHHostKeyInsecureSkipVerifyColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hostkey.db")
	db, err := Open(Config{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := db.Query(`PRAGMA table_info(targets)`)
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()
	var found bool
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    *string
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "ssh_host_key_insecure_skip_verify" {
			found = true
		}
	}
	if !found {
		t.Fatal("ssh_host_key_insecure_skip_verify column missing after migrate")
	}
}

// DBs that already have schemaMarkCurrent must still receive later column patches.
func TestMigrate_FastPathAddsSSHKeyKeyType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ssh-key-type.db")
	db, err := Open(Config{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE migration_marks (name TEXT PRIMARY KEY)`)
	if err != nil {
		t.Fatalf("marks table: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO migration_marks(name) VALUES (?)`, schemaMarkCurrent)
	if err != nil {
		t.Fatalf("insert mark: %v", err)
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE ssh_keys (
		id TEXT PRIMARY KEY,
		label TEXT NOT NULL,
		ssh_private_key TEXT NOT NULL DEFAULT '',
		ssh_private_key_passphrase TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create ssh_keys: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	ok, err := hasColumn(ctx, db, "ssh_keys", "key_type")
	if err != nil {
		t.Fatalf("hasColumn: %v", err)
	}
	if !ok {
		t.Fatal("ssh_keys.key_type missing after migrate fast-path")
	}

	var n int
	err = db.QueryRowContext(ctx, `SELECT COUNT(1) FROM ssh_keys`).Scan(&n)
	if err != nil {
		t.Fatalf("list ssh_keys: %v", err)
	}
}

func TestMigrate_ExecError(t *testing.T) {
	// Use a closed DB so ExecContext fails
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = Migrate(db)
	if err == nil {
		t.Fatal("expected error when migrating closed db")
	}
}
