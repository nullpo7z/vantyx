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
	// File transfer protocol toggles (for SSH/telnet: SFTP/FTP/TFTP の「ファイル転送で使用するプロトコル」の有効・無効). Stored in DB.
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
	// Tags: ターゲットに付与されたタグ。ユーザーが同じタグを持つとアクセス可能。
	TagsForTarget(ctx context.Context, targetID TargetID) ([]string, error)
	SetTargetTags(ctx context.Context, targetID TargetID, tags []string) error
}

var (
	ErrTargetExists          = errors.New("target already exists")
	ErrTargetNotFound        = errors.New("target not found")
	ErrEncryptionKeyRequired = errors.New("SSH password encryption key not configured (set VANTYX_SSH_PASSWORD_ENCRYPTION_KEY); required by ASVS L2 for sensitive data at rest")
)
