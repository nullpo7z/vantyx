package httpapi

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/nullpo7z/vantyx/internal/auth"
)

var httpapiTestDBTemplate []byte

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
	// Build a ready-to-use SQLite template once. Individual tests copy it
	// into their TempDir, avoiding repeated schema migrations.
	func() {
		dir, err := os.MkdirTemp("", "vantyx-httpapi-testdb-*")
		if err != nil {
			return
		}
		defer os.RemoveAll(dir)
		dbPath := filepath.Join(dir, "template.db")
		_ = os.Setenv("VANTYX_SQLITE_PATH", dbPath)
		_ = os.Setenv(initialAdminPasswordEnv, "Admin123!")
		app := NewApp()
		if app != nil && app.UserStore != nil {
			_ = app.UserStore.SetForcePasswordChange("admin", false)
		}
		if app != nil && app.DB != nil {
			_, _ = app.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			_ = app.DB.Close()
		}
		_ = os.Unsetenv("VANTYX_SQLITE_PATH")
		b, err := os.ReadFile(dbPath)
		if err == nil && len(b) > 0 {
			httpapiTestDBTemplate = b
		}
	}()
	os.Exit(m.Run())
}
