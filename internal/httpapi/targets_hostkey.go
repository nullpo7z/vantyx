package httpapi

// SSH host-key TOFU helper endpoints.
//
// Two admin-only routes plug into the existing /api/targets surface:
//
//   POST /api/targets/probe-host-key
//     Body: {"host":"example.com","port":22}
//     Resp: {"fingerprint":"SHA256:<base64>","host":"example.com","port":22}
//
//     The handler dials the host with host-key verification disabled
//     (sshproxy.WithInsecureSkipHostKeyVerify) but routes the captured
//     fingerprint into a local string via WithCapturedFingerprint, so
//     the SHA-256 fingerprint of the upstream key can be returned to
//     the SPA without ever persisting it. The Add Target / Edit Target
//     modals use this for the "trust on first use" confirmation step.
//
//   PUT /api/targets/{target_id}/ssh-host-key
//     Body: {"fingerprint":""}                   (clear)
//     Body: {"fingerprint":"SHA256:<base64>"}    (adopt / replace)
//
//     Validates the fingerprint format (validation lives inside
//     access.SetSSHHostKeyFingerprint) and emits a target_host_key_*
//     audit event. The full target row including the host key is
//     returned so the SPA can refresh its in-memory list.
//
// Both routes require admin role and emit dedicated audit events:
//   target_host_key_probed       (POST /probe-host-key success)
//   target_host_key_probe_failed (POST /probe-host-key dial error)
//   target_host_key_adopted      (PUT  /ssh-host-key with fingerprint)
//   target_host_key_cleared      (PUT  /ssh-host-key with empty value)

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// probeHostKeyRequest is the JSON body for POST
// /api/targets/probe-host-key. host is required; port defaults to 22
// when omitted. The handler enforces a short total timeout so a stuck
// dial cannot tie up the request goroutine.
type probeHostKeyRequest struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

// probeHostKeyResponse is the JSON returned on success. fingerprint is
// the SHA-256 fingerprint of the offered host key (matching
// `ssh-keygen -lf`); host and port echo back the inputs so the SPA can
// pin them on the target form even if the user edited fields between
// the probe call and the create call.
type probeHostKeyResponse struct {
	Host        string `json:"host"`
	Port        uint16 `json:"port"`
	Fingerprint string `json:"fingerprint"`
}

// updateHostKeyRequest is the JSON body for PUT
// /api/targets/{target_id}/ssh-host-key. An empty Fingerprint clears
// the column; a non-empty value must be in "SHA256:<base64>" form.
type updateHostKeyRequest struct {
	Fingerprint string `json:"fingerprint"`
}

// probeHostKeyTimeout caps the total time spent dialing the upstream
// during a host-key probe. SSH banner exchange usually completes well
// under one second, so 8s leaves slack for slow VPN paths but does not
// hold the request goroutine open if the host black-holes the SYN.
const probeHostKeyTimeout = 8 * time.Second

