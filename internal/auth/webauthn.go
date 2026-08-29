package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// WebAuthnCredential is a stored passkey / security key registered as a
// second factor. Only the public key is stored; the private key never
// leaves the authenticator.
type WebAuthnCredential struct {
	ID              []byte // raw credential ID
	UserID          string
	Name            string
	PublicKey       []byte
	AttestationType string
	AAGUID          []byte
	SignCount       uint32
	Transports      []string
	BackupEligible  bool
	BackedUp        bool
	CreatedAt       time.Time
	LastUsedAt      *time.Time
}

// IDString is the URL-safe base64 form used in API paths.
func (c WebAuthnCredential) IDString() string { return base64.RawURLEncoding.EncodeToString(c.ID) }

var ErrWebAuthnNotFound = errors.New("passkey not found")

const webauthnMaxPerUser = 10

// WebAuthnStore persists passkeys.
type WebAuthnStore interface {
	ListForUser(ctx context.Context, userID string) ([]WebAuthnCredential, error)
	Count(ctx context.Context, userID string) (int, error)
	Add(ctx context.Context, c WebAuthnCredential) error
	// UpdateSignCount records the counter and last use after a login.
	UpdateSignCount(ctx context.Context, id []byte, signCount uint32, backedUp bool) error
	Delete(ctx context.Context, userID string, id []byte) error
	DeleteAllForUser(ctx context.Context, userID string) (int64, error)
}

// SQLiteWebAuthnStore implements WebAuthnStore on user_webauthn_credentials.
type SQLiteWebAuthnStore struct{ db *sql.DB }

// NewSQLiteWebAuthnStore creates a store.
func NewSQLiteWebAuthnStore(db *sql.DB) *SQLiteWebAuthnStore { return &SQLiteWebAuthnStore{db: db} }

const webauthnCols = `id, user_id, name, public_key, attestation_type, aaguid, sign_count, transports, backup_eligible, backed_up, created_at, last_used_at`

func scanWebAuthn(sc interface{ Scan(...interface{}) error }) (*WebAuthnCredential, error) {
	var c WebAuthnCredential
	var idB64, transports string
	var created int64
	var lastUsed sql.NullInt64
	var eligible, backedUp int
	if err := sc.Scan(&idB64, &c.UserID, &c.Name, &c.PublicKey, &c.AttestationType, &c.AAGUID, &c.SignCount, &transports, &eligible, &backedUp, &created, &lastUsed); err != nil {
		return nil, err
	}
	id, err := base64.RawURLEncoding.DecodeString(idB64)
	if err != nil {
		return nil, err
	}
	c.ID = id
	_ = json.Unmarshal([]byte(transports), &c.Transports)
	c.BackupEligible, c.BackedUp = eligible != 0, backedUp != 0
	c.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		t := time.Unix(lastUsed.Int64, 0).UTC()
		c.LastUsedAt = &t
	}
	return &c, nil
}

func (s *SQLiteWebAuthnStore) ListForUser(ctx context.Context, userID string) ([]WebAuthnCredential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+webauthnCols+` FROM user_webauthn_credentials WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebAuthnCredential
	for rows.Next() {
		c, err := scanWebAuthn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *SQLiteWebAuthnStore) Count(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_webauthn_credentials WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func (s *SQLiteWebAuthnStore) Add(ctx context.Context, c WebAuthnCredential) error {
	if len(c.ID) == 0 || strings.TrimSpace(c.UserID) == "" {
		return ErrIDOrUsernameEmpty
	}
	n, err := s.Count(ctx, c.UserID)
	if err != nil {
		return err
	}
	if n >= webauthnMaxPerUser {
		return errors.New("too many passkeys for this user")
	}
	transports, _ := json.Marshal(c.Transports)
	if c.Transports == nil {
		transports = []byte("[]")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_webauthn_credentials (id, user_id, name, public_key, attestation_type, aaguid, sign_count, transports, backup_eligible, backed_up, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, c.IDString(), c.UserID, c.Name, c.PublicKey, c.AttestationType, c.AAGUID, c.SignCount, string(transports), boolInt(c.BackupEligible), boolInt(c.BackedUp), c.CreatedAt.Unix())
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *SQLiteWebAuthnStore) UpdateSignCount(ctx context.Context, id []byte, signCount uint32, backedUp bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_webauthn_credentials SET sign_count = ?, backed_up = ?, last_used_at = ? WHERE id = ?`, signCount, boolInt(backedUp), time.Now().Unix(), base64.RawURLEncoding.EncodeToString(id))
	return err
}

func (s *SQLiteWebAuthnStore) Delete(ctx context.Context, userID string, id []byte) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_webauthn_credentials WHERE id = ? AND user_id = ?`, base64.RawURLEncoding.EncodeToString(id), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrWebAuthnNotFound
	}
	return nil
}

func (s *SQLiteWebAuthnStore) DeleteAllForUser(ctx context.Context, userID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_webauthn_credentials WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
