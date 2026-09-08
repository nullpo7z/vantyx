package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// sqlDriver is the driver name for sql.Open; may be overridden in tests to trigger error paths.
var sqlDriver = "sqlite"

// Config holds basic SQLite connection settings.
type Config struct {
	// Path is the filesystem path to the SQLite database file.
	Path string

	// MaxOpenConns is the maximum number of open connections to the database.
	// If 0, a sensible default is used.
	MaxOpenConns int

	// MaxIdleConns is the maximum number of idle connections in the pool.
	// If 0, a sensible default is used.
	MaxIdleConns int

	// ConnMaxLifetime defines maximum amount of time a connection may be reused.
	// If 0, a sensible default is used.
	ConnMaxLifetime time.Duration
}

// dsn returns the SQLite DSN with WAL, foreign_keys and busy_timeout for all connections.
// journal_mode=WAL improves concurrency (readers do not block writers). busy_timeout makes
// SQLite wait up to 5s on lock instead of returning SQLITE_BUSY immediately.
func dsn(path string) string {
	// _txlock=immediate makes every explicit transaction take the WAL
	// write lock at BEGIN. Without it a transaction that reads first and
	// writes later (SELECT ... then INSERT, the common store pattern)
	// fails with SQLITE_BUSY_SNAPSHOT (517, "database is locked") the
	// moment another connection commits in between -- busy_timeout does
	// not apply to that upgrade. With it the writer simply waits its turn.
	const pragmas = "_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)"
	if path == "" || path == ":memory:" {
		return "file::memory:?" + pragmas
	}
	return "file:" + path + "?" + pragmas
}

// DefaultPath is the default SQLite file path when VANTYX_SQLITE_PATH is not set.
// Use this so data persists across restarts. Callers (e.g. NewApp) set this when env is empty.
const DefaultPath = "data/vantyx.db"

// Open creates a *sql.DB for the given configuration and applies common pool settings.
// If Path is empty, :memory: is used. For file paths, the parent directory is created if needed.
func Open(cfg Config) (*sql.DB, error) {
	if cfg.Path == "" {
		cfg.Path = ":memory:"
	}
	if cfg.Path != "" && cfg.Path != ":memory:" {
		dir := filepath.Dir(cfg.Path)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open(sqlDriver, dsn(cfg.Path))
	if err != nil {
		return nil, err
	}

	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 10
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle < 0 {
		maxIdle = 0
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = 5 * time.Minute
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
