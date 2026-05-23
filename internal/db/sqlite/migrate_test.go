package sqlite

import (
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
	tables := []string{"users", "access_groups", "targets", "user_groups", "group_targets", "sessions", "user_ssh_keys"}
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
