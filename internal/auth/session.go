package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"time"
)

// randReader is used by randomID; may be overridden in tests to trigger error paths.
var randReader io.Reader = rand.Reader

// Session represents an authenticated user session.
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionStore defines the behavior required for managing sessions.
//
// Implementations MUST persist the SHA-256 hash of the session token,
// not the token itself (ASVS V3.2.2 / CWE-312). The Session.ID
// returned by Create is the raw token to send to the client; Get
// performs the lookup by hashing the supplied value.
type SessionStore interface {
	Create(userID string) (*Session, error)
	Get(id string) (*Session, error)
	Delete(id string) error
	// DeleteAllForUser invalidates every session belonging to userID
	// except optionally keepID (pass "" to revoke them all). Used after
	// a password rotation to honour ASVS V3.3.1.
	DeleteAllForUser(userID, keepID string) error
}

var ErrSessionNotFound = errors.New("session not found")

func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := randReader.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
