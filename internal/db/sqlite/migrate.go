package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// hasColumn reports whether table already has a column named col by
// inspecting SQLite's pragma_table_info view.
//
// Using a structured check is preferable to substring-matching the
// "duplicate column" error returned by ALTER TABLE: the error message
// has changed between SQLite versions and is also locale-sensitive
// (M-9 / CWE-754).
func hasColumn(ctx context.Context, db *sql.DB, table, col string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT name FROM pragma_table_info('%s')`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

// addColumnIfMissing applies an ALTER TABLE ADD COLUMN statement only
// when the target column does not yet exist. Safe to call multiple
// times during startup.
func addColumnIfMissing(ctx context.Context, db *sql.DB, table, col, ddl string) error {
	present, err := hasColumn(ctx, db, table, col)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		// Race: another startup path may have applied the column in
		// parallel. Fall back to the legacy error-string check so we
		// stay idempotent in that edge case.
		if strings.Contains(err.Error(), "duplicate column") {
			return nil
		}
		return err
	}
	return nil
}

const schemaMarkCurrent = "schema_v2026_05_28"

func hasMigrationMark(ctx context.Context, db *sql.DB, name string) bool {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM migration_marks WHERE name = ?`, name).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func setMigrationMark(ctx context.Context, db *sql.DB, name string) {
	_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO migration_marks(name) VALUES (?)`, name)
}

// Migrate applies the minimal schema required for Vantyx.
// It is safe to call multiple times; CREATE TABLE statements use IF NOT EXISTS.
func Migrate(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stmts := []string{
		// migration_marks records one-shot data migrations so they
		// don't re-run on every startup (which would otherwise let an
		// attacker reset privileged rows by recreating them).
		`CREATE TABLE IF NOT EXISTS migration_marks (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
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
		// Collaborative session invitations (Phase A: terminal only).
		// token_hash is SHA-256(token); plain tokens are never persisted.
		// invitee_user_id is NULL for link invitations and filled in
		// at consumption time for audit; the column carries the user
		// that actually joined either way.
		`CREATE TABLE IF NOT EXISTS session_invitations (
			id TEXT PRIMARY KEY,
			token_hash TEXT NOT NULL UNIQUE,
			session_id TEXT NOT NULL,
			session_kind TEXT NOT NULL,
			target_id TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			invitee_user_id TEXT,
			mode TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			used_at INTEGER,
			revoked_at INTEGER,
			created_at INTEGER NOT NULL
		);`,
		// Background file transfer jobs (persisted across restarts).
		`CREATE TABLE IF NOT EXISTS file_transfer_jobs (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			target_name TEXT NOT NULL DEFAULT '',
			backend TEXT NOT NULL,
			direction TEXT NOT NULL,
			remote_path TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL,
			progress INTEGER NOT NULL DEFAULT 0,
			total INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			temp_path TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Fast-path: the schema is already at the current version.
	// This avoids re-running dozens of pragma_table_info probes on every startup,
	// which is especially expensive in test suites that create many temp DBs.
	if hasMigrationMark(ctx, db, schemaMarkCurrent) {
		return nil
	}
	// Optional columns for targets (SSH credentials).
	for _, alter := range []struct {
		col, ddl string
	}{
		{"ssh_username", `ALTER TABLE targets ADD COLUMN ssh_username TEXT NOT NULL DEFAULT ''`},
		{"ssh_password", `ALTER TABLE targets ADD COLUMN ssh_password TEXT NOT NULL DEFAULT ''`},
		{"ssh_private_key", `ALTER TABLE targets ADD COLUMN ssh_private_key TEXT NOT NULL DEFAULT ''`},
		{"ssh_private_key_passphrase", `ALTER TABLE targets ADD COLUMN ssh_private_key_passphrase TEXT NOT NULL DEFAULT ''`},
	} {
		if err := addColumnIfMissing(ctx, db, "targets", alter.col, alter.ddl); err != nil {
			return err
		}
	}
	// User role: admin | user. Default user; existing id='admin' -> admin.
	if err := addColumnIfMissing(ctx, db, "users", "role", `ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`); err != nil {
		return err
	}
	// One-shot promotion of the bundled admin user. Guarded so we only
	// run it during the initial role-column rollout; subsequent
	// restarts must not re-elevate an id='admin' row created by a
	// malicious or accidental write.
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO migration_marks(name) VALUES ('admin_role_seed_v1')`); err == nil {
		var seeded int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM migration_marks WHERE name = 'admin_role_seed_v1'`).Scan(&seeded); err == nil && seeded == 1 {
			if _, err := db.ExecContext(ctx, `UPDATE users SET role = 'admin' WHERE id = 'admin'`); err != nil {
				return err
			}
		}
	}
	// force_password_change forces a rotation on the next login, used
	// for the bootstrap admin password (ASVS V2.10.4 / CWE-1188).
	if err := addColumnIfMissing(ctx, db, "users", "force_password_change",
		`ALTER TABLE users ADD COLUMN force_password_change INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	// Per-user UI locale (BCP 47 short code, currently '' | 'en' | 'ja').
	// Empty means "no preference" so the frontend falls back to its default.
	if err := addColumnIfMissing(ctx, db, "users", "locale",
		`ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	// Recordings: optional session name/description (from terminal session StartOptions).
	for _, alter := range []struct {
		col, ddl string
	}{
		{"session_name", `ALTER TABLE recordings ADD COLUMN session_name TEXT NOT NULL DEFAULT ''`},
		{"session_description", `ALTER TABLE recordings ADD COLUMN session_description TEXT NOT NULL DEFAULT ''`},
	} {
		if err := addColumnIfMissing(ctx, db, "recordings", alter.col, alter.ddl); err != nil {
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
	for _, alter := range []struct {
		col, ddl string
	}{
		{"sftp_enabled", `ALTER TABLE targets ADD COLUMN sftp_enabled INTEGER NOT NULL DEFAULT 1`},
		{"ftp_enabled", `ALTER TABLE targets ADD COLUMN ftp_enabled INTEGER NOT NULL DEFAULT 0`},
		{"tftp_enabled", `ALTER TABLE targets ADD COLUMN tftp_enabled INTEGER NOT NULL DEFAULT 0`},
		// SHA-256 fingerprint of the upstream SSH host key.
		// Required by sshproxy / sftp to prevent MITM (ASVS V2.6, CWE-295).
		{"ssh_host_key_fingerprint", `ALTER TABLE targets ADD COLUMN ssh_host_key_fingerprint TEXT NOT NULL DEFAULT ''`},
		// Per-target opt-out of host-key verification. Off by default;
		// the access store reads / writes the column already, but
		// historically the migration was missing so brand-new DBs
		// failed at the first SetSSHHostKeyInsecureSkipVerify call.
		{"ssh_host_key_insecure_skip_verify", `ALTER TABLE targets ADD COLUMN ssh_host_key_insecure_skip_verify INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := addColumnIfMissing(ctx, db, "targets", alter.col, alter.ddl); err != nil {
			return err
		}
	}
	// Migrate from tags to columns: no-sftp -> sftp_enabled=0, tftp_enabled tag -> tftp_enabled=1
	_, _ = db.ExecContext(ctx, `UPDATE targets SET sftp_enabled = 0 WHERE id IN (SELECT target_id FROM target_tags WHERE tag = 'no-sftp')`)
	_, _ = db.ExecContext(ctx, `UPDATE targets SET tftp_enabled = 1 WHERE id IN (SELECT target_id FROM target_tags WHERE tag = 'tftp_enabled')`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_audit_logs_time_id ON audit_logs(time DESC, id DESC)`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_command_logs_time_id ON command_logs(time DESC, id DESC)`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_file_transfer_jobs_user_updated ON file_transfer_jobs(user_id, updated_at DESC, id DESC)`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_session_invitations_session ON session_invitations(session_id)`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_session_invitations_invitee ON session_invitations(invitee_user_id)`)
	// Link invitations: max_uses NULL = unlimited; use_count tracks joins.
	if err := addColumnIfMissing(ctx, db, "session_invitations", "max_uses",
		`ALTER TABLE session_invitations ADD COLUMN max_uses INTEGER`); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, db, "session_invitations", "use_count",
		`ALTER TABLE session_invitations ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, db, "session_invitations", "invite_group_id",
		`ALTER TABLE session_invitations ADD COLUMN invite_group_id TEXT`); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, db, "session_invitations", "invite_tag",
		`ALTER TABLE session_invitations ADD COLUMN invite_tag TEXT`); err != nil {
		return err
	}
	// Legacy link rows without max_uses behave as single-use.
	_, _ = db.ExecContext(ctx, `UPDATE session_invitations SET max_uses = 1
		WHERE invitee_user_id IS NULL AND max_uses IS NULL`)
	setMigrationMark(ctx, db, schemaMarkCurrent)
	return nil
}
