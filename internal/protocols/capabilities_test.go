package protocols

import "github.com/nullpo7z/vantyx/internal/access"

func bools(xs ...bool) []bool { return xs }

// These tests are intentionally simple but give us a single place to assert
// which high-level capabilities each protocol is expected to support.

func ExampleSupports() {
	_ = bools(
		Supports(access.ProtocolSSH, CapabilityTerminal),
		Supports(access.ProtocolSSH, CapabilityFileTransfer),
		Supports(access.ProtocolTFTP, CapabilityTFTPServer),
	)
	// Output:
}

func ExampleSupportsFileTransfer() {
	_ = bools(
		SupportsFileTransfer(access.ProtocolSSH),
		SupportsFileTransfer(access.ProtocolFTP),
		SupportsFileTransfer(access.ProtocolTFTP),
	)
	// Output:
}
