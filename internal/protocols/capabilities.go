package protocols

import "github.com/nullpo7z/vantyx/internal/access"

// Capability represents a high-level feature that a protocol may support.
// このパッケージは「どのプロトコルがどの機能をサポートするか」を 1 箇所にまとめて管理するためのものです。
type Capability string

const (
	CapabilityTerminal     Capability = "terminal"      // 対話型セッション（SSH/Telnet/RDP/VNC など）
	CapabilityFileTransfer Capability = "file_transfer" // ファイル転送（SFTP/FTP/TFTP サーバー経由 など）
	CapabilityTFTPServer   Capability = "tftp_server"   // Vantyx 内蔵 TFTP サーバー機能
)

// Supports reports whether protocol p supports the given high-level capability.
func Supports(p access.Protocol, cap Capability) bool {
	switch cap {
	case CapabilityTerminal:
		// 対話型セッションを持つプロトコル
		switch p {
		case access.ProtocolSSH, access.ProtocolTelnet, access.ProtocolRDP, access.ProtocolVNC:
			return true
		default:
			return false
		}
	case CapabilityFileTransfer:
		// ファイル転送に使えるプロトコル
		switch p {
		case access.ProtocolSSH, access.ProtocolFTP, access.ProtocolTFTP:
			return true
		default:
			return false
		}
	case CapabilityTFTPServer:
		return p == access.ProtocolTFTP
	default:
		return false
	}
}

// SupportsFileTransfer is a convenience wrapper for Supports(p, CapabilityFileTransfer).
func SupportsFileTransfer(p access.Protocol) bool {
	return Supports(p, CapabilityFileTransfer)
}
