package auth

import "errors"

// User represents a local Vantyx user.
type User struct {
	ID           string
	Username     string
	PasswordHash string
}

// UserStore defines the behavior required for managing users.
type UserStore interface {
	CreateUser(id, username, plainPassword string) (*User, error)
	Authenticate(username, plainPassword string) (*User, error)
	GetByID(id string) (*User, error)
	UpdatePassword(userID, currentPlain, newPlain string) error
}

var (
	ErrUserExists        = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidSecret     = errors.New("invalid credentials")
	ErrWrongPassword     = errors.New("current password is wrong")
	ErrPasswordUnchanged = errors.New("new password must differ from current")
)
