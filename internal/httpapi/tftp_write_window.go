package httpapi

// Embedded TFTP server write-window control.
//
// The embedded TFTP server (internal/tftp) rejects every WRQ (write) by
// default because UDP source addresses are trivially spoofable (CWE-290);
// an operator must explicitly open a short window before a device is
// allowed to push a file. These routes are that control plane, scoped
// like the sibling /api/tftp/targets/{target_id}/files/* endpoints (any
// user with access to the target, not admin-only):
//
//   GET    /api/tftp/targets/{target_id}/write-window
//     Resp (open):   {"open":true,"target_id":"...","client_ip":"...","expires_at":"..."}
//     Resp (closed): {"open":false,"target_id":"..."}
//     Always 200 — a status read never depends on whether the embedded
//     server happens to be running; "not running" just means "not open".
//
//   GET    /api/tftp/targets/{target_id}/write-window/events  (SSE)
//     Streams the same shape as the GET above: one message immediately on
//     connect with the current status, then one more each time it changes
//     (opened, closed, or the underlying TTL reset by a re-open) so every
//     tab/operator watching the same target updates live, without polling.
//
//   POST   /api/tftp/targets/{target_id}/write-window
//     Body: {"ttl_seconds":300}  (optional; default 300, max 1800)
//     Resp: {"open":true,"target_id":"...","client_ip":"192.0.2.10","expires_at":"..."}
//     Replaces any existing window for the target (same target_id always
//     means the same authorized IP — see below — so this only ever
//     resets the TTL, it never silently authorizes a different device).
//
//   DELETE /api/tftp/targets/{target_id}/write-window
//     Closes any open window for the target immediately.
//
// The authorized client IP is NOT caller-supplied: internal/tftp.Server's
// authorize() already requires a request's source IP to equal the target's
// stored Host before it ever consults the write window (see
// tftp.Server.authorize and enforceWindow), for both the target_id-prefixed
// path and the by-IP fallback lookup. A window opened for any IP other
// than target.Host could therefore never match a real request — accepting
// client_ip as a request field invited a silent, hard-to-diagnose failure
// mode (typo → "window opened" 200 response, but every real push still
// rejected). Deriving it from target.Host removes that footgun entirely.

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/tftp"
)

// tftpWriteWindowDefaultTTL is used when ttl_seconds is omitted or <= 0.
const tftpWriteWindowDefaultTTL = 5 * time.Minute

// tftpWriteWindowMaxTTL caps how long a single window can stay open,
// regardless of the requested ttl_seconds.
const tftpWriteWindowMaxTTL = 30 * time.Minute

type openTFTPWriteWindowRequest struct {
	TTLSeconds int `json:"ttl_seconds"`
}

