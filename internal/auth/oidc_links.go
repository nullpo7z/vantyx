package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrOIDCLinkNotFound is returned when no local user is linked to the
// given issuer + subject.
var ErrOIDCLinkNotFound = errors.New("oidc identity not linked")

// OIDCLinkStore maps OIDC identities (issuer + subject) to local users.
type OIDCLinkStore interface {
	// Lookup returns the local user ID linked to issuer/subject.
	Lookup(ctx context.Context, issuer, subject string) (string, error)
	// Link records that issuer/subject signs in as userID. Idempotent.
	Link(ctx context.Context, issuer, subject, userID string) error
	// UnlinkUser removes every OIDC identity of userID.
	UnlinkUser(ctx context.Context, userID string) error
	// ManagedGroups returns the group IDs the IdP granted userID on the
	// last login (sorted).
	ManagedGroups(ctx context.Context, userID string) ([]string, error)
	// SetManagedGroups replaces the set of IdP-granted group IDs.
	SetManagedGroups(ctx context.Context, userID string, groupIDs []string) error
}

// SQLiteOIDCLinkStore implements OIDCLinkStore on user_oidc_links.
type SQLiteOIDCLinkStore struct{ db *sql.DB }

// NewSQLiteOIDCLinkStore creates a store.
func NewSQLiteOIDCLinkStore(db *sql.DB) *SQLiteOIDCLinkStore {
	return &SQLiteOIDCLinkStore{db: db}
}

func (s *SQLiteOIDCLinkStore) Lookup(ctx context.Context, issuer, subject string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM user_oidc_links WHERE issuer = ? AND subject = ?`, issuer, subject).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrOIDCLinkNotFound
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (s *SQLiteOIDCLinkStore) Link(ctx context.Context, issuer, subject, userID string) error {
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(userID) == "" {
		return ErrIDOrUsernameEmpty
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_oidc_links (issuer, subject, user_id) VALUES (?, ?, ?)
		ON CONFLICT(issuer, subject) DO UPDATE SET user_id = excluded.user_id
	`, issuer, subject, userID)
	return err
}

func (s *SQLiteOIDCLinkStore) UnlinkUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_oidc_links WHERE user_id = ?`, userID)
	return err
}

func (s *SQLiteOIDCLinkStore) ManagedGroups(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id FROM user_oidc_groups WHERE user_id = ? ORDER BY group_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *SQLiteOIDCLinkStore) SetManagedGroups(ctx context.Context, userID string, groupIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_oidc_groups WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, g := range groupIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_oidc_groups (user_id, group_id) VALUES (?, ?)`, userID, g); err != nil {
			return err
		}
	}
	return tx.Commit()
}
