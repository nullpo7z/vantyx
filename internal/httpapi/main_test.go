package httpapi

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// TestMain pins the auth package's bcrypt cost to bcrypt.MinCost so
// the suite stays fast after the production default was bumped above
// bcrypt.DefaultCost (L-3). Without this the per-test bcrypt work
// would add several minutes to every CI run.
func TestMain(m *testing.M) {
	auth.SetBcryptCostForTests(bcrypt.MinCost)
	os.Exit(m.Run())
}
