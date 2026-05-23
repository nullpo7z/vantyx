package sshproxy

import "testing"

func TestAuthMethods_PasswordOnly(t *testing.T) {
	m, err := AuthMethods("p", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 method, got %d", len(m))
	}
}

func TestAuthMethods_NoCredentials(t *testing.T) {
	m, err := AuthMethods("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("expected 0 methods, got %d", len(m))
	}
}
