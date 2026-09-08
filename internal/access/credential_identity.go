package access

import (
	"context"
	"errors"
	"strings"
)

// CredentialIdentityID identifies a stored login identity in the Identities library.
type CredentialIdentityID string

// CredentialIdentity holds decrypted credentials for internal use only.
type CredentialIdentity struct {
	ID          CredentialIdentityID
	Label       string
	SSHUsername string
	SSHPassword string
	SSHKeyID    SSHKeyID
	// Populated when SSHKeyID is set.
	Key *SSHKey
}

// CredentialIdentitySummary is the safe list view.
type CredentialIdentitySummary struct {
	ID            CredentialIdentityID
	Label         string
	SSHUsername   string
	HasPassword   bool
	SSHKeyID      SSHKeyID
	SSHKeyLabel   string
	HasSSHKey     bool
	HasPassphrase bool // from linked key when present
}

type CredentialIdentityStore interface {
	List(ctx context.Context) ([]CredentialIdentitySummary, error)
	Create(ctx context.Context, id CredentialIdentityID, label, sshUsername, sshPassword string, sshKeyID SSHKeyID) (*CredentialIdentitySummary, error)
	Update(ctx context.Context, id CredentialIdentityID, label, sshUsername string, sshPassword *string, sshKeyID *string) (*CredentialIdentitySummary, error)
	Delete(ctx context.Context, id CredentialIdentityID) error
	GetDecrypted(ctx context.Context, id CredentialIdentityID) (*CredentialIdentity, error)
}

var (
	ErrCredentialIdentityExists    = errors.New("credential identity already exists")
	ErrCredentialIdentityNotFound  = errors.New("credential identity not found")
	ErrCredentialIdentityIDEmpty   = errors.New("credential identity id required")
	ErrCredentialIdentityIDTooLong = errors.New("credential identity id too long")
	ErrCredentialIdentityIDInvalid = errors.New("credential identity id contains invalid characters")
	ErrCredentialIdentityLabelReq  = errors.New("credential identity label required")
	ErrCredentialIdentityUserReq   = errors.New("credential identity username required")
	ErrCredentialIdentityAuthReq   = errors.New("credential identity requires password and/or ssh key")
	ErrCredentialIdentityInUse     = errors.New("credential identity is referenced by a target")
)

func validateCredentialIdentityID(id CredentialIdentityID) error {
	return validateCredentialLibraryID(string(id), ErrCredentialIdentityIDEmpty, ErrCredentialIdentityIDTooLong, ErrCredentialIdentityIDInvalid)
}

func validateCredentialIdentityLabel(label string) error {
	return validateCredentialLibraryLabel(label, ErrCredentialIdentityLabelReq)
}

func validateCredentialIdentityAuth(sshPassword string, sshKeyID SSHKeyID) error {
	if strings.TrimSpace(sshPassword) == "" && strings.TrimSpace(string(sshKeyID)) == "" {
		return ErrCredentialIdentityAuthReq
	}
	return nil
}
