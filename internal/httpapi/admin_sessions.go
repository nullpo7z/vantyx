package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
)

// Admin session oversight: list every live session, watch one as a
// read-only participant, or terminate it. All three are admin-only and
// audited; the owner and participants learn about terminations through
// the session event stream and the closing bridge.

// adminSessionItem is one row of GET /api/admin/sessions.
type adminSessionItem struct {
	Kind          string    `json:"kind"` // terminal | vnc | rdp
	SessionID     string    `json:"session_id"`
	OwnerUserID   string    `json:"owner_user_id"`
	OwnerUsername string    `json:"owner_username"`
	TargetID      string    `json:"target_id"`
	TargetName    string    `json:"target_name"`
	TargetPath    string    `json:"target_path,omitempty"`
	Protocol      string    `json:"protocol"`
	Name          string    `json:"name,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeen      time.Time `json:"last_seen"`
	Idle          bool      `json:"idle"`
	IdleSeconds   int       `json:"idle_seconds,omitempty"`
	// Participants lists everyone in the sharing room besides the owner
	// (viewers / writers), including admins who are watching.
	Participants []string `json:"participants"`
	// Watching is true when the calling admin is already a participant.
	Watching bool `json:"watching"`
}

func (a *App) roomParticipantNames(sessionID, ownerID, adminID string) (names []string, watching bool) {
	names = []string{}
	if a.SharingRegistry == nil {
		return names, false
	}
	room, ok := a.SharingRegistry.Get(sessionID)
	if !ok {
		return names, false
	}
	for _, p := range room.Participants() {
		if p.UserID == ownerID {
			continue
		}
		if p.UserID == adminID {
			watching = true
		}
		label := p.Username
		if label == "" {
			label = p.UserID
		}
		names = append(names, label)
	}
	sort.Strings(names)
	return names, watching
}

// handleAdminListSessions: GET /api/admin/sessions.
func (a *App) handleAdminListSessions(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	ctx := r.Context()
	adminID := a.currentUserID(r)
	items := make([]adminSessionItem, 0)

	targetInfo := func(targetID string) (name, path string, protocol access.Protocol) {
		protocol = access.ProtocolSSH
		if t, err := a.TargetStore.Get(ctx, access.TargetID(targetID)); err == nil && t != nil {
			return t.Name, t.Path, t.Protocol
		}
		return targetID, "", protocol
	}

	if lister, ok := a.TerminalSessionManager.(listTerminalSessions); ok {
		var mgr *session.Manager
		if m, ok := a.TerminalSessionManager.(*session.Manager); ok {
			mgr = m
		}
		for _, id := range lister.ActiveIDs() {
			sess, ok := a.TerminalSessionManager.Get(id)
			if !ok {
				continue
			}
			name, path, protocol := targetInfo(sess.TargetID)
			ti := terminalSessionItemFrom(sess, mgr, protocol, path)
			parts, watching := a.roomParticipantNames(string(id), sess.UserID, adminID)
			items = append(items, adminSessionItem{
				Kind: "terminal", SessionID: string(id),
				OwnerUserID: sess.UserID, OwnerUsername: a.usernameFor(ctx, sess.UserID),
				TargetID: sess.TargetID, TargetName: firstNonEmpty(sess.TargetName, name), TargetPath: path,
				Protocol: string(protocol), Name: sess.Name,
				CreatedAt: ti.CreatedAt, LastSeen: ti.LastSeen, Idle: ti.Idle, IdleSeconds: ti.IdleSeconds,
				Participants: parts, Watching: watching,
			})
		}
	}
	if a.VNCSessionManager != nil {
		for _, id := range a.VNCSessionManager.ActiveIDs() {
			sess, ok := a.VNCSessionManager.Get(id)
			if !ok {
				continue
			}
			name, path, _ := targetInfo(sess.TargetID)
			ti := terminalSessionItemFrom(sess, a.VNCSessionManager, access.ProtocolVNC, path)
			parts, watching := a.roomParticipantNames(string(id), sess.UserID, adminID)
			items = append(items, adminSessionItem{
				Kind: "vnc", SessionID: string(id),
				OwnerUserID: sess.UserID, OwnerUsername: a.usernameFor(ctx, sess.UserID),
				TargetID: sess.TargetID, TargetName: firstNonEmpty(sess.TargetName, name), TargetPath: path,
				Protocol: string(access.ProtocolVNC), Name: sess.Name,
				CreatedAt: ti.CreatedAt, LastSeen: ti.LastSeen, Idle: ti.Idle, IdleSeconds: ti.IdleSeconds,
				Participants: parts, Watching: watching,
			})
		}
	}
	if a.RDPVNCManager != nil {
		for _, s := range a.RDPVNCManager.AllSessions() {
			name, path, _ := targetInfo(s.TargetID)
			ri := rdpSessionItemFrom(s, a.RDPVNCManager)
			parts, watching := a.roomParticipantNames(s.ID, s.UserID, adminID)
			items = append(items, adminSessionItem{
				Kind: "rdp", SessionID: s.ID,
				OwnerUserID: s.UserID, OwnerUsername: a.usernameFor(ctx, s.UserID),
				TargetID: s.TargetID, TargetName: firstNonEmpty(s.TargetName, name), TargetPath: path,
				Protocol:  string(access.ProtocolRDP),
				CreatedAt: ri.CreatedAt, LastSeen: ri.LastSeen, Idle: ri.Idle, IdleSeconds: ri.IdleSeconds,
				Participants: parts, Watching: watching,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, map[string]interface{}{"items": items})
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// adminSessionOwner resolves kind/session_id to (ownerID, targetID, targetName).
func (a *App) adminSessionOwner(kind, sessionID string) (ownerID, targetID, targetName string, ok bool) {
	switch kind {
	case "terminal":
		if sess, found := a.TerminalSessionManager.Get(session.ID(sessionID)); found {
			return sess.UserID, sess.TargetID, sess.TargetName, true
		}
	case "vnc":
		if a.VNCSessionManager != nil {
			if sess, found := a.VNCSessionManager.Get(session.ID(sessionID)); found {
				return sess.UserID, sess.TargetID, sess.TargetName, true
			}
		}
	case "rdp":
		if a.RDPVNCManager != nil {
			if s, found := a.RDPVNCManager.GetSession(sessionID); found {
				return s.UserID, s.TargetID, s.TargetName, true
			}
		}
	}
	return "", "", "", false
}

type adminTerminateRequest struct {
	Reason string `json:"reason"`
}

// handleAdminTerminateSession: DELETE /api/admin/sessions/{kind}/{session_id}.
func (a *App) handleAdminTerminateSession(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	kind := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "kind")))
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	ownerID, targetID, _, ok := a.adminSessionOwner(kind, sessionID)
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	var req adminTerminateRequest
	if r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	adminID := a.currentUserID(r)
	adminName := a.usernameFor(r.Context(), adminID)
	reason := strings.TrimSpace(req.Reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}
	// Tell the owner and every participant first: the bridge closes their
	// connections right after and the UI needs the "why".
	a.publishSharingEventForSession(sessionID, ownerID, "session_terminated_by_admin", adminID, adminName, map[string]interface{}{
		"reason": reason,
		"kind":   kind,
	})
	cleanStop := true
	switch kind {
	case "terminal":
		cleanStop = a.TerminalSessionManager.Stop(session.ID(sessionID))
	case "vnc":
		cleanStop = a.VNCSessionManager.Stop(session.ID(sessionID))
	case "rdp":
		a.finishVideoRecording(sessionID)
		a.RDPVNCManager.RemoveSession(sessionID)
	}
	if a.SharingRegistry != nil {
		a.SharingRegistry.Remove(sessionID)
	}
	fields := auditFields{
		"user_id":    adminID,
		"session_id": sessionID,
		"kind":       kind,
		"owner_id":   ownerID,
		"target_id":  targetID,
		"reason":     reason,
	}
	if !cleanStop {
		fields["clean_stop"] = false
	}
	audit("session_terminated_by_admin", fields)
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminWatchSession: POST /api/admin/sessions/{kind}/{session_id}/watch
// adds the calling admin to the session's sharing room as a viewer
// (no invitation needed) and returns the page URL to open. The bridge
// then attaches them read-only exactly like an invited viewer; the owner
// sees them in the participant list and gets a participant_joined event.
func (a *App) handleAdminWatchSession(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if a.SharingRegistry == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "kind")))
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	ownerID, targetID, targetName, ok := a.adminSessionOwner(kind, sessionID)
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	adminID := a.currentUserID(r)
	if adminID == ownerID {
		writeJSONErrorKey(w, r, "sharing.cannotInviteSelf", http.StatusBadRequest)
		return
	}
	adminName := a.usernameFor(r.Context(), adminID)
	room := a.SharingRegistry.EnsureRoom(sessionID, targetID, ownerID, a.usernameFor(r.Context(), ownerID))
	if !room.IsParticipant(adminID) {
		if err := room.AddViewer(adminID, adminName, "admin-watch", time.Now().UTC()); err != nil {
			writeInternalError(w, err)
			return
		}
		audit("session_watched_by_admin", auditFields{
			"user_id":    adminID,
			"session_id": sessionID,
			"kind":       kind,
			"owner_id":   ownerID,
			"target_id":  targetID,
		})
		a.publishSharingEventForSession(sessionID, ownerID, sharing.EventParticipantJoined, adminID, adminName, map[string]interface{}{"admin_watch": true})
	}
	q := url.Values{}
	q.Set("session_id", sessionID)
	q.Set("mode", "viewer")
	q.Set("target_id", targetID)
	if targetName != "" {
		q.Set("target_name", targetName)
	}
	path := map[string]string{"terminal": "/terminal", "vnc": "/vnc", "rdp": "/rdp"}[kind]
	writeJSON(w, map[string]interface{}{
		"session_id": sessionID,
		"kind":       kind,
		"role":       string(sharing.RoleViewer),
		"url":        path + "?" + q.Encode(),
	})
}
