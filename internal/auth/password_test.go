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

func TestHashPassword_BcryptError(t *testing.T) {
	old := bcryptCost
	defer func() { bcryptCost = old }()
	bcryptCost = 32 // above bcrypt.MaxCost (31) to force GenerateFromPassword to return error
	_, err := HashPassword("any")
	if err == nil {
		t.Fatalf("expected error for invalid cost, got nil")
	}
}