// handleProbeHostKey opens a one-shot SSH connection to (host, port),
// captures the offered host key's SHA-256 fingerprint via
// sshproxy.WithCapturedFingerprint, and returns it. Authentication is
// not attempted; the connection is torn down as soon as the host-key
// callback runs.
func (a *App) handleProbeHostKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.requireAdmin(w, r) {
		return
	}
	userID := a.currentUserID(r)

	var req probeHostKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Host = strings.TrimSpace(req.Host)
	if req.Host == "" {
		writeJSONErrorKey(w, r, "validation.hostEmpty", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}

	captured := ""
	opts := []sshproxy.BridgeOption{
		sshproxy.WithInsecureSkipHostKeyVerify(),
		sshproxy.WithCapturedFingerprint(&captured),
	}
	cb := sshproxy.HostKeyCallbackForOptions(opts...)

	ctx, cancel := context.WithTimeout(r.Context(), probeHostKeyTimeout)
	defer cancel()

	addr := net.JoinHostPort(req.Host, strconv.Itoa(int(req.Port)))
	// IMPORTANT: do NOT set HostKeyAlgorithms here. The detachable
	// SSH bridge in internal/sshproxy/bridge.go does not set it
	// either, so it uses x/crypto/ssh's default algorithm preference
	// order. When a server advertises multiple host keys (typically
	// RSA + ED25519 on stock OpenSSH), the SSH KEXINIT picks the
	// first algorithm both sides support — and the default order
	// places RSA *before* ED25519. If the probe forces a different
	// preference, it captures a key the actual bridge will never
	// see, leaving the operator stuck in an adoption / re-mismatch
	// loop. Letting Go's defaults run keeps probe and bridge
	// always-agreeing.
	cfg := &ssh.ClientConfig{
		User:            "vantyx-host-key-probe",
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: cb,
		Timeout:         probeHostKeyTimeout,
	}

	conn, dialErr := dialSSHWithContext(ctx, "tcp", addr, cfg)
	if conn != nil {
		_ = conn.Close()
	}
	if captured == "" {
		// The host-key callback never fired: the dial failed before
		// the SSH handshake offered a key. Surface a localized
		// dial/timeout error and audit the failure.
		audit("target_host_key_probe_failed", auditFields{
			"user_id": userID,
			"host":    req.Host,
			"port":    req.Port,
			"error":   safeErrString(dialErr),
		})
		writeProxyError(w, r, proxyerrors.WrapTCPDialError("ssh", dialErrOrTimeout(ctx, dialErr)), http.StatusBadGateway)
		return
	}

	audit("target_host_key_probed", auditFields{
		"user_id":     userID,
		"host":        req.Host,
		"port":        req.Port,
		"fingerprint": captured,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(probeHostKeyResponse{
		Host:        req.Host,
		Port:        req.Port,
		Fingerprint: captured,
	})
}

// handleUpdateTargetHostKey records (or clears) the expected SSH host
// key fingerprint of an existing target. Empty fingerprint clears the
// column; otherwise the value is validated by
// access.SetSSHHostKeyFingerprint (which canonicalizes "SHA256:..."
// and rejects non-base64 bodies).
func (a *App) handleUpdateTargetHostKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.requireAdmin(w, r) {
		return
	}
	targetID := chi.URLParam(r, "target_id")
	userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}

	var req updateHostKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	fp := strings.TrimSpace(req.Fingerprint)

	ctx := r.Context()
	if err := a.TargetStore.SetSSHHostKeyFingerprint(ctx, target.ID, fp); err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONErrorKey(w, r, "common.targetNotFound", http.StatusNotFound)
			return
		}
		if writeAccessValidationError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	if fp == "" {
		audit("target_host_key_cleared", auditFields{
			"user_id":   userID,
			"target_id": string(target.ID),
		})
	} else {
		audit("target_host_key_adopted", auditFields{
			"user_id":     userID,
			"target_id":   string(target.ID),
			"fingerprint": fp,
			"source":      "update",
		})
	}

	t, _ := a.TargetStore.Get(ctx, target.ID)
	tags, _ := a.TargetStore.TagsForTarget(ctx, target.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(targetToResponse(t, tags))
}

// dialSSHWithContext bridges net.Dialer (which honours ctx cancellation)
// with ssh.NewClientConn so the probe dial picks up the request
// timeout instead of hanging on a black-holed host. ssh.Dial uses a
// blocking net.Dial under the hood and ignores any ctx.
func dialSSHWithContext(ctx context.Context, network, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	d := net.Dialer{Timeout: cfg.Timeout}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// dialErrOrTimeout returns ctx.Err() if the context already fired
// (so the user-facing error reports "timeout" instead of "connection
// reset by peer"), otherwise the original dial error.
func dialErrOrTimeout(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

// safeErrString returns the error string or empty if err is nil. Used
// to keep audit fields free of <nil> placeholders.
func safeErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
