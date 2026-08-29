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
		`CREATE TABLE IF NOT EXISTS session_invitation_consumers (
			invitation_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			consumed_at INTEGER NOT NULL,
			PRIMARY KEY (invitation_id, user_id),
			FOREIGN KEY (invitation_id) REFERENCES session_invitations(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_session_invitation_consumers_user ON session_invitation_consumers(user_id);`,
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
		// User SSH public keys for SSH server (vantyx) public key authentication.
		`CREATE TABLE IF NOT EXISTS user_ssh_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			key_line TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		// Credential library: Keys (private key material) and Identities (login profiles).
		`CREATE TABLE IF NOT EXISTS ssh_keys (
			id TEXT PRIMARY KEY,
			label TEXT NOT NULL,
			key_type TEXT NOT NULL DEFAULT '',
			ssh_private_key TEXT NOT NULL DEFAULT '',
			ssh_private_key_passphrase TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ssh_keys_label ON ssh_keys(label)`,
		`CREATE TABLE IF NOT EXISTS credential_identities (
			id TEXT PRIMARY KEY,
			label TEXT NOT NULL,
			ssh_username TEXT NOT NULL DEFAULT '',
			ssh_password TEXT NOT NULL DEFAULT '',
			ssh_key_id TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (ssh_key_id) REFERENCES ssh_keys(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_credential_identities_label ON credential_identities(label)`,
		// Per-user TOTP second factor. secret_enc is the base32 secret
		// encrypted with VANTYX_SSH_PASSWORD_ENCRYPTION_KEY; enabled=0
		// means enrolment started but the first code was not confirmed
		// yet. recovery_codes is a JSON array of SHA-256 hex digests of
		// unused one-time recovery codes.
		`CREATE TABLE IF NOT EXISTS user_totp (
			user_id TEXT PRIMARY KEY,
			secret_enc TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			recovery_codes TEXT NOT NULL DEFAULT '[]',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			confirmed_at TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		// OIDC identities linked to local users (issuer + subject is the
		// stable identity; usernames/emails at the IdP may change).
		`CREATE TABLE IF NOT EXISTS user_oidc_links (
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			user_id TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (issuer, subject),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_oidc_links_user ON user_oidc_links(user_id)`,
		// Group memberships granted by the IdP's groups claim (see
		// VANTYX_OIDC_GROUP_MAP). Tracked separately from user_groups so a
		// login that no longer carries a group revokes only what OIDC
		// granted, never memberships an admin added by hand.
		// Access requests: a user asks for (time-limited) membership of a
		// group; an admin approves (creating the membership) or denies.
		`CREATE TABLE IF NOT EXISTS access_requests (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at INTEGER NOT NULL,
			decided_at INTEGER,
			decided_by TEXT,
			decision_note TEXT,
			expires_at INTEGER,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_access_requests_status ON access_requests(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_access_requests_user ON access_requests(user_id, created_at)`,
		// API tokens (bearer credentials for automation); only the SHA-256
		// of the token is stored.
		// WebAuthn / passkey credentials (second factor); public keys only.
		`CREATE TABLE IF NOT EXISTS user_webauthn_credentials (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			public_key BLOB NOT NULL,
			attestation_type TEXT NOT NULL DEFAULT '',
			aaguid BLOB,
			sign_count INTEGER NOT NULL DEFAULT 0,
			transports TEXT NOT NULL DEFAULT '[]',
			backup_eligible INTEGER NOT NULL DEFAULT 0,
			backed_up INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_webauthn_user ON user_webauthn_credentials(user_id)`,
		`CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			prefix TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'read',
			created_at INTEGER NOT NULL,
			expires_at INTEGER,
			last_used_at INTEGER,
			revoked_at INTEGER,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id)`,
		`CREATE TABLE IF NOT EXISTS user_oidc_groups (
			user_id TEXT NOT NULL,
			group_id TEXT NOT NULL,
			PRIMARY KEY (user_id, group_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE
		)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Idempotent column patches must run even when schemaMarkCurrent is set;
	// otherwise DBs that received the mark before a column was introduced
	// skip ALTER TABLE and handlers fail (e.g. ssh_keys.key_type).
	if err := applyAdditiveColumnPatches(ctx, db); err != nil {
		return err
	}
	// Fast-path: the schema is already at the current version.
	// This avoids re-running dozens of pragma_table_info probes on every startup,
	// which is especially expensive in test suites that create many temp DBs.
	if hasMigrationMark(ctx, db, schemaMarkCurrent) {
		return nil
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
	// Legacy link rows without max_uses behave as single-use.
	_, _ = db.ExecContext(ctx, `UPDATE session_invitations SET max_uses = 1
		WHERE invitee_user_id IS NULL AND max_uses IS NULL`)
	setMigrationMark(ctx, db, schemaMarkCurrent)
	return nil
}

// applyAdditiveColumnPatches adds columns introduced after older schema
// marks were written. Safe on every startup.
func applyAdditiveColumnPatches(ctx context.Context, db *sql.DB) error {
	for _, alter := range []struct {
		table, col, ddl string
	}{
		{"targets", "ssh_username", `ALTER TABLE targets ADD COLUMN ssh_username TEXT NOT NULL DEFAULT ''`},
		{"targets", "ssh_password", `ALTER TABLE targets ADD COLUMN ssh_password TEXT NOT NULL DEFAULT ''`},
		{"targets", "ssh_private_key", `ALTER TABLE targets ADD COLUMN ssh_private_key TEXT NOT NULL DEFAULT ''`},
		{"targets", "ssh_private_key_passphrase", `ALTER TABLE targets ADD COLUMN ssh_private_key_passphrase TEXT NOT NULL DEFAULT ''`},
		{"users", "role", `ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`},
		{"users", "force_password_change", `ALTER TABLE users ADD COLUMN force_password_change INTEGER NOT NULL DEFAULT 0`},
		{"users", "locale", `ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT ''`},
		// Suspended accounts keep their rows but cannot authenticate.
		{"users", "disabled", `ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0`},
		// Optional membership expiry (unix seconds, NULL = permanent). Expired
		// rows grant nothing; they stay visible to admins until purged.
		{"user_groups", "expires_at", `ALTER TABLE user_groups ADD COLUMN expires_at INTEGER`},
		{"recordings", "session_name", `ALTER TABLE recordings ADD COLUMN session_name TEXT NOT NULL DEFAULT ''`},
		{"recordings", "session_description", `ALTER TABLE recordings ADD COLUMN session_description TEXT NOT NULL DEFAULT ''`},
		{"targets", "sftp_enabled", `ALTER TABLE targets ADD COLUMN sftp_enabled INTEGER NOT NULL DEFAULT 1`},
		{"targets", "ftp_enabled", `ALTER TABLE targets ADD COLUMN ftp_enabled INTEGER NOT NULL DEFAULT 0`},
		{"targets", "tftp_enabled", `ALTER TABLE targets ADD COLUMN tftp_enabled INTEGER NOT NULL DEFAULT 0`},
		{"targets", "ssh_host_key_fingerprint", `ALTER TABLE targets ADD COLUMN ssh_host_key_fingerprint TEXT NOT NULL DEFAULT ''`},
		{"targets", "ssh_host_key_insecure_skip_verify", `ALTER TABLE targets ADD COLUMN ssh_host_key_insecure_skip_verify INTEGER NOT NULL DEFAULT 0`},
		{"targets", "credential_identity_id", `ALTER TABLE targets ADD COLUMN credential_identity_id TEXT NOT NULL DEFAULT ''`},
		{"targets", "ssh_key_id", `ALTER TABLE targets ADD COLUMN ssh_key_id TEXT NOT NULL DEFAULT ''`},
		{"session_invitations", "max_uses", `ALTER TABLE session_invitations ADD COLUMN max_uses INTEGER`},
		{"session_invitations", "use_count", `ALTER TABLE session_invitations ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`},
		{"session_invitations", "invite_group_id", `ALTER TABLE session_invitations ADD COLUMN invite_group_id TEXT`},
		{"session_invitations", "invite_tag", `ALTER TABLE session_invitations ADD COLUMN invite_tag TEXT`},
		{"ssh_keys", "key_type", `ALTER TABLE ssh_keys ADD COLUMN key_type TEXT NOT NULL DEFAULT ''`},
	} {
		if err := addColumnIfMissing(ctx, db, alter.table, alter.col, alter.ddl); err != nil {
			return err
		}
	}
	return nil
}
