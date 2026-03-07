package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hashed, err := HashPassword("Secret1!")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hashed == "" {
		t.Fatalf("expected non-empty hash")
	}

	if !VerifyPassword(hashed, "Secret1!") {
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

func TestValidatePassword_Policy(t *testing.T) {
	tests := []struct {
		name string
		pw   string
		want error
	}{
		{"empty", "", ErrEmptyPassword},
		{"short", "Ab1!", ErrPasswordTooShort},
		{"no upper", "password1!", ErrPasswordNoUpper},
		{"no lower", "PASSWORD1!", ErrPasswordNoLower},
		{"no digit", "Password!", ErrPasswordNoDigit},
		{"no special", "Password1", ErrPasswordNoSpecial},
		{"ok", "Password1!", nil},
		{"ok long", "MyP@ssw0rd", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidatePassword(tt.pw)
			if got != tt.want {
				t.Fatalf("ValidatePassword(%q) = %v, want %v", tt.pw, got, tt.want)
			}
		})
	}
}

func TestHashPassword_BcryptError(t *testing.T) {
	old := bcryptCost
	defer func() { bcryptCost = old }()
	bcryptCost = 32 // above bcrypt.MaxCost (31) to force GenerateFromPassword to return error
	_, err := HashPassword("Any1!xxx")
	if err == nil {
		t.Fatalf("expected error for invalid cost, got nil")
	}
}
