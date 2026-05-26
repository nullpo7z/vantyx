package sshproxy

import (
	"os"
	"testing"
)

// TestMain enables the legacy "skip host-key verification" behavior for
// the sshproxy unit tests, which dial fake SSH servers whose host key
// is generated on the fly. Production code paths receive an explicit
// SHA-256 fingerprint via WithHostKeyFingerprint; only tests rely on
// the insecure default.
func TestMain(m *testing.M) {
	_ = os.Setenv("VANTYX_SSH_INSECURE_IGNORE_HOST_KEY", "1")
	os.Exit(m.Run())
}
