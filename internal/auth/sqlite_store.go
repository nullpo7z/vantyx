package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
)

// SQLiteUserStore implements UserStore backed by SQLite.
type SQLiteUserStore struct {
	db *sql.DB
}

// NewSQLiteUserStore creates a new SQLite-backed user store.
func NewSQLiteUserStore(db *sql.DB) *SQLiteUserStore {
	return &SQLiteUserStore{db: db}
}

// CreateUser inserts a new user with hashed password and role.
func (s *SQLiteUserStore) CreateUser(id, username, plainPassword, role string) (*User, error) {
	if id == "" || username == "" {
		return nil, ErrIDOrUsernameEmpty
	}
	if role == "" {
		role = RoleUser
	}
	if role != RoleAdmin && role != RoleUser {
		role = RoleUser
	}

	hash, err := HashPassword(plainPassword)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role)
		VALUES (?, ?, ?, ?)
	`, id, username, hash, role)
	if err != nil {
		return nil, ErrUserExists
	}

	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: hash,
		Role:         role,
	}, nil
}

// dummyBcryptHash is used by Authenticate to equalize the timing
// between "user does not exist" and "wrong password" so an attacker
// cannot enumerate usernames by latency (CWE-208).
//
// The constant is a bcrypt hash of an unguessable random string.
// VerifyPassword always returns false for any real password.
const dummyBcryptHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy" //nolint:gosec // dummy hash; no real secret

// Authenticate verifies username/password and returns the user on success.
//
// Always runs bcrypt against either the real or dummy hash so the
// response time is independent of whether the username exists
// (CWE-208 user enumeration).
func (s *SQLiteUserStore) Authenticate(username, plainPassword string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var u User
	var forcePW int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, COALESCE(role, 'user'), COALESCE(locale, ''), COALESCE(force_password_change, 0)
		FROM users
		WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Locale, &forcePW)
	if err == sql.ErrNoRows {
		// Run bcrypt against a dummy hash so the latency matches.
		_ = VerifyPassword(dummyBcryptHash, plainPassword)
		return nil, ErrInvalidSecret
	}
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(u.PasswordHash, plainPassword) {
		return nil, ErrInvalidSecret
	}
	u.ForcePasswordChange = forcePW != 0
	return &u, nil
}

// GetByID returns a user by ID.
func (s *SQLiteUserStore) GetByID(id string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var u User
	var forcePW int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, COALESCE(role, 'user'), COALESCE(locale, ''), COALESCE(force_password_change, 0)
		FROM users
		WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Locale, &forcePW)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	u.ForcePasswordChange = forcePW != 0
	return &u, nil
}

// ListUsers returns users with the given limit and offset. If limit <= 0, 100 is used.
func (s *SQLiteUserStore) ListUsers(limit, offset int) ([]*User, error) {
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, COALESCE(role, 'user'), COALESCE(locale, ''), COALESCE(force_password_change, 0)
		FROM users
		ORDER BY username
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		var u User
		var forcePW int
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Locale, &forcePW); err != nil {
			return nil, err
		}
		u.ForcePasswordChange = forcePW != 0
		out = append(out, &u)
	}
	return out, rows.Err()
}

// SetForcePasswordChange flips the force_password_change flag.
func (s *SQLiteUserStore) SetForcePasswordChange(userID string, force bool) error {
	if strings.TrimSpace(userID) == "" {
		return ErrUserNotFound
	}
	val := 0
	if force {
		val = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `UPDATE users SET force_password_change = ? WHERE id = ?`, val, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrUserNotFound
	}
	return nil
}

// supportedUILocales lists the locale codes the UI currently ships translations for.
// An empty string ("") means "no preference" and is always accepted.
var supportedUILocales = map[string]struct{}{
	"":   {},
	"en": {},
	"ja": {},
}

// NormalizeUILocale returns a canonical, validated locale code or ErrInvalidLocale.
// Callers receive an empty string when the user clears their preference.
func NormalizeUILocale(locale string) (string, error) {
	loc := strings.ToLower(strings.TrimSpace(locale))
	if _, ok := supportedUILocales[loc]; !ok {
		return "", ErrInvalidLocale
	}
	return loc, nil
}

// UpdateLocale persists the user's UI locale preference. Pass "" to clear it.
func (s *SQLiteUserStore) UpdateLocale(userID, locale string) error {
	if strings.TrimSpace(userID) == "" {
		return ErrUserNotFound
	}
	loc, err := NormalizeUILocale(locale)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `UPDATE users SET locale = ? WHERE id = ?`, loc, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrUserNotFound
	}
	return nil
}

const maxUserTagLen = 64

func validateUserTag(tag string) error {
	if tag == "" || len(tag) > maxUserTagLen {
		return ErrTagLength
	}
	for _, r := range tag {
		if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return ErrTagChars
		}
	}
	return nil
}

// TagsForUser returns tags assigned to the user.
func (s *SQLiteUserStore) TagsForUser(userID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT tag FROM user_tags WHERE user_id = ? ORDER BY tag`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// SetUserTags replaces all tags for the user. Pass nil or empty to clear.
func (s *SQLiteUserStore) SetUserTags(userID string, tags []string) error {
	if userID == "" {
		return ErrUserNotFound
	}
	for _, tag := range tags {
		if err := validateUserTag(tag); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrUserNotFound
		}
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM user_tags WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO user_tags (user_id, tag) VALUES (?, ?)`, userID, tag); err != nil {
			return err
		}
	}
	return nil
}

// UpdatePassword updates the user's password after verifying current (ASVS default password change).
func (s *SQLiteUserStore) UpdatePassword(userID, currentPlain, newPlain string) error {
	if err := ValidatePassword(newPlain); err != nil {
		return err
	}
	if currentPlain == newPlain {
		return ErrPasswordUnchanged
	}
	u, err := s.GetByID(userID)
	if err != nil {
		return err
	}
	if !VerifyPassword(u.PasswordHash, currentPlain) {
		return ErrWrongPassword
	}
	hash, err := HashPassword(newPlain)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrUserNotFound
	}
	return nil
}

// AddPublicKey adds an SSH public key (authorized_keys line) for the user. Returns key ID or error.
func (s *SQLiteUserStore) AddPublicKey(userID, keyLine string) (int64, error) {
	keyLine = strings.TrimSpace(keyLine)
	if keyLine == "" {
		return 0, ErrInvalidPublicKey
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine)); err != nil {
		return 0, ErrInvalidPublicKey
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrUserNotFound
		}
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO user_ssh_keys (user_id, key_line) VALUES (?, ?)`, userID, keyLine)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListPublicKeys returns all stored SSH public keys for the user.
func (s *SQLiteUserStore) ListPublicKeys(userID string) ([]UserSSHKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, key_line, created_at FROM user_ssh_keys WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSSHKey
	for rows.Next() {
		var k UserSSHKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyLine, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeletePublicKey removes an SSH public key. The key must belong to the user.
func (s *SQLiteUserStore) DeletePublicKey(userID string, keyID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_ssh_keys WHERE user_id = ? AND id = ?`, userID, keyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrUserNotFound
	}
	return nil
}

// AuthenticateByPublicKey authenticates a user by SSH public key (username + key). Returns the user on success.
func (s *SQLiteUserStore) AuthenticateByPublicKey(username string, key ssh.PublicKey) (*User, error) {
	if username == "" || key == nil {
		return nil, ErrInvalidSecret
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, COALESCE(role, 'user'), COALESCE(locale, '') FROM users WHERE username = ?`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Locale)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidSecret
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key_line FROM user_ssh_keys WHERE user_id = ?`, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keyMarshal := key.Marshal()
	for rows.Next() {
		var keyLine string
		if err := rows.Scan(&keyLine); err != nil {
			return nil, err
		}
		stored, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyLine))
		if err != nil {
			continue
		}
		if bytes.Equal(stored.Marshal(), keyMarshal) {
			return &u, nil
		}
	}
	return nil, ErrInvalidSecret
}

// SQLiteSessionStore implements SessionStore backed by SQLite.
type SQLiteSessionStore struct {
	db  *sql.DB
	ttl time.Duration
}

// NewSQLiteSessionStore creates a new SQLite-backed session store.
func NewSQLiteSessionStore(db *sql.DB, ttl time.Duration) *SQLiteSessionStore {
	return &SQLiteSessionStore{db: db, ttl: ttl}
}

// hashSessionToken returns the SHA-256 hex digest of the raw token.
// The database column "sessions.id" holds this digest, never the raw
// token (ASVS V3.2.2 / CWE-312).
func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create creates a new session for the given user ID.
// The returned Session.ID is the raw token to send to the client; the
// database row keyed by the SHA-256 hash of that token.
func (s *SQLiteSessionStore) Create(userID string) (*Session, error) {
	if userID == "" {
		return nil, errors.New("userID must not be empty")
	}
	raw, err := randomID(32)
	if err != nil {
		return nil, err
	}
	hashed := hashSessionToken(raw)
	now := time.Now().UTC()
	expires := now.Add(s.ttl)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, hashed, userID, now, expires)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:        raw,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expires,
	}, nil
}

// Get returns a valid (non-expired) session by raw token. The store
// hashes the token before looking it up, so a database leak does not
// expose usable credentials.
func (s *SQLiteSessionStore) Get(rawToken string) (*Session, error) {
	if rawToken == "" {
		return nil, ErrSessionNotFound
	}
	hashed := hashSessionToken(rawToken)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var sess Session
	err := s.db.QueryRowContext(ctx, `
		SELECT user_id, created_at, expires_at
		FROM sessions
		WHERE id = ?
	`, hashed).Scan(&sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = s.Delete(rawToken)
		return nil, ErrSessionNotFound
	}
	// Return the raw token to keep parity with Create(); callers that
	// surface session IDs externally should be re-checked, but the
	// existing code paths use ID only for logging.
	sess.ID = rawToken
	return &sess, nil
}

// Delete removes a session identified by its raw token. Returns nil
// when the row is gone (idempotent) and a non-nil error only on
// database failures so the caller can audit them.
func (s *SQLiteSessionStore) Delete(rawToken string) error {
	if rawToken == "" {
		return nil
	}
	hashed := hashSessionToken(rawToken)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, hashed)
	return err
}

// DeleteAllForUser invalidates every session for userID, optionally
// keeping the one whose raw token matches keepToken. Called from the
// password change flow (ASVS V3.3.1 / CWE-613).
func (s *SQLiteSessionStore) DeleteAllForUser(userID, keepToken string) error {
	if userID == "" {
		return errors.New("userID must not be empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if keepToken == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
		return err
	}
	keepHash := hashSessionToken(keepToken)
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keepHash)
	return err
}
