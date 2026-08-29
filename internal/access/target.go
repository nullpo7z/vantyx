package access

import (
	"context"
	"errors"
)

// Protocol is the connection protocol for a target.
type Protocol string

const (
	ProtocolSSH    Protocol = "ssh"
	ProtocolTelnet Protocol = "telnet"
	ProtocolVNC    Protocol = "vnc"
	ProtocolFTP    Protocol = "ftp"
	ProtocolTFTP   Protocol = "tftp"
	ProtocolRDP    Protocol = "rdp"
)

// Target represents a device that users can connect to via SSH or Telnet.
type Target struct {
	ID       TargetID
	Name     string
	Host     string
	Port     uint16
	Protocol Protocol
	// Path is an optional hierarchical folder path (e.g. "prod/network").
	// Empty means root.
	Path string
	// SSH credentials (optional). Stored when registering the server.
	SSHUsername string
	SSHPassword string
	// SSH public key auth: PEM-encoded private key and optional passphrase. Encrypted at rest like SSHPassword.
	SSHPrivateKey           string
	SSHPrivateKeyPassphrase string
	// SSHHostKeyFingerprint is the expected SHA-256 fingerprint of the
	// upstream SSH server's host key (format: "SHA256:<base64>"). When
	// non-empty, sshproxy / sftp connections to this target are aborted
	// unless the server presents a matching key. ASVS V2.6 / V9.2.
	SSHHostKeyFingerprint string
	// SSHHostKeyInsecureSkipVerify, when true, disables host-key
	// verification for this target only. Equivalent to setting the
	// global VANTYX_SSH_INSECURE_IGNORE_HOST_KEY=1 env var but scoped
	// to one target so the rest of the deployment retains its
	// default-deny posture (CWE-295). The fingerprint field, if also
	// set, is *ignored* in this mode.
	SSHHostKeyInsecureSkipVerify bool
	// File transfer protocol toggles (for SSH / Telnet targets: enable / disable SFTP / FTP / TFTP for the file transfer UI). Persisted to the DB.
	SFTPEnabled bool
	FTPEnabled  bool
	TFTPEnabled bool
	// CredentialIdentityID / SSHKeyID record which entry in the
	// Identities / SSH Keys library (if any) this target's credentials
	// were last set from. The secrets themselves are always copied onto
	// the target's own fields (above) at create/update time; these IDs
	// are kept only so the edit UI can show which source is in effect
	// instead of always falling back to "manual entry". Empty when the
	// credentials were entered manually.
	CredentialIdentityID CredentialIdentityID
	SSHKeyID             SSHKeyID
}

// TargetStore defines the behavior required for managing targets.
type TargetStore interface {
	Create(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol) (*Target, error)
	CreateWithPath(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol, groupID GroupID, path string, sshUsername, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string, sftpEnabled, ftpEnabled, tftpEnabled bool) (*Target, error)
	Get(ctx context.Context, id TargetID) (*Target, error)
	Update(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol, path, sshUsername, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string, sftpEnabled, ftpEnabled, tftpEnabled bool) (*Target, error)
	Delete(ctx context.Context, id TargetID) error
	ListByIDs(ctx context.Context, ids []TargetID, opts *ListOpts) ([]*Target, error)
	// ListByProtocol returns targets for the given protocol (e.g. TFTP). Credentials are not populated.
	ListByProtocol(ctx context.Context, protocol Protocol) ([]*Target, error)
	// Tags applied to the target. Users that share any of these tags gain access.
	TagsForTarget(ctx context.Context, targetID TargetID) ([]string, error)
	SetTargetTags(ctx context.Context, targetID TargetID, tags []string) error
	// SetSSHHostKeyFingerprint records the expected SHA-256 fingerprint
	// of the upstream SSH server's host key. Pass "" to clear it (which
	// reinstates the TOFU-prompt behavior on the next connection).
	SetSSHHostKeyFingerprint(ctx context.Context, targetID TargetID, fingerprint string) error
	// SetSSHHostKeyInsecureSkipVerify toggles per-target host-key
	// verification bypass. When true, sshproxy / sftp accept any
	// host key for this target. Use only when the operator
	// explicitly accepts the MITM risk.
	SetSSHHostKeyInsecureSkipVerify(ctx context.Context, targetID TargetID, skip bool) error
	// SetCredentialSource records which Identity / SSH Key library entry
	// (if any) the target's credentials were last set from, for the
	// edit UI's benefit. Pass "" for whichever of the two is not in
	// use; both empty clears the link (manual entry).
	SetCredentialSource(ctx context.Context, targetID TargetID, credentialIdentityID CredentialIdentityID, sshKeyID SSHKeyID) error
}

var (
	ErrTargetExists              = errors.New("target already exists")
	ErrTargetNotFound            = errors.New("target not found")
	ErrEncryptionKeyRequired     = errors.New("SSH password encryption key not configured (set VANTYX_SSH_PASSWORD_ENCRYPTION_KEY); required by ASVS L2 for sensitive data at rest")
	ErrHostKeyFingerprintInvalid = errors.New("SSH host key fingerprint must be empty or of the form 'SHA256:<base64>'")
)
