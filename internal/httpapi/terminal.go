package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
	"github.com/nullpo7z/vantyx/internal/recording"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/telnetproxy"
)

// supportsDetachableTerminal reports whether p has a long-lived
// detachable bridge implementation (SSH or Telnet today).
func supportsDetachableTerminal(p access.Protocol) bool {
	return p == access.ProtocolSSH || p == access.ProtocolTelnet
}

// listTerminalSessions is satisfied by [*session.Manager] for
// GET /api/terminal/sessions.
type listTerminalSessions interface {
	ActiveIDs() []session.ID
}

// terminalSessionStarter is satisfied by [*session.Manager]; allows
// tests to inject a stub.
type terminalSessionStarter interface {
	Start(id session.ID, opts session.StartOptions, fn func(context.Context, *session.Session)) (*session.Session, error)
	Get(id session.ID) (*session.Session, bool)
	Touch(id session.ID)
	Stop(id session.ID)
}

// writeJSON serialises v as JSON to w. Caller should not have written
// the body before calling.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleSSHWebSocket upgrades the connection and starts a
// goroutine-backed terminal session.
//
// New connection: requires query parameter target_id; the user must
// have access to that target.
//
// Resume (attach): requires query parameter session_id; no target_id
// or credentials are sent. The caller must own the existing session.
//
//nolint:gocyclo // handles both new-connection and resume code paths.
func (a *App) handleSSHWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		audit("terminal_ws_unauthorized", auditFields{
			"reason": "no_session_cookie",
		})
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		audit("terminal_ws_unauthorized", auditFields{
			"reason": "invalid_session",
		})
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}

	// Resume (attach) to an existing session.
	if sessionIDParam := r.URL.Query().Get("session_id"); sessionIDParam != "" {
		a.handleTerminalAttach(w, r, sess.UserID, sessionIDParam)
		return
	}

	targetID := r.URL.Query().Get("target_id")
	userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}

	if !supportsDetachableTerminal(target.Protocol) {
		audit("terminal_ws_not_implemented", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"protocol":  target.Protocol,
		})
		writeJSONErrorKey(w, r, "sessions.onlySSHTelnet", http.StatusNotImplemented)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		audit("terminal_ws_upgrade_failed", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "common.failedUpgradeConnection", http.StatusBadRequest)
		return
	}

	creds, err := readTerminalCredentials(conn, target)
	if err != nil {
		audit("terminal_ws_credentials_invalid", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		_ = conn.Close()
		return
	}
	audit("terminal_ws_credentials_ok", auditFields{
		"user_id":   userID,
		"target_id": targetID,
		"ssh_user":  creds.Username,
	})

	var id session.ID
	if terminalSessionIDGen != nil {
		id = terminalSessionIDGen()
	} else {
		id, err = newTerminalSessionID()
		if err != nil {
			audit("terminal_session_idgen_failed", auditFields{
				"user_id":   sess.UserID,
				"target_id": targetID,
				"error":     err.Error(),
			})
			_ = conn.Close()
			http.Error(w, "failed to start terminal session", http.StatusInternalServerError)
			return
		}
	}

	opts := session.StartOptions{
		UserID:      userID,
		TargetID:    targetID,
		TargetName:  target.Name,
		Name:        creds.Name,
		Description: creds.Description,
	}
	q := r.URL.Query()
	cols, rows := 80, 24
	if c := q.Get("cols"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 && n <= 512 {
			cols = n
		}
	}
	if rv := q.Get("rows"); rv != "" {
		if n, err := strconv.Atoi(rv); err == nil && n > 0 && n <= 256 {
			rows = n
		}
	}
	_, err = a.TerminalSessionManager.Start(id, opts, func(ctx context.Context, termSess *session.Session) {
		a.runDetachableBridge(ctx, termSess, a.TerminalSessionManager, id, conn, target, creds, cols, rows)
		if a.SessionEventBroker != nil {
			a.SessionEventBroker.Broadcast()
		}
	})
	if err != nil {
		audit("terminal_session_start_failed", auditFields{
			"user_id":   sess.UserID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		_ = conn.Close()
		http.Error(w, "failed to start terminal session", http.StatusInternalServerError)
		return
	}
	audit("terminal_session_start", auditFields{
		"session_id": id,
		"user_id":    sess.UserID,
		"target_id":  targetID,
		"host":       target.Host,
		"port":       target.Port,
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
}

// handleTerminalAttach handles the resume / viewer branch of /ws/ssh.
//
// Two flavors are supported:
//
//   - mode=writer (default): the caller owns the backgrounded session
//     and wants to re-attach. Requires owner identity or current
//     write-token holder.
//   - mode=viewer: the caller has already accepted an invitation and
//     joined via POST /api/terminal/sessions/{id}/join. The bridge
//     attaches them as a read-only client and silently drops their
//     stdin until they obtain the write token.
func (a *App) handleTerminalAttach(w http.ResponseWriter, r *http.Request, userID, sessionIDParam string) {
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	viewer := mode == "viewer"
	termSess, room, err := a.terminalSessionByOwnerOrParticipant(r.Context(), sessionIDParam, userID, viewer)
	if err != nil {
		switch {
		case errors.Is(err, sharing.ErrRoomNotFound),
			errors.Is(err, sharing.ErrParticipantMissing):
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		case errors.Is(err, sharing.ErrNotWriter):
			writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		default:
			writeInternalError(w, err)
		}
		return
	}
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		audit("terminal_ws_upgrade_failed_attach", auditFields{
			"session_id": sessionIDParam,
			"error":      err.Error(),
		})
		writeJSONErrorKey(w, r, "common.failedUpgradeConnection", http.StatusBadRequest)
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte("vantyx:ready"))
	if err := writeTerminalWsMeta(conn, struct {
		SessionID string `json:"session_id"`
	}{SessionID: sessionIDParam}); err != nil {
		_ = conn.Close()
		return
	}
	attachMode := session.AttachModeWriter
	if viewer {
		attachMode = session.AttachModeViewer
	}
	username := a.usernameFor(r.Context(), userID)
	select {
	case termSess.AttachCh <- session.AttachReq{Conn: conn, UserID: userID, Username: username, Mode: attachMode}:
		audit("terminal_session_attach", auditFields{
			"session_id": sessionIDParam,
			"user_id":    userID,
			"mode":       string(modeLabel(attachMode)),
		})
		// If a writer reconnects, make sure the bridge knows; the
		// previous controller may have demoted them after a kick or
		// reload of the room state.
		if !viewer && a.SharingBridges != nil {
			if controller, ok := a.SharingBridges.Get(termSess.ID()); ok {
				controller.SetWriter(roomWriterID(room))
			}
		}
	default:
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: session attach slot busy"))
		_ = conn.Close()
	}
}

// modeLabel maps session.AttachMode to its short audit-log string.
func modeLabel(m session.AttachMode) string {
	if m == session.AttachModeViewer {
		return "viewer"
	}
	return "writer"
}

// TerminalSessionItem is one entry in the GET /api/terminal/sessions
// response.
type TerminalSessionItem struct {
	SessionID   string    `json:"session_id"`
	TargetID    string    `json:"target_id"`
	TargetName  string    `json:"target_name"`
	TargetPath  string    `json:"target_path,omitempty"` // hierarchical path of the target (e.g. "prod/network").
	Protocol    string    `json:"protocol"`              // target protocol (ssh / telnet / ...).
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
	Idle        bool      `json:"idle"`
	IdleSeconds int       `json:"idle_seconds,omitempty"`
	// Role indicates how the current user is attached to this session:
	// "owner" if they own the underlying terminal (and therefore are
	// the default writer), or "viewer" if they have joined via a
	// sharing invitation. Empty when the session listing is used in
	// pre-collaboration code paths.
	Role          string `json:"role,omitempty"`
	OwnerUserID   string `json:"owner_user_id,omitempty"`
	OwnerUsername string `json:"owner_username,omitempty"`
}

func terminalSessionItemFrom(sess *session.Session, mgr *session.Manager, protocol access.Protocol, targetPath string) TerminalSessionItem {
	item := TerminalSessionItem{
		SessionID:   string(sess.ID()),
		TargetID:    sess.TargetID,
		TargetName:  sess.TargetName,
		TargetPath:  targetPath,
		Protocol:    string(protocol),
		Name:        sess.Name,
		Description: sess.Description,
		CreatedAt:   sess.CreatedAt(),
		LastSeen:    sess.LastSeen(),
	}
	if mgr != nil && mgr.IsIdle(sess) {
		item.Idle = true
		item.IdleSeconds = int(mgr.IdleDuration(sess).Seconds())
	}
	return item
}

// handleTerminalSessions returns the list of active terminal sessions
// for the current user.
func (a *App) handleTerminalSessions(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}

	lister, ok := a.TerminalSessionManager.(listTerminalSessions)
	if !ok {
		writeJSON(w, map[string]interface{}{"items": []TerminalSessionItem{}})
		return
	}
	ctx := r.Context()
	allowedSet, err := a.allowedTargetIDSet(ctx, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var mgr *session.Manager
	if m, ok := a.TerminalSessionManager.(*session.Manager); ok {
		mgr = m
	}
	ids := lister.ActiveIDs()
	seen := make(map[session.ID]struct{}, len(ids))
	items := make([]TerminalSessionItem, 0, len(ids))
	for _, id := range ids {
		sess, ok := a.TerminalSessionManager.Get(id)
		if !ok || sess.UserID != userID {
			continue
		}
		if _, ok := allowedSet[access.TargetID(sess.TargetID)]; !ok {
			continue
		}
		protocol := access.ProtocolSSH
		targetPath := ""
		if target, err := a.TargetStore.Get(ctx, access.TargetID(sess.TargetID)); err == nil && target != nil {
			protocol = target.Protocol
			targetPath = target.Path
		}
		item := terminalSessionItemFrom(sess, mgr, protocol, targetPath)
		item.Role = "owner"
		item.OwnerUserID = sess.UserID
		item.OwnerUsername = a.usernameFor(ctx, sess.UserID)
		items = append(items, item)
		seen[id] = struct{}{}
	}
	// Add sessions where the current user is participating as a
	// viewer / writer through the sharing registry. The room's owner
	// must still have access to the target; we re-check the viewer's
	// own access too so a revoked grant does not surface stale rows.
	if a.SharingRegistry != nil {
		for _, sid := range a.SharingRegistry.RoomsForUser(userID) {
			id := session.ID(sid)
			if _, dup := seen[id]; dup {
				continue
			}
			sess, ok := a.TerminalSessionManager.Get(id)
			if !ok {
				continue
			}
			if _, ok := allowedSet[access.TargetID(sess.TargetID)]; !ok {
				continue
			}
			protocol := access.ProtocolSSH
			targetPath := ""
			if target, err := a.TargetStore.Get(ctx, access.TargetID(sess.TargetID)); err == nil && target != nil {
				protocol = target.Protocol
				targetPath = target.Path
			}
			item := terminalSessionItemFrom(sess, mgr, protocol, targetPath)
			item.Role = "viewer"
			item.OwnerUserID = sess.UserID
			item.OwnerUsername = a.usernameFor(ctx, sess.UserID)
			items = append(items, item)
			seen[id] = struct{}{}
		}
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleTerminalSessionDelete terminates the given terminal session.
// Caller must own the session.
func (a *App) handleTerminalSessionDelete(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONErrorKey(w, r, "sessions.idRequired", http.StatusBadRequest)
		return
	}
	id := session.ID(sessionID)
	termSess, ok := a.TerminalSessionManager.Get(id)
	if !ok || termSess.UserID != userID {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	canAccess, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(termSess.TargetID))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canAccess {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	a.TerminalSessionManager.Stop(id)
	audit("terminal_session_stop", auditFields{
		"session_id": sessionID,
		"user_id":    userID,
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
	w.WriteHeader(http.StatusNoContent)
}

// runDetachableBridge starts the SSH / Telnet bridge and keeps it
// running when the client detaches. The initial conn is attached
// first; further attaches (resume) come via termSess.AttachCh.
//
// When VANTYX_RECORDINGS_DIR is set an asciinema-format recording is
// written and metadata is stored in the recordings table.
//
// cols / rows are the initial terminal size (from the client) used for
// PTY and recording; 0 falls back to the bridge default.
//
//nolint:gocyclo // bridge coordinates recording, command logging, and the upstream bridge.
func (a *App) runDetachableBridge(ctx context.Context, termSess *session.Session, manager terminalSessionStarter, id session.ID, conn *websocket.Conn, target *access.Target, creds sshproxy.Credentials, cols, rows int) {
	_ = conn.WriteMessage(websocket.TextMessage, []byte("vantyx:ready"))
	// Send session_id so the client can reconnect (resume) without credentials.
	_ = writeTerminalWsMeta(conn, struct {
		SessionID string `json:"session_id"`
	}{SessionID: string(id)})
	touch := func() { manager.Touch(id) }

	tee, stdinRecorder, recordingCloser := a.setupRecording(ctx, termSess, id, cols, rows)
	if recordingCloser != nil {
		defer recordingCloser()
	}
	tee, stdinRecorder = a.wrapWithCommandLog(termSess, id, tee, stdinRecorder)

	// Initial WebSocket attach is the session owner. Pass user metadata
	// down so the bridge knows who to consider as the writer.
	ownerAttach := session.AttachReq{Conn: conn, UserID: termSess.UserID, Mode: session.AttachModeWriter}
	if a.SharingBridges != nil {
		defer a.SharingBridges.Unregister(id)
	}
	var bridgeErr error
	var endReason, endMsg string
	switch target.Protocol {
	case access.ProtocolTelnet:
		endReason = "telnet_session_closed"
		endMsg = "session_ended: Telnet session closed"
		var telStdin telnetproxy.StdinRecorder
		if stdinRecorder != nil {
			telStdin = telnetproxy.StdinRecorderFunc(stdinRecorder.RecordInput)
		}
		var sink telnetproxy.BridgeControlSink
		if a.SharingBridges != nil {
			sink = telnetBridgeSink{id: id, br: a.SharingBridges}
		}
		bridgeErr = telnetproxy.RunBridgeDetachable(ctx, endMsg, target.Host, target.Port, creds.Username, creds.Password, termSess.Output, termSess.AttachCh, ownerAttach, touch, tee, telStdin, cols, rows, nil, sink)
	default:
		endReason = "ssh_session_closed"
		endMsg = "session_ended: SSH session closed"
		opts := []sshproxy.BridgeOption{sshproxy.WithHostKeyFingerprint(target.SSHHostKeyFingerprint)}
		if a.SharingBridges != nil {
			opts = append(opts, sshproxy.WithBridgeControlSink(sshBridgeSink{id: id, br: a.SharingBridges}))
		}
		bridgeErr = sshproxy.RunBridgeDetachable(ctx, endMsg, target.Host, target.Port, creds.Username, creds.Password, creds.PrivateKey, creds.PrivateKeyPassphrase, termSess.Output, termSess.AttachCh, ownerAttach, touch, tee, stdinRecorder, cols, rows, nil, opts...)
	}
	if bridgeErr != nil {
		audit("terminal_bridge_end_error", auditFields{
			"session_id": id,
			"error":      proxyerrors.UnwrapForAudit(bridgeErr),
		})
		if frame, evt, ok := encodeHostKeyErrorFrame(bridgeErr, target); ok {
			audit(evt, auditFields{
				"session_id": id,
				"target_id":  string(target.ID),
				"host":       target.Host,
				"port":       target.Port,
			})
			_ = conn.WriteMessage(websocket.TextMessage, wrapTerminalWsMeta(frame))
		} else {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+localizedBridgeMessage(ctx, bridgeErr)))
		}
	} else {
		audit("terminal_bridge_end", auditFields{
			"session_id": id,
			"reason":     endReason,
		})
		_ = conn.WriteMessage(websocket.TextMessage, []byte(endMsg))
	}
	_ = conn.Close()
	// Returning from this callback lets the Manager goroutine close
	// sess.done and call delete(m.sessions, id). Calling
	// manager.Stop(id) here would deadlock because Stop waits on
	// <-sess.done, which is only closed once this callback returns.
}

// setupRecording configures asciinema recording for a session when
// VANTYX_RECORDINGS_DIR is set. It returns the writer tee, the stdin
// recorder, and a closer that flushes / closes the cast file and
// updates the recordings table. When recording is disabled all three
// return values are zero.
func (a *App) setupRecording(ctx context.Context, termSess *session.Session, id session.ID, cols, rows int) (io.Writer, sshproxy.StdinRecorder, func()) {
	recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
	if recordingDir == "" {
		return nil, nil, nil
	}
	_ = os.MkdirAll(recordingDir, 0750) // #nosec G703 -- path from env, dir is admin-configured.
	// Session IDs are RFC3339Nano timestamps containing colons. Replace
	// characters that are illegal on Windows file systems so the
	// recording file can be opened by tooling on any host.
	safeName := strings.ReplaceAll(string(id), ":", "-")
	safeName = strings.ReplaceAll(safeName, ".", "-")
	castPath := filepath.Join(recordingDir, safeName+".cast")
	// Recordings may contain sensitive output (passwords, tokens), so
	// open with 0o600 to bypass the user's umask (M-18 / CWE-732).
	f, err := os.OpenFile(castPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G703 G304 -- path under recordingDir, safeName sanitised.
	if err != nil {
		audit("recording_create_failed", auditFields{
			"session_id": id,
			"path":       castPath,
			"error":      err.Error(),
		})
		return nil, nil, nil
	}
	startedAt := time.Now().UTC()
	w, h := cols, rows
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	asc := recording.NewAsciinemaWriter(f, w, h)
	if a.DB != nil {
		sessName := termSess.Name
		sessDesc := termSess.Description
		if _, err := a.DB.ExecContext(ctx, `INSERT INTO recordings (id, user_id, target_id, session_id, channel_type, file_path, started_at, session_name, session_description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(id), termSess.UserID, termSess.TargetID, string(id), "browser", castPath, startedAt.Format("2006-01-02 15:04:05"), sessName, sessDesc); err != nil {
			audit("recording_insert_failed", auditFields{
				"session_id": id,
				"error":      err.Error(),
			})
		}
	}
	closer := func() {
		_ = f.Sync() // flush the OS buffer to disk before closing.
		_ = f.Close()
		if a.DB != nil {
			_, _ = a.DB.ExecContext(context.Background(), `UPDATE recordings SET ended_at = ? WHERE id = ?`, time.Now().UTC().Format("2006-01-02 15:04:05"), string(id))
		}
	}
	return asc, asc, closer
}

// wrapWithCommandLog adds a command-log recorder on top of an existing
// tee / stdin recorder pair. The recorder captures stdin and PTY echo
// (for tab completion) into the command_logs table.
func (a *App) wrapWithCommandLog(termSess *session.Session, id session.ID, tee io.Writer, stdinRecorder sshproxy.StdinRecorder) (io.Writer, sshproxy.StdinRecorder) {
	cmdRec := newCommandLogRecorder(a.CommandLogStore, string(id), termSess.UserID, termSess.TargetID)
	if cmdRec == nil {
		return tee, stdinRecorder
	}
	prev := stdinRecorder
	wrapped := sshproxy.StdinRecorderFunc(func(p []byte) {
		if prev != nil {
			prev.RecordInput(p)
		}
		cmdRec.RecordInput(p)
	})
	stdoutTap := commandLogStdoutWriter{rec: cmdRec}
	if tee != nil {
		tee = io.MultiWriter(tee, stdoutTap)
	} else {
		tee = stdoutTap
	}
	return tee, wrapped
}
