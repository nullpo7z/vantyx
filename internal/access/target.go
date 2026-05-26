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
	// File transfer protocol toggles (for SSH / Telnet targets: enable / disable SFTP / FTP / TFTP for the file transfer UI). Persisted to the DB.
	SFTPEnabled bool
	FTPEnabled  bool
	TFTPEnabled bool
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
}

var (
	ErrTargetExists              = errors.New("target already exists")
	ErrTargetNotFound            = errors.New("target not found")
	ErrEncryptionKeyRequired     = errors.New("SSH password encryption key not configured (set VANTYX_SSH_PASSWORD_ENCRYPTION_KEY); required by ASVS L2 for sensitive data at rest")
	ErrHostKeyFingerprintInvalid = errors.New("SSH host key fingerprint must be empty or of the form 'SHA256:<base64>'")
)
