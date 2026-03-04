package netutil

import (
	"net"
	"testing"
)

func TestParseIPv6WithZone_ValidWithZone(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces: %v", err)
	}
	if len(ifaces) == 0 {
		t.Skip("no network interfaces available")
	}

	name := ifaces[0].Name
	addr := "fe80::1%" + name

	ip, iface, err := ParseIPv6WithZone(addr)
	if err != nil {
		t.Fatalf("ParseIPv6WithZone returned error: %v", err)
	}
	if ip == nil {
		t.Fatalf("expected non-nil IP")
	}
	if iface == nil {
		t.Fatalf("expected non-nil interface")
	}
	if iface.Name != name {
		t.Fatalf("expected interface %q, got %q", name, iface.Name)
	}
}

func TestParseIPv6WithZone_ValidWithoutZone(t *testing.T) {
	ip, iface, err := ParseIPv6WithZone("2001:db8::1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip == nil {
		t.Fatalf("expected non-nil IP")
	}
	if iface != nil {
		t.Fatalf("expected nil interface for address without zone, got %v", iface)
	}
}

func TestParseIPv6WithZone_InvalidInputs(t *testing.T) {
	cases := []string{
		"",
		"not-an-ip",
		"192.0.2.1",
		"2001:db8::1%nonexistent0",
	}

	for _, c := range cases {
		_, _, err := ParseIPv6WithZone(c)
		if err == nil {
			t.Fatalf("expected error for input %q, got nil", c)
		}
	}
}
