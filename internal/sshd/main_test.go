package sshd

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// TestMain pins the auth package's bcrypt cost to bcrypt.MinCost so
// CLI gateway tests do not pay the production work factor on every
// fixture (L-3).
func TestMain(m *testing.M) {
	_ = os.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", "1")
	auth.SetBcryptCostForTests(bcrypt.MinCost)
	os.Exit(m.Run())
}
