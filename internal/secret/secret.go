// Package secret provides encryption for sensitive data at rest (OWASP ASVS L2).
// Uses AES-256-GCM; key must be 32 bytes and provided at installation (e.g. env).
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

const (
	// NonceSize is the GCM nonce size (12 bytes recommended).
	NonceSize = 12
	// KeySize is the AES-256 key size in bytes.
	KeySize = 32
	// CiphertextVersionPrefix is prepended to encrypted values (v1 = AES-256-GCM).
	CiphertextVersionPrefix = "v1:"
)

var (
	ErrInvalidKey   = errors.New("secret: encryption key must be 32 bytes")
	ErrDecrypt      = errors.New("secret: decryption failed")
	ErrInvalidInput = errors.New("secret: invalid ciphertext format")
)

// Encrypt encrypts plaintext with AES-256-GCM. Key must be KeySize bytes.
// Returns CiphertextVersionPrefix + base64(nonce || ciphertext).
// Caller should zero key when no longer needed (ASVS 7.13).
func Encrypt(key []byte, plaintext string) (string, error) {
	if len(key) != KeySize {
		return "", ErrInvalidKey
	}
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	plain := []byte(plaintext)
	ciphertext := gcm.Seal(nil, nonce, plain, nil)
	// Zero plaintext copy (ASVS 7.13)
	for i := range plain {
		plain[i] = 0
	}
	combined := make([]byte, 0, len(nonce)+len(ciphertext))
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)
	return CiphertextVersionPrefix + base64.RawStdEncoding.EncodeToString(combined), nil
}

// Decrypt decrypts a value produced by Encrypt. If ciphertext does not have
// the version prefix, it is returned as-is (legacy plaintext).
func Decrypt(key []byte, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if len(ciphertext) < len(CiphertextVersionPrefix) || ciphertext[:len(CiphertextVersionPrefix)] != CiphertextVersionPrefix {
		// Legacy plaintext
		return ciphertext, nil
	}
	if len(key) != KeySize {
		return "", ErrInvalidKey
	}
	raw, err := base64.RawStdEncoding.DecodeString(ciphertext[len(CiphertextVersionPrefix):])
	if err != nil {
		return "", ErrInvalidInput
	}
	if len(raw) < NonceSize {
		return "", ErrInvalidInput
	}
	nonce := raw[:NonceSize]
	enc := raw[NonceSize:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, enc, nil)
	if err != nil {
		return "", ErrDecrypt
	}
	s := string(plain)
	// Zero plaintext buffer (ASVS 7.13)
	for i := range plain {
		plain[i] = 0
	}
	return s, nil
}

// LoadKeyFromEnv reads a 32-byte key from the environment variable (base64 encoded).
// Returns nil if the variable is empty or invalid (caller may then refuse to store secrets).
// ASVS 7.14: secrets are replaceable and placed at installation.
func LoadKeyFromEnv(envVar string) []byte {
	b64 := os.Getenv(envVar)
	if b64 == "" {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(key) != KeySize {
		return nil
	}
	return key
}
