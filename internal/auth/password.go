package auth

import (
	"errors"
	"fmt"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the cost passed to bcrypt; may be overridden in tests to trigger error paths.
var bcryptCost = bcrypt.DefaultCost

// MinPasswordLength is the minimum acceptable length for a user
// password (ASVS V2.3). HTTP handlers reference this when formatting
// the localized "password too short" message.
const MinPasswordLength = 8

var (
	ErrEmptyPassword     = errors.New("password must not be empty")
	ErrPasswordTooShort  = errors.New("password must be at least 8 characters")
	ErrPasswordNoUpper   = errors.New("password must contain at least one uppercase letter")
	ErrPasswordNoLower   = errors.New("password must contain at least one lowercase letter")
	ErrPasswordNoDigit   = errors.New("password must contain at least one digit")
	ErrPasswordNoSpecial = errors.New("password must contain at least one special character")
)

// ValidatePassword checks password policy (ASVS V2.3): min 8 chars, upper, lower, digit, special.
func ValidatePassword(plain string) error {
	if plain == "" {
		return ErrEmptyPassword
	}
	if len(plain) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range plain {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}
	if !hasUpper {
		return ErrPasswordNoUpper
	}
	if !hasLower {
		return ErrPasswordNoLower
	}
	if !hasDigit {
		return ErrPasswordNoDigit
	}
	if !hasSpecial {
		return ErrPasswordNoSpecial
	}
	return nil
}

// HashPassword returns a bcrypt hash of the password.
func HashPassword(plain string) (string, error) {
	if err := ValidatePassword(plain); err != nil {
		return "", err
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
