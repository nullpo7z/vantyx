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
	if enc[:len(CiphertextVersionPrefixV2)] != CiphertextVersionPrefixV2 {
		t.Fatalf("expected v2 prefix, got %q", enc[:min(3, len(enc))])
	}
	dec, err := Decrypt(key, enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if dec != plain {
		t.Fatalf("decrypted %q, want %q", dec, plain)
	}
}

func TestEncryptDecrypt_AAD(t *testing.T) {
	key := make([]byte, KeySize)
	enc, err := EncryptWithAAD(key, "alpha", []byte("ctx-1"))
	if err != nil {
		t.Fatalf("EncryptWithAAD: %v", err)
	}
	if _, err := DecryptWithAAD(key, enc, []byte("ctx-2")); err == nil {
		t.Fatal("expected error for wrong AAD")
	}
	dec, err := DecryptWithAAD(key, enc, []byte("ctx-1"))
	if err != nil || dec != "alpha" {
		t.Fatalf("matched AAD decrypt: %q err=%v", dec, err)
	}
}

func TestKeyID_Mismatch(t *testing.T) {
	keyA := make([]byte, KeySize)
	keyA[0] = 1
	keyB := make([]byte, KeySize)
	keyB[0] = 2
	enc, err := Encrypt(keyA, "x")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := Decrypt(keyB, enc); err != ErrKeyMismatch {
		t.Fatalf("expected ErrKeyMismatch, got %v", err)
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

func TestDecrypt_RejectsPlaintext(t *testing.T) {
	key := make([]byte, KeySize)
	_, err := Decrypt(key, "not-encrypted")
	if err != ErrInvalidInput {
		t.Fatalf("Decrypt plaintext: got %v", err)
	}
}

func TestDecrypt_RejectsV1(t *testing.T) {
	key := make([]byte, KeySize)
	_, err := Decrypt(key, "v1:YWJj")
	if err != ErrInvalidInput {
		t.Fatalf("Decrypt v1: got %v", err)
	}
}

func TestIsEncrypted(t *testing.T) {
	key := make([]byte, KeySize)
	enc, _ := Encrypt(key, "x")
	if !IsEncrypted(enc) {
		t.Fatal("expected IsEncrypted true for v2")
	}
	if IsEncrypted("plain") || IsEncrypted("v1:foo") {
		t.Fatal("expected IsEncrypted false for non-v2")
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

func TestDecrypt_InvalidKey(t *testing.T) {
	key := make([]byte, KeySize)
	enc, _ := Encrypt(key, "x")
	_, err := Decrypt([]byte("short"), enc)
	if err != ErrInvalidKey {
		t.Fatalf("Decrypt wrong key length: got %v", err)
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key := make([]byte, KeySize)
	_, err := Decrypt(key, CiphertextVersionPrefixV2+"!!!")
	if err != ErrInvalidInput {
		t.Fatalf("Decrypt bad base64: got %v", err)
	}
}

func TestDecrypt_TooShortPayload(t *testing.T) {
	key := make([]byte, KeySize)
	short := base64.RawStdEncoding.EncodeToString([]byte("short"))
	_, err := Decrypt(key, CiphertextVersionPrefixV2+short)
	if err != ErrInvalidInput {
		t.Fatalf("Decrypt too short: got %v", err)
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	key := make([]byte, KeySize)
	enc, _ := Encrypt(key, "secret")
	body := enc[len(CiphertextVersionPrefixV2):]
	raw, _ := base64.RawStdEncoding.DecodeString(body)
	tamperAt := keyIDSize + NonceSize
	if len(raw) > tamperAt {
		raw[tamperAt] ^= 0xff
	}
	tampered := CiphertextVersionPrefixV2 + base64.RawStdEncoding.EncodeToString(raw)
	_, err := Decrypt(key, tampered)
	if err != ErrDecrypt {
		t.Fatalf("Decrypt tampered: got %v", err)
	}
}

func TestLoadKeyFromEnv_InvalidBase64(t *testing.T) {
	const envKey = "VANTYX_TEST_ENCRYPTION_KEY"
	defer os.Unsetenv(envKey)
	os.Setenv(envKey, "!!!")
	if LoadKeyFromEnv(envKey) != nil {
		t.Fatal("expected nil for invalid base64")
	}
}

func TestLoadKeyFromEnv_WrongKeyLength(t *testing.T) {
	const envKey = "VANTYX_TEST_ENCRYPTION_KEY"
	defer os.Unsetenv(envKey)
	os.Setenv(envKey, base64.StdEncoding.EncodeToString([]byte("16 bytes only!!")))
	if LoadKeyFromEnv(envKey) != nil {
		t.Fatal("expected nil for wrong key length")
	}
}
