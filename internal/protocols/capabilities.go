package protocols

import "github.com/nullpo7z/vantyx/internal/access"

// Capability is a high-level feature that a protocol may or may not
// implement. The package keeps the mapping in one place so that adding
// a new bridge is a single edit.
type Capability string

const (
	CapabilityTerminal     Capability = "terminal"      // interactive session (SSH / Telnet / RDP / VNC ...).
	CapabilityFileTransfer Capability = "file_transfer" // file transfer (SFTP / FTP / TFTP).
	CapabilityTFTPServer   Capability = "tftp_server"   // exposes Vantyx's embedded TFTP server.
)

// Supports reports whether protocol p exposes the given capability.
func Supports(p access.Protocol, cap Capability) bool {
	switch cap {
	case CapabilityTerminal:
		// Protocols that drive an interactive session.
		switch p {
		case access.ProtocolSSH, access.ProtocolTelnet, access.ProtocolRDP, access.ProtocolVNC:
			return true
		default:
			return false
		}
	case CapabilityFileTransfer:
		// Protocols usable for file transfer.
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
