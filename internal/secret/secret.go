// Package secret provides encryption for sensitive data at rest (OWASP ASVS L2).
// Uses AES-256-GCM; key must be 32 bytes and provided at installation (e.g. env).
//
// Ciphertext layout
//
//	v1:<base64(nonce || ciphertext)>                            -- legacy, no AAD
//	v2:<base64(keyID(8 bytes) || nonce || ciphertext)>          -- AAD-bound, key
//	                                                               fingerprint in header
//
// Reads support both formats. Writes always emit v2 so an attacker
// who can flip a ciphertext between rows (CWE-345) can no longer have
// the same key decrypt it without detection: the AAD passed by the
// caller is part of the GCM tag.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
)

const (
	// NonceSize is the GCM nonce size (12 bytes recommended).
	NonceSize = 12
	// KeySize is the AES-256 key size in bytes.
	KeySize = 32
	// CiphertextVersionPrefix is the legacy (v1) prefix kept for
	// backwards compatibility on reads.
	CiphertextVersionPrefix = "v1:"
	// CiphertextVersionPrefixV2 marks the AAD-bound format.
	CiphertextVersionPrefixV2 = "v2:"
	// keyIDSize is the truncated SHA-256 prefix used as a stable
	// fingerprint of the encryption key, embedded in v2 ciphertexts
	// so we can detect key mismatches across rotations.
	keyIDSize = 8
)

var (
	ErrInvalidKey   = errors.New("secret: encryption key must be 32 bytes")
	ErrDecrypt      = errors.New("secret: decryption failed")
	ErrInvalidInput = errors.New("secret: invalid ciphertext format")
	// ErrKeyMismatch indicates the v2 ciphertext was encrypted with a
	// different key (its keyID prefix does not match the supplied key).
	ErrKeyMismatch = errors.New("secret: ciphertext was encrypted with a different key")
)

// KeyID returns a stable 8-byte fingerprint of key (truncated SHA-256).
// It does not leak the key but lets callers route to the right key when
// multiple keys are configured (rotation).
func KeyID(key []byte) []byte {
	sum := sha256.Sum256(key)
	out := make([]byte, keyIDSize)
	copy(out, sum[:keyIDSize])
	return out
}

// Encrypt encrypts plaintext with AES-256-GCM and no associated data.
// Equivalent to EncryptWithAAD(key, plaintext, nil). Kept so existing
// callers compile unchanged; new callers SHOULD use [EncryptWithAAD]
// so the ciphertext is bound to the column / row it lives in (which
// stops an attacker who can modify the DB from swapping ciphertexts
// between rows — CWE-345).
func Encrypt(key []byte, plaintext string) (string, error) {
	return EncryptWithAAD(key, plaintext, nil)
}

// EncryptWithAAD encrypts plaintext with AES-256-GCM, binding the
// ciphertext to aad (e.g. "<target_id>:ssh_password"). The same aad
// must be supplied to DecryptWithAAD or decryption fails.
//
// The output always uses the v2 format (keyID || nonce || ciphertext).
func EncryptWithAAD(key []byte, plaintext string, aad []byte) (string, error) {
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
	ciphertext := gcm.Seal(nil, nonce, plain, aad)
	for i := range plain {
		plain[i] = 0
	}
	kid := KeyID(key)
	combined := make([]byte, 0, keyIDSize+len(nonce)+len(ciphertext))
	combined = append(combined, kid...)
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)
	return CiphertextVersionPrefixV2 + base64.RawStdEncoding.EncodeToString(combined), nil
}

// Decrypt decrypts a value produced by Encrypt with empty AAD. If
// ciphertext does not have the version prefix, it is returned as-is
// (legacy plaintext).
func Decrypt(key []byte, ciphertext string) (string, error) {
	return DecryptWithAAD(key, ciphertext, nil)
}

// DecryptWithAAD decrypts a value produced by EncryptWithAAD with the
// same aad. It also accepts legacy v1 ciphertexts (in which case aad
// is ignored — those were not AAD-bound).
func DecryptWithAAD(key []byte, ciphertext string, aad []byte) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	switch {
	case strings.HasPrefix(ciphertext, CiphertextVersionPrefixV2):
		return decryptV2(key, ciphertext[len(CiphertextVersionPrefixV2):], aad)
	case strings.HasPrefix(ciphertext, CiphertextVersionPrefix):
		return decryptV1(key, ciphertext[len(CiphertextVersionPrefix):])
	default:
		// Legacy plaintext (no prefix).
		return ciphertext, nil
	}
}

func decryptV1(key []byte, body string) (string, error) {
	if len(key) != KeySize {
		return "", ErrInvalidKey
	}
	raw, err := base64.RawStdEncoding.DecodeString(body)
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
	for i := range plain {
		plain[i] = 0
	}
	return s, nil
}

func decryptV2(key []byte, body string, aad []byte) (string, error) {
	if len(key) != KeySize {
		return "", ErrInvalidKey
	}
	raw, err := base64.RawStdEncoding.DecodeString(body)
	if err != nil {
		return "", ErrInvalidInput
	}
	if len(raw) < keyIDSize+NonceSize {
		return "", ErrInvalidInput
	}
	gotKID := raw[:keyIDSize]
	if subtle.ConstantTimeCompare(gotKID, KeyID(key)) != 1 {
		return "", ErrKeyMismatch
	}
	nonce := raw[keyIDSize : keyIDSize+NonceSize]
	enc := raw[keyIDSize+NonceSize:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, enc, aad)
	if err != nil {
		return "", ErrDecrypt
	}
	s := string(plain)
	for i := range plain {
		plain[i] = 0
	}
	return s, nil
}

// LoadKeyFromEnv reads a 32-byte key from the environment variable (base64 encoded).
// Returns nil if the variable is empty or invalid (caller may then refuse to store secrets).
// ASVS 7.14: secrets are replaceable and placed at installation.
//
// Callers that handle sensitive data at rest must use [LoadKeyFromEnvStrict]
// instead so a misconfigured deployment fails to start rather than
// silently downgrading to "plaintext mode".
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

// LoadKeyFromEnvStrict returns the 32-byte key referenced by envVar
// or an error explaining why it could not be loaded.
//
// Use this in production startup code so the server refuses to run
// without a usable encryption key. Set VANTYX_ALLOW_PLAINTEXT_SECRETS=1
// to opt in to the legacy "no encryption" behaviour (development /
// migration only; never in production, ASVS V6.4 / CWE-326).
func LoadKeyFromEnvStrict(envVar string) ([]byte, error) {
	b64 := os.Getenv(envVar)
	if b64 == "" {
		if os.Getenv("VANTYX_ALLOW_PLAINTEXT_SECRETS") == "1" {
			return nil, nil
		}
		return nil, errors.New("secret: " + envVar + " is required (32 random bytes base64-encoded); set VANTYX_ALLOW_PLAINTEXT_SECRETS=1 only for non-production use")
	}
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("secret: " + envVar + " is not valid base64")
	}
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	return key, nil
}
