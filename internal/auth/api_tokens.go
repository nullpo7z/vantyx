package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// API tokens are long-lived bearer credentials for automation
// (scripts, CI, monitoring). Only the SHA-256 of a token is stored; the
// plain value is shown once at creation. Scope "read" allows GET/HEAD
// only, "write" allows every method the owning user may call.

const (
	APITokenPrefix     = "vtx_"
	APITokenScopeRead  = "read"
	APITokenScopeWrite = "write"
	apiTokenMaxPerUser = 50
)

var (
	ErrAPITokenNotFound = errors.New("api token not found")
	ErrAPITokenInvalid  = errors.New("api token invalid, expired or revoked")
	ErrAPITokenLimit    = errors.New("too many api tokens for this user")
	ErrAPITokenScope    = errors.New("scope must be read or write")
)

// APIToken is the stored record (never includes the plain token).
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	Prefix     string // first 12 characters of the plain token, for display
	Scope      string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// Active reports whether the token may authenticate at now.
func (t APIToken) Active(now time.Time) bool {
	if t.RevokedAt != nil {
		return false
	}
	if t.ExpiresAt != nil && !now.Before(*t.ExpiresAt) {
		return false
	}
	return true
}

// APITokenStore persists API tokens.
type APITokenStore interface {
	// Create stores a new token and returns the record plus the plain
	// token (shown once).
	Create(ctx context.Context, userID, name, scope string, expiresAt *time.Time) (*APIToken, string, error)
	// Authenticate resolves a plain token to its active record.
	Authenticate(ctx context.Context, plain string) (*APIToken, error)
	// Touch records last use (best-effort).
	Touch(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]APIToken, error)
	// Revoke marks the token revoked; userID "" skips the ownership check.
	Revoke(ctx context.Context, userID, id string) error
}

// HashAPIToken returns the hex SHA-256 of a plain token.
func HashAPIToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// SQLiteAPITokenStore implements APITokenStore on api_tokens.
type SQLiteAPITokenStore struct{ db *sql.DB }

// NewSQLiteAPITokenStore creates a store.
func NewSQLiteAPITokenStore(db *sql.DB) *SQLiteAPITokenStore {
	return &SQLiteAPITokenStore{db: db}
}

func (s *SQLiteAPITokenStore) Create(ctx context.Context, userID, name, scope string, expiresAt *time.Time) (*APIToken, string, error) {
	userID = strings.TrimSpace(userID)
	name = strings.TrimSpace(name)
	if userID == "" || name == "" {
		return nil, "", ErrIDOrUsernameEmpty
	}
	if scope != APITokenScopeRead && scope != APITokenScopeWrite {
		return nil, "", ErrAPITokenScope
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL`, userID).Scan(&n); err != nil {
		return nil, "", err
	}
	if n >= apiTokenMaxPerUser {
		return nil, "", ErrAPITokenLimit
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	plain := APITokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	idRaw := make([]byte, 9)
	if _, err := rand.Read(idRaw); err != nil {
		return nil, "", err
	}
	id := base64.RawURLEncoding.EncodeToString(idRaw)
	now := time.Now().UTC()
	var exp interface{}
	if expiresAt != nil {
		e := expiresAt.UTC()
		exp = e.Unix()
		expiresAt = &e
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, scope, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, userID, name, HashAPIToken(plain), plain[:12], scope, now.Unix(), exp)
	if err != nil {
		return nil, "", err
	}
	return &APIToken{ID: id, UserID: userID, Name: name, Prefix: plain[:12], Scope: scope, CreatedAt: now, ExpiresAt: expiresAt}, plain, nil
}

const apiTokenCols = `id, user_id, name, prefix, scope, created_at, expires_at, last_used_at, revoked_at` // #nosec G101 -- SQL column list, not a credential

func scanAPIToken(sc interface{ Scan(...interface{}) error }) (*APIToken, error) {
	var t APIToken
	var created int64
	var exp, used, revoked sql.NullInt64
	if err := sc.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.Scope, &created, &exp, &used, &revoked); err != nil {
		return nil, err
	}
	t.CreatedAt = time.Unix(created, 0).UTC()
	if exp.Valid {
		v := time.Unix(exp.Int64, 0).UTC()
		t.ExpiresAt = &v
	}
	if used.Valid {
		v := time.Unix(used.Int64, 0).UTC()
		t.LastUsedAt = &v
	}
	if revoked.Valid {
		v := time.Unix(revoked.Int64, 0).UTC()
		t.RevokedAt = &v
	}
	return &t, nil
}

func (s *SQLiteAPITokenStore) Authenticate(ctx context.Context, plain string) (*APIToken, error) {
	plain = strings.TrimSpace(plain)
	if !strings.HasPrefix(plain, APITokenPrefix) || len(plain) < 20 {
		return nil, ErrAPITokenInvalid
	}
	t, err := scanAPIToken(s.db.QueryRowContext(ctx, `SELECT `+apiTokenCols+` FROM api_tokens WHERE token_hash = ?`, HashAPIToken(plain)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPITokenInvalid
	}
	if err != nil {
		return nil, err
	}
	if !t.Active(time.Now()) {
		return nil, ErrAPITokenInvalid
	}
	return t, nil
}

func (s *SQLiteAPITokenStore) Touch(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

func (s *SQLiteAPITokenStore) ListForUser(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+apiTokenCols+` FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *SQLiteAPITokenStore) Revoke(ctx context.Context, userID, id string) error {
	q := `UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`
	args := []interface{}{time.Now().Unix(), id}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrAPITokenNotFound
	}
	return nil
}
