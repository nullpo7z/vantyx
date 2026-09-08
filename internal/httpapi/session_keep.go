package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/session"
)

// Session keep flag: a user marks one of their live sessions as
// intentionally left running (e.g. a long job or an agent), which stops
// it being flagged idle in the sessions view. It is a per-session,
// in-memory hint only -- it does not extend any lifetime or bypass an
// admin terminate; a fresh session starts unpinned.

type setSessionKeepRequest struct {
	Keep bool `json:"keep"`
}

// handleSetSessionKeep: PUT /api/{terminal|vnc|rdp}/sessions/{id}/keep.
// The owner (or an admin) pins or unpins the session. The path prefix
// selects which session manager holds it.
func (a *App) handleSetSessionKeep(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		writeJSONErrorKey(w, r, "sessions.idRequired", http.StatusBadRequest)
		return
	}
	var req setSessionKeepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	isAdmin, err := a.currentUserIsAdmin(r)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	// Resolve the owner and set the flag on whichever manager owns the
	// session, matching the mount prefix.
	kind := "terminal"
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/vnc/"):
		kind = "vnc"
	case strings.HasPrefix(r.URL.Path, "/api/rdp/"):
		kind = "rdp"
	}

	var ownerID string
	var ok bool
	switch kind {
	case "vnc":
		if a.VNCSessionManager == nil {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return
		}
		if sess, found := a.VNCSessionManager.Get(session.ID(sessionID)); found {
			ownerID, ok = sess.UserID, true
		}
	case "rdp":
		if a.RDPVNCManager == nil {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return
		}
		if s, found := a.RDPVNCManager.GetSession(sessionID); found {
			ownerID, ok = s.UserID, true
		}
	default:
		if sess, found := a.TerminalSessionManager.Get(session.ID(sessionID)); found {
			ownerID, ok = sess.UserID, true
		}
	}
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	if ownerID != userID && !isAdmin {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}

	switch kind {
	case "vnc":
		a.VNCSessionManager.SetKeep(session.ID(sessionID), req.Keep)
	case "rdp":
		a.RDPVNCManager.SetKeep(sessionID, req.Keep)
	default:
		if m, isMgr := a.TerminalSessionManager.(*session.Manager); isMgr {
			m.SetKeep(session.ID(sessionID), req.Keep)
		}
	}
	audit("session_keep_set", auditFields{
		"user_id":    userID,
		"session_id": sessionID,
		"kind":       kind,
		"keep":       req.Keep,
		"owner_id":   ownerID,
	})
	writeJSON(w, map[string]interface{}{"session_id": sessionID, "keep": req.Keep})
}
