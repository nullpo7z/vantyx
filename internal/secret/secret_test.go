package secret

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	plain := "secret-password"
	enc, err := Encrypt(key, plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == "" || enc == plain {
		t.Fatalf("expected encrypted value different from plaintext")
	}
	if enc[:len(CiphertextVersionPrefix)] != CiphertextVersionPrefix {
		t.Fatalf("expected v1 prefix, got %q", enc[:min(3, len(enc))])
	}
	dec, err := Decrypt(key, enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("decrypted %q, want %q", dec, plain)
	}
}

func TestEncryptDecrypt_Empty(t *testing.T) {
	key := make([]byte, KeySize)
	enc, err := Encrypt(key, "")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	if enc != "" {
		t.Fatalf("expected empty encrypted value, got %q", enc)
	}
	dec, err := Decrypt(key, "")
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if dec != "" {
		t.Fatalf("expected empty decrypted, got %q", dec)
	}
}

func TestDecrypt_LegacyPlaintext(t *testing.T) {
	key := make([]byte, KeySize)
	dec, err := Decrypt(key, "legacy-plaintext")
	if err != nil {
		t.Fatalf("Decrypt legacy: %v", err)
	}
	if dec != "legacy-plaintext" {
		t.Fatalf("expected legacy as-is, got %q", dec)
	}
}

func TestEncrypt_InvalidKey(t *testing.T) {
	_, err := Encrypt([]byte("short"), "x")
	if err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestLoadKeyFromEnv(t *testing.T) {
	const envKey = "VANTYX_TEST_ENCRYPTION_KEY"
	defer os.Unsetenv(envKey)
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	os.Setenv(envKey, base64.StdEncoding.EncodeToString(key))
	loaded := LoadKeyFromEnv(envKey)
	if len(loaded) != KeySize {
		t.Fatalf("loaded key length %d, want %d", len(loaded), KeySize)
	}
	for i := range key {
		if loaded[i] != key[i] {
			t.Fatalf("loaded key byte %d = %d, want %d", i, loaded[i], key[i])
		}
	}
	os.Unsetenv(envKey)
	if LoadKeyFromEnv(envKey) != nil {
		t.Fatal("expected nil when env unset")
	}
}
