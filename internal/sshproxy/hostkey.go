package sshproxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// BridgeOption configures optional behavior of the SSH bridge helpers
// (RunBridge / RunBridgeStream / RunBridgeDetachable) and of
// BuildClientConfig.
//
// The zero value rejects connections to servers whose key is unknown,
// which is the secure default required by OWASP ASVS V2.6.
type BridgeOption func(*bridgeOptions)

type bridgeOptions struct {
	hostKeyFingerprint       string
	insecureSkipVerify       bool
	targetInsecureSkipVerify bool
	// captured is populated by the host-key callback when a connection
	// is rejected because no fingerprint was configured; callers may
	// surface it via TOFU flows.
	captured *string
	// controlSink, when set, receives the live BridgeController so the
	// HTTP layer can drive writer / viewer hand-offs. The bridge calls
	// Register exactly once after the SSH session is set up; nil sinks
	// are ignored.
	controlSink BridgeControlSink
	// resizeRecorder, when set, is notified of every PTY resize so
	// recordings can capture asciicast "r" events (see ResizeRecorder).
	resizeRecorder ResizeRecorder
}

// WithHostKeyFingerprint sets the expected SHA-256 host-key fingerprint
// for the upstream SSH server. Accepted format is the OpenSSH default
// "SHA256:<base64-without-padding>" (matching `ssh-keygen -lf`).
//
// When fp is empty the option is a no-op so that legacy code paths can
// pass through their stored value unmodified.
func WithHostKeyFingerprint(fp string) BridgeOption {
	return func(o *bridgeOptions) {
		o.hostKeyFingerprint = strings.TrimSpace(fp)
	}
}

// WithInsecureSkipHostKeyVerify disables host-key verification for all
// targets when combined with VANTYX_SSH_INSECURE_IGNORE_HOST_KEY=1 or
// when used alone in tests. Prefer [WithTargetInsecureSkipVerify] for
// per-target break-glass scoped to one bastion destination.
func WithInsecureSkipHostKeyVerify() BridgeOption {
	return func(o *bridgeOptions) { o.insecureSkipVerify = true }
}

// WithTargetInsecureSkipVerify disables host-key verification for a
// single target connection. Equivalent to setting
// target.SSHHostKeyInsecureSkipVerify in the access layer (CWE-295).
func WithTargetInsecureSkipVerify() BridgeOption {
	return func(o *bridgeOptions) { o.targetInsecureSkipVerify = true }
}

// WithBridgeControlSink registers a sink that receives the live
// BridgeController when the bridge is ready. Used by internal/httpapi
// to wire up the collaborative-session writer / viewer hand-off.
func WithBridgeControlSink(sink BridgeControlSink) BridgeOption {
	return func(o *bridgeOptions) { o.controlSink = sink }
}

// WithResizeRecorder registers a recorder that is notified whenever the
// target PTY is resized, so a recording can replay at the terminal
// geometry the session actually ran at instead of the fixed size baked
// into the cast header at start time.
func WithResizeRecorder(rr ResizeRecorder) BridgeOption {
	return func(o *bridgeOptions) { o.resizeRecorder = rr }
}

// WithCapturedFingerprint configures the bridge / host-key callback to
// publish the SHA-256 fingerprint of the upstream key into *out. It is
// populated:
//
//   - on every connection when WithInsecureSkipHostKeyVerify is also
//     set (host key probing helper for TOFU adoption); and
//   - when host-key verification is rejected because no fingerprint is
//     configured, so the rejecting error reporter can still surface the
//     offered key to the operator.
//
// The pointer must be non-nil. Passing nil makes this option a no-op.
func WithCapturedFingerprint(out *string) BridgeOption {
	return func(o *bridgeOptions) {
		if out != nil {
			o.captured = out
		}
	}
}

func buildOptions(opts []BridgeOption) *bridgeOptions {
	o := &bridgeOptions{}
	for _, f := range opts {
		if f != nil {
			f(o)
		}
	}
	return o
}

