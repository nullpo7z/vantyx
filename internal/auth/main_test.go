package auth

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain pins bcryptCost to bcrypt.MinCost so the suite stays fast
// after the production default was bumped to 12 (L-3). Individual
// tests that need a specific value still mutate the package variable
// directly and restore the previous value with defer.
func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}
