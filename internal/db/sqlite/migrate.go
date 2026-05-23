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
		`CREATE TABLE IF NOT EXISTS group_tags (
			group_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (group_id, tag),
			FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS target_tags (
			target_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (target_id, tag),
			FOREIGN KEY (target_id) REFERENCES targets(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS user_tags (
			user_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (user_id, tag),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
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
		// Audit logs (best-effort; used by audit log UI and exports).
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time TIMESTAMP NOT NULL,
			event TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			status INTEGER NOT NULL DEFAULT 0,
			remote TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			fields_json TEXT NOT NULL DEFAULT ''
		);`,
		// App settings (admin-configurable, persisted).
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		// Command logs: user input lines (best-effort, from terminal stdin).
		`CREATE TABLE IF NOT EXISTS command_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			time TIMESTAMP NOT NULL,
			line_text TEXT NOT NULL
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
		`ALTER TABLE targets ADD COLUMN ssh_private_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE targets ADD COLUMN ssh_private_key_passphrase TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.ExecContext(ctx, alter); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	// User role: admin | user. Default user; existing id='admin' -> admin.
	if _, err := db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET role = 'admin' WHERE id = 'admin'`); err != nil {
		return err
	}
	// Recordings: optional session name/description (from terminal session StartOptions).
	for _, alter := range []string{
		`ALTER TABLE recordings ADD COLUMN session_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE recordings ADD COLUMN session_description TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.ExecContext(ctx, alter); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	// User SSH public keys for SSH server (vantyx) public key authentication.
	_, _ = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user_ssh_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		key_line TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	)`)
	// File transfer protocol toggles (SFTP/FTP/TFTP). Stored in DB instead of tags.
	for _, alter := range []string{
		`ALTER TABLE targets ADD COLUMN sftp_enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE targets ADD COLUMN ftp_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE targets ADD COLUMN tftp_enabled INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.ExecContext(ctx, alter); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	// Migrate from tags to columns: no-sftp -> sftp_enabled=0, tftp_enabled tag -> tftp_enabled=1
	_, _ = db.ExecContext(ctx, `UPDATE targets SET sftp_enabled = 0 WHERE id IN (SELECT target_id FROM target_tags WHERE tag = 'no-sftp')`)
	_, _ = db.ExecContext(ctx, `UPDATE targets SET tftp_enabled = 1 WHERE id IN (SELECT target_id FROM target_tags WHERE tag = 'tftp_enabled')`)
	return nil
}
