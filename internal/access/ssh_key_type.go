package access

import (
	"strings"

	"golang.org/x/crypto/ssh"
)

// DetectSSHKeyType returns a display label (RSA, ED25519, ECDSA, …) for PEM private key material.
func DetectSSHKeyType(privateKeyPEM, passphrase string) string {
	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	if privateKeyPEM == "" {
		return ""
	}
	var (
		signer ssh.Signer
		err    error
	)
	if strings.TrimSpace(passphrase) != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(privateKeyPEM), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(privateKeyPEM))
	}
	if err != nil {
		return "UNKNOWN"
	}
	return sshPublicKeyTypeLabel(signer.PublicKey())
}

func sshPublicKeyTypeLabel(pub ssh.PublicKey) string {
	switch pub.Type() {
	case ssh.KeyAlgoRSA, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512:
		return "RSA"
	case ssh.KeyAlgoED25519:
		return "ED25519"
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return "ECDSA"
	default:
		t := strings.TrimSpace(pub.Type())
		if t == "" {
			return "UNKNOWN"
		}
		return strings.ToUpper(t)
	}
}
