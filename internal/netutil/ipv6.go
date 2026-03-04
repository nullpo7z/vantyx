package netutil

import (
	"fmt"
	"net"
	"strings"
)

// ParseIPv6WithZone parses an IPv6 address that may contain a zone ID (e.g. "fe80::1%eth0")
// and returns the IP and an interface suitable for net.Dial / net.DialIP.
func ParseIPv6WithZone(s string) (net.IP, *net.Interface, error) {
	if s == "" {
		return nil, nil, fmt.Errorf("empty address")
	}

	var zone string
	host := s
	if i := strings.LastIndex(s, "%"); i != -1 {
		host = s[:i]
		zone = s[i+1:]
	}

	ip := net.ParseIP(host)
	if ip == nil || ip.To16() == nil || ip.To4() != nil {
		return nil, nil, fmt.Errorf("invalid IPv6 address: %q", host)
	}

	var iface *net.Interface
	if zone != "" {
		ifi, err := net.InterfaceByName(zone)
		if err != nil {
			return nil, nil, fmt.Errorf("lookup interface %q: %w", zone, err)
		}
		iface = ifi
	}

	return ip, iface, nil
}

