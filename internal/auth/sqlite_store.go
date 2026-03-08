package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode"
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
		return nil, errors.New("id and username must not be empty")
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

// Authenticate verifies username/password and returns the user on success.
func (s *SQLiteUserStore) Authenticate(username, plainPassword string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, COALESCE(role, 'user')
		FROM users
		WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err == sql.ErrNoRows {
		return nil, ErrInvalidSecret
	}
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(u.PasswordHash, plainPassword) {
		return nil, ErrInvalidSecret
	}
	return &u, nil
}

// GetByID returns a user by ID.
func (s *SQLiteUserStore) GetByID(id string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, COALESCE(role, 'user')
		FROM users
		WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
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
		SELECT id, username, password_hash, COALESCE(role, 'user')
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
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

const maxUserTagLen = 64

func validateUserTag(tag string) error {
	if tag == "" || len(tag) > maxUserTagLen {
		return errors.New("tag must be 1–64 characters")
	}
	for _, r := range tag {
		if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return errors.New("tag may only contain letters, numbers, hyphen, underscore")
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

// SQLiteSessionStore implements SessionStore backed by SQLite.
type SQLiteSessionStore struct {
	db  *sql.DB
	ttl time.Duration
}

// NewSQLiteSessionStore creates a new SQLite-backed session store.
func NewSQLiteSessionStore(db *sql.DB, ttl time.Duration) *SQLiteSessionStore {
	return &SQLiteSessionStore{db: db, ttl: ttl}
}

// Create creates a new session for the given user ID.
func (s *SQLiteSessionStore) Create(userID string) (*Session, error) {
	if userID == "" {
		return nil, errors.New("userID must not be empty")
	}
	id, err := randomID(32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expires := now.Add(s.ttl)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, id, userID, now, expires)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:        id,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expires,
	}, nil
}

// Get returns a valid (non-expired) session by ID.
func (s *SQLiteSessionStore) Get(id string) (*Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var sess Session
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, created_at, expires_at
		FROM sessions
		WHERE id = ?
	`, id).Scan(&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		s.Delete(id)
		return nil, ErrSessionNotFound
	}
	return &sess, nil
}

// Delete removes a session by ID.
func (s *SQLiteSessionStore) Delete(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
}
