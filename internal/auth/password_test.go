package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hashed, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hashed == "" {
		t.Fatalf("expected non-empty hash")
	}

	if !VerifyPassword(hashed, "secret123") {
		t.Fatalf("expected VerifyPassword to succeed for correct password")
	}

	if VerifyPassword(hashed, "wrong") {
		t.Fatalf("expected VerifyPassword to fail for incorrect password")
	}
}

func TestHashPasswordRejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatalf("expected error for empty password, got nil")
	}
}

func TestVerifyPasswordRejectsEmptyInputs(t *testing.T) {
	if VerifyPassword("", "pw") {
		t.Fatalf("expected false when hash is empty")
	}
	if VerifyPassword("hash", "") {
		t.Fatalf("expected false when password is empty")
	}
}