type tftpWriteWindowStatusResponse struct {
	Open      bool   `json:"open"`
	TargetID  string `json:"target_id"`
	ClientIP  string `json:"client_ip,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// tftpWriteWindowStatusFor builds the current status for target. It is
// shared by the GET handler, the SSE handler's initial snapshot, and the
// POST/DELETE handlers' change-event payload, so all three ways of
// observing this state always agree.
func tftpWriteWindowStatusFor(target *access.Target) tftpWriteWindowStatusResponse {
	resp := tftpWriteWindowStatusResponse{TargetID: string(target.ID)}
	// tftp.Current() == nil (embedded server not running) means no window
	// can possibly be open; resp.Open stays false rather than treating
	// this as an error, since a status read should always have a
	// definitive answer.
	if srv := tftp.Current(); srv != nil {
		if info, ok := srv.WriteWindowStatus(target.ID); ok {
			resp.Open = true
			resp.ClientIP = info.ClientIP.String()
			resp.ExpiresAt = info.ExpiresAt.UTC().Format(time.RFC3339)
		}
	}
	return resp
}

// publishTFTPWriteWindowStatus notifies every SSE subscriber currently
// watching target.ID of its current status. Best-effort: marshal/registry
// failures are silently ignored, matching the other SSE brokers in this
// package (a missed live update just means the next GET/poll catches up).
func (a *App) publishTFTPWriteWindowStatus(target *access.Target) {
	if a.TFTPWriteWindowEvents == nil {
		return
	}
	payload, err := json.Marshal(tftpWriteWindowStatusFor(target))
	if err != nil {
		return
	}
	a.TFTPWriteWindowEvents.Publish(string(target.ID), payload)
}

// handleTFTPGetWriteWindow reports whether a write window is currently
// open for the target. GET /api/tftp/targets/{target_id}/write-window
func (a *App) handleTFTPGetWriteWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tftpWriteWindowStatusFor(target))
}

// handleTFTPWriteWindowEvents streams write-window status changes for one
// TFTP target over SSE: an initial message with the current status, then
// one more each time it's opened, closed, or its TTL is reset by a
// re-open — from any operator or tab, not just the caller's own actions.
// GET /api/tftp/targets/{target_id}/write-window/events
func (a *App) handleTFTPWriteWindowEvents(w http.ResponseWriter, r *http.Request) {
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}
	if a.TFTPWriteWindowEvents == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe before sending the initial snapshot so a change published
	// between the snapshot and the subscribe call can't be missed.
	ch := a.TFTPWriteWindowEvents.Subscribe(string(target.ID))
	defer a.TFTPWriteWindowEvents.Unsubscribe(ch)

	initial, err := json.Marshal(tftpWriteWindowStatusFor(target))
	if err == nil {
		if _, err := w.Write([]byte("data: " + string(initial) + "\n\n")); err != nil {
			return
		}
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write([]byte("data: " + string(msg) + "\n\n")); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

// handleTFTPOpenWriteWindow opens a time-bounded TFTP write window so the
// target's configured Host IP may push a file into its directory via a
// real TFTP WRQ. POST /api/tftp/targets/{target_id}/write-window
func (a *App) handleTFTPOpenWriteWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}
	userID := a.currentUserID(r)

	// The authorized IP is the target's own stored Host, not caller input
	// (see the package doc comment above for why). tftp.Server.authorize()
	// requires this to already be a literal IP for any TFTP request
	// against the target to work at all, so a non-IP Host means the
	// target can never be reached over the real TFTP protocol regardless
	// of the window.
	clientIP := net.ParseIP(target.Host)
	if clientIP == nil {
		writeJSONErrorKey(w, r, "tftp.targetHostNotIP", http.StatusBadRequest)
		return
	}

	var req openTFTPWriteWindowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}

	ttl := tftpWriteWindowDefaultTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
		if ttl > tftpWriteWindowMaxTTL {
			ttl = tftpWriteWindowMaxTTL
		}
	}

	// Validate all input before checking server availability so a
	// malformed request or misconfigured target always gets a
	// deterministic 400, not a 503 that depends on whether the embedded
	// server happens to be running.
	srv := tftp.Current()
	if srv == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}

	srv.OpenWriteWindow(target.ID, clientIP, ttl)
	audit("tftp_write_window_opened", auditFields{
		"user_id":     userID,
		"target_id":   string(target.ID),
		"client_ip":   clientIP.String(),
		"ttl_seconds": int(ttl.Seconds()),
	})
	status := tftpWriteWindowStatusFor(target)
	a.publishTFTPWriteWindowStatus(target)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}

// handleTFTPCloseWriteWindow revokes any open write window for the target
// immediately. DELETE /api/tftp/targets/{target_id}/write-window
func (a *App) handleTFTPCloseWriteWindow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}
	userID := a.currentUserID(r)

	srv := tftp.Current()
	if srv == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}

	srv.CloseWriteWindow(target.ID)
	audit("tftp_write_window_closed", auditFields{
		"user_id":   userID,
		"target_id": string(target.ID),
	})
	a.publishTFTPWriteWindowStatus(target)
	w.WriteHeader(http.StatusNoContent)
}
