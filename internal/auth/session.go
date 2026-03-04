package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Session represents an authenticated user session.
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// InMemorySessionStore keeps sessions in memory for Phase 1.
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
	now      func() time.Time
}

var ErrSessionNotFound = errors.New("session not found")

// NewInMemorySessionStore creates a new session store.
func NewInMemorySessionStore(ttl time.Duration) *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
		now:      time.Now,
	}
}

// Create creates a new session for the given user ID.
func (s *InMemorySessionStore) Create(userID string) (*Session, error) {
	if userID == "" {
		return nil, errors.New("userID must not be empty")
	}
	id, err := randomID(32)
	if err != nil {
		return nil, err
	}
	now := s.now()
	sess := &Session{
		ID:        id,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	return sess, nil
}

// Get returns a valid (non-expired) session by ID.
func (s *InMemorySessionStore) Get(id string) (*Session, error) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	if s.now().After(sess.ExpiresAt) {
		s.Delete(id)
		return nil, ErrSessionNotFound
	}
	return sess, nil
}

// Delete removes a session by ID.
func (s *InMemorySessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
