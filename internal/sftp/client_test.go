package sftp

import (
	"context"
	"testing"
)

func TestNewClient_ConnectionRefused(t *testing.T) {
	ctx := context.Background()
	// Port 1 typically has nothing listening; connection will fail quickly.
	_, err := NewClient(ctx, "127.0.0.1", 1, "user", "pass", "", "")
	if err == nil {
		t.Fatal("expected error when connecting to closed port")
	}
}

func TestPortString(t *testing.T) {
	if s := portString(0); s != "22" {
		t.Errorf("portString(0) = %q, want 22", s)
	}
	if s := portString(22); s != "22" {
		t.Errorf("portString(22) = %q, want 22", s)
	}
	if s := portString(2222); s != "2222" {
		t.Errorf("portString(2222) = %q, want 2222", s)
	}
}
