package httpapi

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// TestMain wires up the environment expected by the test suite:
//
//   - VANTYX_ALLOW_PLAINTEXT_SECRETS=1: secret.LoadKeyFromEnvStrict
//     fails fast when the encryption key is missing (C-8). The test
//     suite intentionally runs without a key, so opt in to the legacy
//     "plaintext mode" the env switch was designed for.
//   - auth.SetBcryptCostForTests(MinCost): keep bcrypt fast after the
//     production default was bumped above bcrypt.DefaultCost (L-3).
func TestMain(m *testing.M) {
	_ = os.Setenv("VANTYX_ALLOW_PLAINTEXT_SECRETS", "1")
	auth.SetBcryptCostForTests(bcrypt.MinCost)
	os.Exit(m.Run())
}
