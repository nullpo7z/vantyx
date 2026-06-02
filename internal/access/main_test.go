package access

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Most unit tests create targets on loopback addresses.
	_ = os.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", "1")
	os.Exit(m.Run())
}
