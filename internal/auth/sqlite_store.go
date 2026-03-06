package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SQLiteUserStore implements UserStore backed by SQLite.
type SQLiteUserStore struct {
	db *sql.DB
}

// NewSQLiteUserStore creates a new SQLite-backed user store.
func NewSQLiteUserStore(db *sql.DB) *SQLiteUserStore {
	return &SQLiteUserStore{db: db}
}

// CreateUser inserts a new user with hashed password.
func (s *SQLiteUserStore) CreateUser(id, username, plainPassword string) (*User, error) {
	if id == "" || username == "" {
		return nil, errors.New("id and username must not be empty")
	}

	hash, err := HashPassword(plainPassword)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash)
		VALUES (?, ?, ?)
	`, id, username, hash)
	if err != nil {
		return nil, ErrUserExists
	}

	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: hash,
	}, nil
}

// Authenticate verifies username/password and returns the user on success.
func (s *SQLiteUserStore) Authenticate(username, plainPassword string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash
		FROM users
		WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.PasswordHash)
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
		SELECT id, username, password_hash
		FROM users
		WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.PasswordHash)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
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
