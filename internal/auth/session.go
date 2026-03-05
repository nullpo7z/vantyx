package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// Session represents an authenticated user session.
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionStore defines the behavior required for managing sessions.
type SessionStore interface {
	Create(userID string) (*Session, error)
	Get(id string) (*Session, error)
	Delete(id string)
}

var ErrSessionNotFound = errors.New("session not found")

func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
