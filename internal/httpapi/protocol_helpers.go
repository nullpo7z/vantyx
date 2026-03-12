package httpapi

import (
	"errors"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
)

// protocolValidationError is a shared error message for invalid protocol strings from JSON.
const protocolValidationError = "protocol must be ssh, telnet, vnc, tftp, ftp, or rdp"

// parseProtocolField converts a protocol name from JSON (e.g. "ssh", "rdp") into access.Protocol.
// 空文字または "ssh" の場合は SSH をデフォルトとし、それ以外の未対応文字列は共通メッセージで 400 を返すためのエラーを返す。
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
		return "", errors.New(protocolValidationError)
	}
}
