package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/sharing"
)

// kickedUserIDs is a nil-safe accessor for the room's rejoin block list,
// exposed on the participants list so the owner's UI can offer
// "allow rejoin".
func kickedUserIDs(room *sharing.Room) []string {
	if room == nil {
		return []string{}
	}
	return room.KickedUserIDs()
}

// allowRejoin lifts a kick block for target in the given room, audits it,
// and writes 204. Shared by the terminal and VNC/RDP handlers.
func (a *App) allowRejoin(w http.ResponseWriter, r *http.Request, room *sharing.Room, sessionID, ownerID, target string) {
	if strings.TrimSpace(target) == "" {
		writeJSONErrorKey(w, r, "sharing.userIDRequired", http.StatusBadRequest)
		return
	}
	if room == nil || !room.Unkick(target) {
		writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusNotFound)
		return
	}
	audit("session_participant_unkicked", auditFields{
		"user_id":    ownerID,
		"session_id": sessionID,
		"target_id":  target,
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAllowRejoin (terminal sessions): POST
// /api/terminal/sessions/{session_id}/participants/{user_id}/allow-rejoin.
// Owner only. Lifts the block set when the owner removed the user so a
// later invitation (of any type) lets them back in.
func (a *App) handleAllowRejoin(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	termSess, ownerID, ok := a.loadOwnedTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	room, _ := a.SharingRegistry.Get(string(termSess.ID()))
	a.allowRejoin(w, r, room, string(termSess.ID()), ownerID, chi.URLParam(r, "user_id"))
}

func (a *App) handleKindAllowRejoin(w http.ResponseWriter, r *http.Request, kind sharing.Kind) {
	sessionID := chi.URLParam(r, "session_id")
	meta, ok := a.loadOwnedSharingMeta(w, r, sessionID, kind)
	if !ok {
		return
	}
	room, _ := a.SharingRegistry.Get(meta.SessionID)
	a.allowRejoin(w, r, room, meta.SessionID, meta.OwnerID, chi.URLParam(r, "user_id"))
}

func (a *App) handleVNCAllowRejoin(w http.ResponseWriter, r *http.Request) {
	a.handleKindAllowRejoin(w, r, sharing.KindVNC)
}

func (a *App) handleRDPAllowRejoin(w http.ResponseWriter, r *http.Request) {
	a.handleKindAllowRejoin(w, r, sharing.KindRDP)
}
