package access

import (
	"errors"
	"net"
	"testing"
)

func TestCheckRestrictedHostIP_DefaultBlocks(t *testing.T) {
	t.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", "")
	cases := []string{
		"127.0.0.1",
		"::1",
		"169.254.169.254",
		"169.254.1.1",
	}
	for _, c := range cases {
		if err := checkRestrictedHostIP(net.ParseIP(c)); err == nil {
			t.Fatalf("expected restriction for %s", c)
		} else if !errors.Is(err, ErrHostRestricted) {
			t.Fatalf("expected ErrHostRestricted for %s, got %v", c, err)
		}
	}
}

func TestCheckRestrictedHostIP_AllowOverride(t *testing.T) {
	t.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", "1")
	if err := checkRestrictedHostIP(net.ParseIP("127.0.0.1")); err != nil {
		t.Fatalf("expected override to allow loopback: %v", err)
	}
}

func TestValidateHost_PublicIPAllowed(t *testing.T) {
	t.Setenv("VANTYX_ALLOW_RESTRICTED_HOSTS", "")
	if err := validateHost("203.0.113.10"); err != nil {
		t.Fatalf("public IP should be allowed: %v", err)
	}
}
