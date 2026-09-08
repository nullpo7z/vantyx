package sshd

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// loadOrGenerateHostKey reads an RSA host key from path or generates one.
// When path (or VANTYX_SSH_HOST_KEY_PATH) is set the key is persisted;
// otherwise a fresh 2048-bit RSA key is generated for this process only
// (suitable for tests and ephemeral dev runs).
func loadOrGenerateHostKey(path string) (ssh.Signer, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("VANTYX_SSH_HOST_KEY_PATH"))
	}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			block, _ := pem.Decode(data)
			if block == nil {
				return nil, fmt.Errorf("sshd: decode host key %s", path)
			}
			key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("sshd: parse host key %s: %w", path, err)
			}
			return ssh.NewSignerFromKey(key)
		}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("sshd: generate host key: %w", err)
		}
		if err := persistHostKey(path, key); err != nil {
			return nil, err
		}
		return ssh.NewSignerFromKey(key)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("sshd: generate host key: %w", err)
	}
	return ssh.NewSignerFromKey(key)
}

func persistHostKey(path string, key *rsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("sshd: mkdir host key dir: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return fmt.Errorf("sshd: write host key %s: %w", path, err)
	}
	return nil
}
