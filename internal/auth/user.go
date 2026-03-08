package auth

import (
	"errors"
	"time"

	"golang.org/x/crypto/ssh"
)

// Role constants.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User represents a local Vantyx user.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string // RoleAdmin or RoleUser
}

// UserSSHKey is a stored SSH public key for vantyx SSH server (public key auth).
type UserSSHKey struct {
	ID        int64
	UserID    string
	KeyLine   string   // one line in authorized_keys format (e.g. "ssh-ed25519 AAAA... comment")
	CreatedAt time.Time
}

// UserStore defines the behavior required for managing users.
type UserStore interface {
	CreateUser(id, username, plainPassword, role string) (*User, error)
	Authenticate(username, plainPassword string) (*User, error)
	AuthenticateByPublicKey(username string, key ssh.PublicKey) (*User, error)
	GetByID(id string) (*User, error)
	ListUsers(limit, offset int) ([]*User, error)
	TagsForUser(userID string) ([]string, error)
	SetUserTags(userID string, tags []string) error
	UpdatePassword(userID, currentPlain, newPlain string) error
	AddPublicKey(userID, keyLine string) (int64, error)
	ListPublicKeys(userID string) ([]UserSSHKey, error)
	DeletePublicKey(userID string, keyID int64) error
}

var (
	ErrUserExists        = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidSecret     = errors.New("invalid credentials")
	ErrWrongPassword     = errors.New("current password is wrong")
	ErrPasswordUnchanged = errors.New("new password must differ from current")
	ErrInvalidPublicKey  = errors.New("invalid SSH public key")
)
