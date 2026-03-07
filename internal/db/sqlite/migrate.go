package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Migrate applies the minimal schema required for Vantyx.
// It is safe to call multiple times; CREATE TABLE statements use IF NOT EXISTS.
func Migrate(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS access_groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS targets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			protocol TEXT NOT NULL,
			group_id TEXT NOT NULL,
			path TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS user_groups (
			user_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			PRIMARY KEY (user_id, group_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS group_targets (
			group_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			PRIMARY KEY (group_id, target_id),
			FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES targets(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		// Phase 2: session recording metadata (asciinema file path, channel type).
		`CREATE TABLE IF NOT EXISTS recordings (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			channel_type TEXT NOT NULL,
			file_path TEXT NOT NULL,
			started_at TIMESTAMP NOT NULL,
			ended_at TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Optional columns for targets (SSH credentials). Ignore if already present.
	for _, alter := range []string{
		`ALTER TABLE targets ADD COLUMN ssh_username TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE targets ADD COLUMN ssh_password TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.ExecContext(ctx, alter); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}
