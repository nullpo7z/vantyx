package httpapi

import (
	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// sshBridgeOptions builds the standard SSH bridge options for a target,
// including host-key fingerprint verification and optional per-target
// insecure skip (see access.Target.SSHHostKeyInsecureSkipVerify).
func sshBridgeOptions(target *access.Target, extra ...sshproxy.BridgeOption) []sshproxy.BridgeOption {
	opts := []sshproxy.BridgeOption{sshproxy.WithHostKeyFingerprint(target.SSHHostKeyFingerprint)}
	if target.SSHHostKeyInsecureSkipVerify {
		opts = append(opts, sshproxy.WithTargetInsecureSkipVerify())
	}
	return append(opts, extra...)
}
