package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
)

func (a *App) handleSharingJoin(w http.ResponseWriter, r *http.Request, expectedSessionID string, expectedKind sharing.Kind, ownerID string) {
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if sharingRecordingRequired(w, r) {
		return
	}
	body, err := decodeJoinInvitationBody(r)
	if err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if body.InvitationID == "" && body.InvitationToken == "" {
		writeJSONErrorKey(w, r, "sharing.tokenOrIDRequired", http.StatusBadRequest)
		return
	}
	inv, lookupErr := a.lookupJoinInvitation(r.Context(), body)
	if lookupErr != nil {
		if errors.Is(lookupErr, errSharingLinkTokenRequired) {
			writeJSONErrorKey(w, r, "sharing.linkTokenRequired", http.StatusBadRequest)
			return
		}
		if errors.Is(lookupErr, sharing.ErrInvitationNotFound) {
			writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, lookupErr)
		return
	}
	if !a.validateJoinInvitation(w, r, inv, userID, expectedSessionID, expectedKind) {
		return
	}
	a.completeSharingJoin(w, r, inv, userID, expectedSessionID, ownerID)
}

func (a *App) handleVNCJoinSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	vncSess, ok := a.VNCSessionManager.Get(session.ID(sessionID))
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	a.handleSharingJoin(w, r, string(vncSess.ID()), sharing.KindVNC, vncSess.UserID)
}

func (a *App) handleRDPJoinSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	rdpSess, ok := a.RDPVNCManager.GetSession(sessionID)
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	a.handleSharingJoin(w, r, rdpSess.ID, sharing.KindRDP, rdpSess.UserID)
}

// handleVNCKickParticipant removes a viewer from a VNC session (owner only).
func (a *App) handleVNCKickParticipant(w http.ResponseWriter, r *http.Request) {
	a.handleKindKickParticipant(w, r, sharing.KindVNC)
}
