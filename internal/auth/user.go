package auth

import (
	"errors"
	"sync"
)

// User represents a local Vantyx user.
type User struct {
	ID           string
	Username     string
	PasswordHash string
}

// InMemoryUserStore is a simple thread-safe user store for initial Phase 1.
type InMemoryUserStore struct {
	mu     sync.RWMutex
	byID   map[string]*User
	byName map[string]*User
}

var (
	ErrUserExists    = errors.New("user already exists")
	ErrUserNotFound  = errors.New("user not found")
	ErrInvalidSecret = errors.New("invalid credentials")
)

// NewInMemoryUserStore creates an empty user store.
func NewInMemoryUserStore() *InMemoryUserStore {
	return &InMemoryUserStore{
		byID:   make(map[string]*User),
		byName: make(map[string]*User),
	}
}

// CreateUser inserts a new user with hashed password.
func (s *InMemoryUserStore) CreateUser(id, username, plainPassword string) (*User, error) {
	if id == "" || username == "" {
		return nil, errors.New("id and username must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[id]; ok {
		return nil, ErrUserExists
	}
	if _, ok := s.byName[username]; ok {
		return nil, ErrUserExists
	}

	hash, err := HashPassword(plainPassword)
	if err != nil {
		return nil, err
	}

	u := &User{
		ID:           id,
		Username:     username,
		PasswordHash: hash,
	}
	s.byID[id] = u
	s.byName[username] = u
	return u, nil
}

// Authenticate verifies username/password and returns the user on success.
func (s *InMemoryUserStore) Authenticate(username, plainPassword string) (*User, error) {
	s.mu.RLock()
	u, ok := s.byName[username]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrInvalidSecret
	}
	if !VerifyPassword(u.PasswordHash, plainPassword) {
		return nil, ErrInvalidSecret
	}
	return u, nil
}
