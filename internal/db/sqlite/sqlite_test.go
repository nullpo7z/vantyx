package sqlite

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpen_EmptyPath(t *testing.T) {
	db, err := Open(Config{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpen_MemoryPath(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpen_FilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(Config{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpen_CustomPool(t *testing.T) {
	db, err := Open(Config{
		Path:            ":memory:",
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpen_NegativeMaxIdleConns(t *testing.T) {
	db, err := Open(Config{Path: ":memory:", MaxIdleConns: -1})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpen_ZeroConnMaxLifetime(t *testing.T) {
	db, err := Open(Config{Path: ":memory:", ConnMaxLifetime: 0})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpen_CreatesParentDir(t *testing.T) {
	// Parent directory is created if missing, then DB is opened.
	path := filepath.Join(t.TempDir(), "sub", "db.db")
	db, err := Open(Config{Path: path})
	if err != nil {
		t.Fatalf("Open with missing parent dir: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpen_PingFails_DirAsPath(t *testing.T) {
	// Using a directory as the DB path causes SQLite to fail on first use (Ping).
	path := t.TempDir()
	_, err := Open(Config{Path: path})
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
}

func TestOpen_MkdirAllFails(t *testing.T) {
	// Parent "directory" is an existing file -> MkdirAll fails.
	dir := t.TempDir()
	fileAsParent := filepath.Join(dir, "file")
	if err := os.WriteFile(fileAsParent, []byte{}, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	path := filepath.Join(fileAsParent, "db.db")
	_, err := Open(Config{Path: path})
	if err == nil {
		t.Fatal("expected error when parent is a file")
	}
}

func TestOpen_DriverError(t *testing.T) {
	old := sqlDriver
	defer func() { sqlDriver = old }()
	sqlDriver = ""
	_, err := Open(Config{Path: ":memory:"})
	if err == nil {
		t.Fatal("expected error when driver is invalid")
	}
}

func TestDSN(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"", "file::memory:?_pragma=foreign_keys(on)"},
		{":memory:", "file::memory:?_pragma=foreign_keys(on)"},
		{"/tmp/db.db", "file:/tmp/db.db?_pragma=foreign_keys(on)"},
	}
	for _, tt := range tests {
		got := dsn(tt.path)
		if got != tt.want {
			t.Errorf("dsn(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
