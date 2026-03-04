package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = bcrypt.DefaultCost

var ErrEmptyPassword = errors.New("password must not be empty")

// HashPassword returns a bcrypt hash of the password.
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", ErrEmptyPassword
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hashed), nil
}

// VerifyPassword compares a bcrypt-hashed password with its possible plaintext equivalent.
func VerifyPassword(hashed, plain string) bool {
	if hashed == "" || plain == "" {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
	return err == nil
}
