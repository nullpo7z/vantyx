package auth

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the cost passed to bcrypt. Bumped above
// bcrypt.DefaultCost (10) so newly created hashes get ~4x the work
// factor recommended by current guidance (OWASP / NIST 800-63B). Tests
// may override via VANTYX_BCRYPT_COST=4 (bcrypt's MinCost) to keep
// suites fast (L-3 / CWE-916).
var bcryptCost = func() int {
	const fallback = 12
	raw := os.Getenv("VANTYX_BCRYPT_COST")
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < bcrypt.MinCost || v > bcrypt.MaxCost {
		return fallback
	}
	return v
}()

// MinPasswordLength is the minimum acceptable length for a user
// password (ASVS V2.3). HTTP handlers reference this when formatting
// the localized "password too short" message.
const MinPasswordLength = 8

// MaxPasswordLength matches bcrypt's 72-byte limit. Inputs longer
// than that would be silently truncated by bcrypt.GenerateFromPassword,
// which is a well-known footgun (CWE-916); we reject them at the
// policy layer so users always get the protection they expect.
const MaxPasswordLength = 72

var (
	ErrEmptyPassword     = errors.New("password must not be empty")
	ErrPasswordTooShort  = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong   = errors.New("password must be at most 72 bytes")
	ErrPasswordNoUpper   = errors.New("password must contain at least one uppercase letter")
	ErrPasswordNoLower   = errors.New("password must contain at least one lowercase letter")
	ErrPasswordNoDigit   = errors.New("password must contain at least one digit")
	ErrPasswordNoSpecial = errors.New("password must contain at least one special character")
)

// ValidatePassword checks password policy (ASVS V2.3): min 8 chars,
// max 72 bytes (bcrypt cap), upper, lower, digit, special.
func ValidatePassword(plain string) error {
	if plain == "" {
		return ErrEmptyPassword
	}
	if len(plain) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(plain) > MaxPasswordLength {
		return ErrPasswordTooLong
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

// VerifyPassword compares a bcrypt-hashed password with its possible
// plaintext equivalent. Inputs longer than the bcrypt 72-byte cap are
// rejected outright to prevent confusion with HashPassword's policy
// (CWE-916); they would be silently truncated otherwise.
func VerifyPassword(hashed, plain string) bool {
	if hashed == "" || plain == "" {
		return false
	}
	if len(plain) > MaxPasswordLength {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
	return err == nil
}
