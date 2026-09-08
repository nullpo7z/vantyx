package access

import (
	"net"
	"os"
	"strings"
)

func restrictedHostsAllowed() bool {
	return strings.TrimSpace(os.Getenv("VANTYX_ALLOW_RESTRICTED_HOSTS")) == "1"
}

// checkRestrictedHostIP rejects addresses that commonly enable SSRF to
// local services when an admin account is compromised or misconfigured.
// Bastion operators that intentionally proxy to localhost may set
// VANTYX_ALLOW_RESTRICTED_HOSTS=1.
func checkRestrictedHostIP(ip net.IP) error {
	if restrictedHostsAllowed() || ip == nil {
		return nil
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return ErrHostRestricted
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return ErrHostRestricted
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 169 && ip4[1] == 254 {
		return ErrHostRestricted
	}
	return nil
}

// CheckRestrictedHostIP is the exported form of checkRestrictedHostIP for
// other packages that dial operator-supplied addresses (webhooks).
func CheckRestrictedHostIP(ip net.IP) error { return checkRestrictedHostIP(ip) }
