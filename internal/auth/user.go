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
	// Locale is the user's preferred UI locale (BCP 47 short code, currently "en" or "ja").
	// Empty means "no preference"; UI clients should fall back to their own default.
	Locale string
	// ForcePasswordChange indicates that the user must rotate their
	// password before any other API call succeeds. Used for the
	// bootstrap admin account (ASVS V2.10.4 / CWE-1188).
	ForcePasswordChange bool
}

// UserSSHKey is a stored SSH public key for vantyx SSH server (public key auth).
type UserSSHKey struct {
	ID        int64
	UserID    string
	KeyLine   string // one line in authorized_keys format (e.g. "ssh-ed25519 AAAA... comment")
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
	UpdateLocale(userID, locale string) error
	AddPublicKey(userID, keyLine string) (int64, error)
	ListPublicKeys(userID string) ([]UserSSHKey, error)
	DeletePublicKey(userID string, keyID int64) error
	// SetForcePasswordChange flips the force-password-change flag for a
	// user. Returns ErrUserNotFound if the row does not exist.
	SetForcePasswordChange(userID string, force bool) error
	// DeleteUser removes the user row. Dependent rows (group membership,
	// tags, login sessions, SSH public keys, file transfer jobs) go with
	// it via ON DELETE CASCADE. Business rules such as "not yourself" and
	// "not the last admin" are enforced by the caller. Returns
	// ErrUserNotFound if the row does not exist.
	DeleteUser(userID string) error
}

// ErrInvalidLocale is returned when an UpdateLocale call receives a value
// that is not in the supported set (currently "", "en", "ja").
var ErrInvalidLocale = errors.New("unsupported locale")

var (
	ErrUserExists        = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidSecret     = errors.New("invalid credentials")
	ErrWrongPassword     = errors.New("current password is wrong")
	ErrPasswordUnchanged = errors.New("new password must differ from current")
	ErrInvalidPublicKey  = errors.New("invalid SSH public key")
)
