package sqlite

import (
	"context"
	"database/sql"
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

// dsn returns the SQLite DSN with foreign_keys enabled for all connections.
func dsn(path string) string {
	if path == "" || path == ":memory:" {
		return "file::memory:?_pragma=foreign_keys(on)"
	}
	return "file:" + path + "?_pragma=foreign_keys(on)"
}

// Open creates a *sql.DB for the given configuration and applies common pool settings.
func Open(cfg Config) (*sql.DB, error) {
	if cfg.Path == "" {
		cfg.Path = ":memory:"
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