// SHA256Fingerprint returns the OpenSSH-style "SHA256:<base64>" fingerprint
// of a public key. Padding "=" is stripped to match `ssh-keygen -lf`.
func SHA256Fingerprint(k ssh.PublicKey) string {
	sum := sha256.Sum256(k.Marshal())
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
}

// HostKeyMismatchError is returned by the host-key callback when the
// server presents a key that does not match the configured fingerprint.
// It implements the error interface and is wrapped as ErrHostKeyMismatch
// so callers can match on it with errors.Is.
type HostKeyMismatchError struct {
	Host     string
	Expected string
	Got      string
}

func (e *HostKeyMismatchError) Error() string {
	return "ssh: host key mismatch for " + e.Host +
		" (expected " + e.Expected + ", got " + e.Got + ")"
}

func (e *HostKeyMismatchError) Unwrap() error { return ErrHostKeyMismatch }

// HostKeyUnknownError is returned when verification is required but no
// expected fingerprint is configured for the target. The Got field is
// the SHA-256 fingerprint of the key the server actually offered, so
// the caller can surface it for a TOFU confirmation flow.
type HostKeyUnknownError struct {
	Host string
	Got  string
}

func (e *HostKeyUnknownError) Error() string {
	return "ssh: host key not configured for " + e.Host +
		" (offered " + e.Got + "); record this fingerprint on the target or set VANTYX_SSH_INSECURE_IGNORE_HOST_KEY=1"
}

func (e *HostKeyUnknownError) Unwrap() error { return ErrHostKeyUnknown }

// Sentinel errors so consumers (e.g. httpapi) can identify failure modes
// with errors.Is without depending on the concrete struct types.
var (
	ErrHostKeyMismatch = errors.New("ssh: host key mismatch")
	ErrHostKeyUnknown  = errors.New("ssh: host key not configured")
)

// HostKeyCallbackForOptions returns an ssh.HostKeyCallback configured
// from the supplied BridgeOption values. Exposed so other packages
// (e.g. internal/sftp) can build their own ssh.ClientConfig while
// reusing Vantyx's host-key verification rules.
func HostKeyCallbackForOptions(opts ...BridgeOption) ssh.HostKeyCallback {
	return hostKeyCallback(buildOptions(opts))
}

// hostKeyCallback returns an ssh.HostKeyCallback enforcing the options.
//
//   - If a fingerprint is configured: the callback verifies the server
//     key matches in constant time.
//   - If no fingerprint is configured but VANTYX_SSH_INSECURE_IGNORE_HOST_KEY=1
//     or WithInsecureSkipHostKeyVerify was passed: connections are
//     accepted (with a captured fingerprint exposed to the caller).
//   - Otherwise the connection is rejected with HostKeyUnknownError so
//     the operator can adopt the key explicitly (TOFU).
func hostKeyCallback(opts *bridgeOptions) ssh.HostKeyCallback {
	if opts == nil {
		opts = &bridgeOptions{}
	}
	if fp := opts.hostKeyFingerprint; fp != "" {
		return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
			got := SHA256Fingerprint(key)
			if subtle.ConstantTimeCompare([]byte(got), []byte(fp)) == 1 {
				return nil
			}
			return &HostKeyMismatchError{Host: hostname, Expected: fp, Got: got}
		}
	}
	insecure := opts.targetInsecureSkipVerify || opts.insecureSkipVerify || os.Getenv("VANTYX_SSH_INSECURE_IGNORE_HOST_KEY") == "1"
	if insecure {
		return func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if opts.captured != nil {
				*opts.captured = SHA256Fingerprint(key)
			}
			return nil
		}
	}
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		got := SHA256Fingerprint(key)
		if opts.captured != nil {
			*opts.captured = got
		}
		return &HostKeyUnknownError{Host: hostname, Got: got}
	}
}
