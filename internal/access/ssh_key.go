package access

import (
	"context"
	"errors"
	"strings"
)

// SSHKeyID identifies a stored SSH private key in the Keys library.
type SSHKeyID string

// SSHKey holds decrypted key material for internal use only.
type SSHKey struct {
	ID                      SSHKeyID
	Label                   string
	SSHPrivateKey           string
	SSHPrivateKeyPassphrase string
}

// SSHKeySummary is the safe list view (no secret material).
type SSHKeySummary struct {
	ID            SSHKeyID
	Label         string
	KeyType       string
	HasPassphrase bool
}

type SSHKeyStore interface {
	List(ctx context.Context) ([]SSHKeySummary, error)
	Create(ctx context.Context, id SSHKeyID, label, sshPrivateKey, sshPrivateKeyPassphrase string) (*SSHKeySummary, error)
	Update(ctx context.Context, id SSHKeyID, label string, sshPrivateKey, sshPrivateKeyPassphrase *string) (*SSHKeySummary, error)
	Delete(ctx context.Context, id SSHKeyID) error
	GetDecrypted(ctx context.Context, id SSHKeyID) (*SSHKey, error)
}

var (
	ErrSSHKeyExists     = errors.New("ssh key already exists")
	ErrSSHKeyNotFound   = errors.New("ssh key not found")
	ErrSSHKeyIDEmpty    = errors.New("ssh key id required")
	ErrSSHKeyLabelReq   = errors.New("ssh key label required")
	ErrSSHKeyPrivateReq = errors.New("ssh private key required")
	ErrSSHKeyInUse      = errors.New("ssh key is referenced by an identity")
)

func validateSSHKeyID(id SSHKeyID) error {
	s := strings.TrimSpace(string(id))
	if s == "" {
		return ErrSSHKeyIDEmpty
	}
	if len(s) > maxIDLen {
		return ErrTargetIDTooLong
	}
	if !idPattern.MatchString(s) {
		return ErrTargetIDInvalid
	}
	return nil
}

func validateSSHKeyLabel(label string) error {
	return validateCredentialLibraryLabel(label, ErrSSHKeyLabelReq)
}
