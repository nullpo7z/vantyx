package httpapi

import (
	"errors"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
)

// ErrInvalidProtocol indicates the caller supplied a protocol string
// that is not one of ssh, telnet, vnc, tftp, ftp, or rdp. Handlers use
// [errors.Is] to map this to the localized `targets.protocolInvalid`
// HTTP response.
var ErrInvalidProtocol = errors.New("protocol must be ssh, telnet, vnc, tftp, ftp, or rdp")

// parseProtocolField converts a protocol name from JSON (e.g. "ssh", "rdp") into access.Protocol.
// Empty input and "ssh" both map to [access.ProtocolSSH]. Unsupported
// strings return [ErrInvalidProtocol] so callers can localize the
// response without echoing untrusted input.
func parseProtocolField(raw string) (access.Protocol, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "ssh":
		return access.ProtocolSSH, nil
	case "telnet":
		return access.ProtocolTelnet, nil
	case "vnc":
		return access.ProtocolVNC, nil
	case "tftp":
		return access.ProtocolTFTP, nil
	case "rdp":
		return access.ProtocolRDP, nil
	case "ftp":
		return access.ProtocolFTP, nil
	default:
		return "", ErrInvalidProtocol
	}
}
