package httpapi

import (
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

func TestParseProtocolField_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected access.Protocol
	}{
		{"empty_defaults_to_ssh", "", access.ProtocolSSH},
		{"ssh", "ssh", access.ProtocolSSH},
		{"ssh_uppercase", "SSH", access.ProtocolSSH},
		{"telnet", "telnet", access.ProtocolTelnet},
		{"vnc", "vnc", access.ProtocolVNC},
		{"tftp", "tftp", access.ProtocolTFTP},
		{"rdp", "rdp", access.ProtocolRDP},
		{"ftp", "ftp", access.ProtocolFTP},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseProtocolField(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestParseProtocolField_Invalid(t *testing.T) {
	t.Parallel()

	invalids := []string{"ssh2", "http", "https", "mysql"}
	for _, in := range invalids {
		in := in
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := parseProtocolField(in)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", in)
			}
			if err.Error() != protocolValidationError {
				t.Fatalf("unexpected error message: %q", err.Error())
			}
		})
	}
}

