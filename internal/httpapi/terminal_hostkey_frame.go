package httpapi

// Structured WebSocket error frames for SSH host-key TOFU.
//
// runDetachableBridge normally surfaces upstream failures as a single
// "error: <localized message>" text frame. For the two host-key error
// modes that the SPA can recover from interactively, we instead emit a
// JSON frame the SPA recognizes and renders as a TOFU adoption /
// mismatch warning dialog. Both frames carry enough context for the
// SPA to call the corresponding /api/targets/{target_id}/ssh-host-key
// endpoint without an extra round trip.
//
// Frame shapes:
//
//   {
//     "type": "host_key_unknown",
//     "target_id": "...",
//     "host":      "...",
//     "port":      22,
//     "fingerprint": "SHA256:..."
//   }
//
//   {
//     "type": "host_key_mismatch",
//     "target_id": "...",
//     "host":      "...",
//     "port":      22,
//     "expected": "SHA256:abc...",
//     "offered":  "SHA256:xyz..."
//   }
//
// All other bridge errors fall through to the legacy text frame.

import (
	"encoding/json"
	"errors"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// hostKeyUnknownFrame is the JSON shape sent on
// *sshproxy.HostKeyUnknownError so the SPA can render a TOFU adoption
// dialog.
type hostKeyUnknownFrame struct {
	Type        string `json:"type"`
	TargetID    string `json:"target_id"`
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Fingerprint string `json:"fingerprint"`
}

// hostKeyMismatchFrame is the JSON shape sent on
// *sshproxy.HostKeyMismatchError so the SPA can render a strong MITM
// warning + checkbox-confirmed re-adoption flow.
type hostKeyMismatchFrame struct {
	Type     string `json:"type"`
	TargetID string `json:"target_id"`
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Expected string `json:"expected"`
	Offered  string `json:"offered"`
}

// encodeHostKeyErrorFrame inspects bridgeErr and, if it carries one of
// the two host-key sentinels, returns the JSON frame, the audit event
// name to record, and ok=true. Other errors are passed through with
// ok=false so the caller falls back to the legacy "error: ..." text
// frame.
func encodeHostKeyErrorFrame(bridgeErr error, target *access.Target) (frame []byte, auditEvent string, ok bool) {
	if bridgeErr == nil || target == nil {
		return nil, "", false
	}

	var unknown *sshproxy.HostKeyUnknownError
	if errors.As(bridgeErr, &unknown) {
		buf, err := json.Marshal(hostKeyUnknownFrame{
			Type:        "host_key_unknown",
			TargetID:    string(target.ID),
			Host:        target.Host,
			Port:        target.Port,
			Fingerprint: unknown.Got,
		})
		if err != nil {
			return nil, "", false
		}
		return buf, "terminal_host_key_unknown_presented", true
	}

	var mismatch *sshproxy.HostKeyMismatchError
	if errors.As(bridgeErr, &mismatch) {
		buf, err := json.Marshal(hostKeyMismatchFrame{
			Type:     "host_key_mismatch",
			TargetID: string(target.ID),
			Host:     target.Host,
			Port:     target.Port,
			Expected: mismatch.Expected,
			Offered:  mismatch.Got,
		})
		if err != nil {
			return nil, "", false
		}
		return buf, "terminal_host_key_mismatch_presented", true
	}

	return nil, "", false
}
