package main

import (
	"os"
	"testing"
)

// TestMain enables plaintext-secret mode so that httpapi.NewApp can
// run without VANTYX_SSH_PASSWORD_ENCRYPTION_KEY. Production startup
// fails fast when the key is missing (C-8 / secret.LoadKeyFromEnvStrict),
// but the unit tests intentionally exercise the no-encryption path.
func TestMain(m *testing.M) {
	_ = os.Setenv("VANTYX_ALLOW_PLAINTEXT_SECRETS", "1")
	os.Exit(m.Run())
}
