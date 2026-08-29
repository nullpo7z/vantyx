package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
)

// sharingSessionMeta identifies a collaborative session regardless of kind.
type sharingSessionMeta struct {
	SessionID string
	TargetID  string
	OwnerID   string
	Kind      sharing.Kind
}

// publishSharingEventForSession is the VNC/RDP counterpart of
// publishSharingEvent; alsoNotify names recipients outside the current
// participant list (e.g. a user who was just kicked).
func (a *App) publishSharingEventForSession(sessionID, ownerID, event, userID, username string, extra map[string]interface{}, alsoNotify ...string) {
	if a.SessionEventBroker == nil || sessionID == "" {
		return
	}
	payload := sharing.Event{
		Type:      event,
		SessionID: sessionID,
		UserID:    userID,
		Username:  username,
		Extra:     extra,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	users := []string{ownerID}
	if a.SharingRegistry != nil {
		if room, ok := a.SharingRegistry.Get(sessionID); ok {
			for _, p := range room.Participants() {
				users = append(users, p.UserID)
			}
		}
	}
	users = append(users, alsoNotify...)
	a.SessionEventBroker.PublishToUsers(body, users...)
	a.SessionEventBroker.Broadcast()
}

func (a *App) disconnectSharingUser(sessionID session.ID, room *sharing.Room, userID string) {
	if room == nil || userID == "" {
		return
	}
	_ = room.RemoveParticipant(userID)
	if a.SharingBridges != nil {
		if controller, ok := a.SharingBridges.Get(sessionID); ok {
			controller.DetachUser(userID)
			controller.SetWriter(room.WriterID())
		}
	}
}

func (a *App) loadOwnedSharingMeta(w http.ResponseWriter, r *http.Request, sessionID string, kind sharing.Kind) (*sharingSessionMeta, bool) {
	switch kind {
	case sharing.KindVNC:
		vncSess, ownerID, ok := a.loadOwnedVNCSession(w, r, sessionID)
		if !ok {
			return nil, false
		}
		return &sharingSessionMeta{
			SessionID: string(vncSess.ID()),
			TargetID:  vncSess.TargetID,
			OwnerID:   ownerID,
			Kind:      kind,
		}, true
	case sharing.KindRDP:
		rdpSess, ownerID, ok := a.loadOwnedRDPSession(w, r, sessionID)
		if !ok {
			return nil, false
		}
		return &sharingSessionMeta{
			SessionID: rdpSess.ID,
			TargetID:  rdpSess.TargetID,
			OwnerID:   ownerID,
			Kind:      kind,
		}, true
	default:
		termSess, ownerID, ok := a.loadOwnedTerminalSession(w, r, sessionID)
		if !ok {
			return nil, false
		}
		return &sharingSessionMeta{
			SessionID: string(termSess.ID()),
			TargetID:  termSess.TargetID,
			OwnerID:   ownerID,
			Kind:      sharing.KindTerminal,
		}, true
	}
}

func (a *App) loadParticipatingSharingMeta(w http.ResponseWriter, r *http.Request, sessionID string, kind sharing.Kind) (*sharingSessionMeta, *sharing.Room, string, bool) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return nil, nil, "", false
	}
	switch kind {
	case sharing.KindVNC:
		if a.VNCSessionManager == nil {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return nil, nil, "", false
		}
		vncSess, ok := a.VNCSessionManager.Get(session.ID(sessionID))
		if !ok {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return nil, nil, "", false
		}
		room, _ := a.SharingRegistry.Get(string(vncSess.ID()))
		if vncSess.UserID != userID && (room == nil || !room.IsParticipant(userID)) {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return nil, nil, "", false
		}
		meta := &sharingSessionMeta{
			SessionID: string(vncSess.ID()),
			TargetID:  vncSess.TargetID,
			OwnerID:   vncSess.UserID,
			Kind:      kind,
		}
		return meta, room, userID, true
	case sharing.KindRDP:
		if a.RDPVNCManager == nil {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return nil, nil, "", false
		}
		rdpSess, ok := a.RDPVNCManager.GetSession(sessionID)
		if !ok {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return nil, nil, "", false
		}
		room, _ := a.SharingRegistry.Get(rdpSess.ID)
		if rdpSess.UserID != userID && (room == nil || !room.IsParticipant(userID)) {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return nil, nil, "", false
		}
		meta := &sharingSessionMeta{
			SessionID: rdpSess.ID,
			TargetID:  rdpSess.TargetID,
			OwnerID:   rdpSess.UserID,
			Kind:      kind,
		}
		return meta, room, userID, true
	default:
		termSess, room, uid, ok := a.loadParticipatingTerminalSession(w, r, sessionID)
		if !ok {
			return nil, nil, "", false
		}
		meta := &sharingSessionMeta{
			SessionID: string(termSess.ID()),
			TargetID:  termSess.TargetID,
			OwnerID:   termSess.UserID,
			Kind:      sharing.KindTerminal,
		}
		return meta, room, uid, true
	}
}

func invitationJoinURLForKind(kind sharing.Kind, sessionID, targetID, plainToken string) string {
	switch kind {
	case sharing.KindVNC:
		return invitationJoinURLVNC(sessionID, plainToken)
	case sharing.KindRDP:
		return invitationJoinURLRDP(sessionID, targetID, plainToken)
	default:
		return invitationJoinURL(sessionID, plainToken)
	}
}

func (a *App) ensureRoomForMeta(meta *sharingSessionMeta) *sharing.Room {
	if meta == nil {
		return nil
	}
	switch meta.Kind {
	case sharing.KindVNC:
		if a.VNCSessionManager == nil {
			return nil
		}
		vncSess, ok := a.VNCSessionManager.Get(session.ID(meta.SessionID))
		if !ok {
			return nil
		}
		return a.ensureVNCRoomFor(vncSess)
	case sharing.KindRDP:
		if a.RDPVNCManager == nil {
			return nil
		}
		rdpSess, ok := a.RDPVNCManager.GetSession(meta.SessionID)
		if !ok {
			return nil
		}
		return a.ensureRDPRoomFor(rdpSess)
	default:
		if a.TerminalSessionManager == nil {
			return nil
		}
		termSess, ok := a.TerminalSessionManager.Get(session.ID(meta.SessionID))
		if !ok {
			return nil
		}
		return a.ensureRoomFor(termSess)
	}
}

func (a *App) writeListParticipants(w http.ResponseWriter, meta *sharingSessionMeta, room *sharing.Room) {
	if meta == nil {
		writeJSON(w, map[string]interface{}{"items": []sharingResponseParticipant{}})
		return
	}
	if room == nil {
		room = a.ensureRoomForMeta(meta)
	}
	out := make([]sharingResponseParticipant, 0)
	if room != nil {
		writerID := room.WriterID()
		for _, p := range room.Participants() {
			out = append(out, sharingResponseParticipant{
				UserID:   p.UserID,
				Username: p.Username,
				Role:     string(p.Role),
				JoinedAt: p.JoinedAt.Unix(),
				IsWriter: p.UserID == writerID,
			})
		}
	}
	writerName := ""
	if room != nil {
		writerID := room.WriterID()
		for _, p := range room.Participants() {
			if p.UserID == writerID {
				writerName = p.Username
				break
			}
		}
		if writerName == "" && writerID != "" && a.UserStore != nil {
			writerName = a.usernameFor(context.Background(), writerID)
		}
	}
	writeJSON(w, map[string]interface{}{
		"items":            out,
		"writer_id":        roomWriterID(room),
		"writer_username":  writerName,
		"owner_id":         meta.OwnerID,
		"pending_requests": []sharingResponseWriteRequest{},
	})
}

func (a *App) handleKindListParticipants(w http.ResponseWriter, r *http.Request, kind sharing.Kind) {
	sessionID := chi.URLParam(r, "session_id")
	meta, room, _, ok := a.loadParticipatingSharingMeta(w, r, sessionID, kind)
	if !ok {
		return
	}
	if room == nil {
		room = a.ensureRoomForMeta(meta)
	}
	a.writeListParticipants(w, meta, room)
}

func (a *App) handleKindInvitationOptions(w http.ResponseWriter, r *http.Request, kind sharing.Kind) {
	sessionID := chi.URLParam(r, "session_id")
	meta, ok := a.loadOwnedSharingMeta(w, r, sessionID, kind)
	if !ok {
		return
	}
	tags, users, err := a.collectInvitationOptions(r.Context(), meta.OwnerID, meta.TargetID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	groups, err := a.collectInvitationGroups(r.Context(), meta.OwnerID, meta.TargetID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if tags == nil {
		tags = []invitationOptionsTag{}
	}
	if users == nil {
		users = []invitationOptionsMember{}
	}
	if groups == nil {
		groups = []invitationOptionsGroup{}
	}
	writeJSON(w, map[string]interface{}{
		"tags":   tags,
		"users":  users,
		"groups": groups,
	})
}

func (a *App) handleKindListInvitations(w http.ResponseWriter, r *http.Request, kind sharing.Kind) {
	if a.SharingStore == nil {
		writeJSON(w, map[string]interface{}{"items": []sharingResponseInvitation{}})
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	meta, ok := a.loadOwnedSharingMeta(w, r, sessionID, kind)
	if !ok {
		return
	}
	invs, err := a.SharingStore.ListBySession(r.Context(), meta.SessionID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]sharingResponseInvitation, 0, len(invs))
	for _, inv := range invs {
		if inv.SessionKind != kind {
			continue
		}
		inviteeName := ""
		if !inv.IsLink() {
			inviteeName = a.usernameFor(r.Context(), inv.InviteeUserID)
		}
		out = append(out, encodeInvitation(inv, inviteeName))
	}
	writeJSON(w, map[string]interface{}{"items": out})
}

func (a *App) handleKindRevokeInvitation(w http.ResponseWriter, r *http.Request, kind sharing.Kind) {
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	meta, ok := a.loadOwnedSharingMeta(w, r, sessionID, kind)
	if !ok {
		return
	}
	invID := chi.URLParam(r, "invitation_id")
	inv, _, err := a.SharingStore.GetByID(r.Context(), invID)
	if err != nil {
		if errors.Is(err, sharing.ErrInvitationNotFound) {
			writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if inv.SessionID != meta.SessionID || inv.SessionKind != kind {
		writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
		return
	}
	if err := a.SharingStore.MarkRevoked(r.Context(), invID, time.Now().UTC()); err != nil {
		writeInternalError(w, err)
		return
	}
	room, _ := a.SharingRegistry.Get(meta.SessionID)
	if room != nil {
		for _, uid := range room.ParticipantUserIDsForInvitation(invID) {
			a.disconnectSharingUser(session.ID(meta.SessionID), room, uid)
			a.publishSharingEventForSession(meta.SessionID, meta.OwnerID, sharing.EventParticipantLeft, uid, "", map[string]interface{}{
				"reason":        "invitation_revoked",
				"invitation_id": invID,
			})
		}
	}
	audit("session_invitation_revoked", auditFields{
		"user_id":    meta.OwnerID,
		"session_id": meta.SessionID,
		"inv_id":     invID,
	})
	a.publishSharingEventForSession(meta.SessionID, meta.OwnerID, sharing.EventInvitationRevoked, meta.OwnerID, "", map[string]interface{}{
		"invitation_id": invID,
	})
	if !inv.IsLink() && strings.TrimSpace(inv.InviteeUserID) != "" {
		a.publishInvitationEventToInvitee(inv.InviteeUserID, sharing.EventInvitationRevoked, inv)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleKindRegenerateInvitationJoinURL(w http.ResponseWriter, r *http.Request, kind sharing.Kind) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	meta, ok := a.loadOwnedSharingMeta(w, r, sessionID, kind)
	if !ok {
		return
	}
	invID := chi.URLParam(r, "invitation_id")
	inv, _, err := a.SharingStore.GetByID(r.Context(), invID)
	if err != nil {
		if errors.Is(err, sharing.ErrInvitationNotFound) {
			writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if inv.SessionID != meta.SessionID || inv.SessionKind != kind {
		writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
		return
	}
	now := time.Now().UTC()
	if !inv.Active(now) {
		writeJSONErrorKey(w, r, "sharing.invitationInactive", http.StatusForbidden)
		return
	}
	tok, err := sharing.GenerateToken()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := a.SharingStore.UpdateTokenHash(r.Context(), invID, tok.Hash); err != nil {
		writeInternalError(w, err)
		return
	}
	audit("session_invitation_link_regenerated", auditFields{
		"user_id":    meta.OwnerID,
		"session_id": meta.SessionID,
		"inv_id":     invID,
	})
	writeJSON(w, map[string]interface{}{
		"join_url": invitationJoinURLForKind(kind, meta.SessionID, meta.TargetID, tok.Plain),
		"token":    tok.Plain,
	})
}

func (a *App) handleKindKickParticipant(w http.ResponseWriter, r *http.Request, kind sharing.Kind) {
	sessionID := chi.URLParam(r, "session_id")
	meta, ok := a.loadOwnedSharingMeta(w, r, sessionID, kind)
	if !ok {
		return
	}
	target := chi.URLParam(r, "user_id")
	if strings.TrimSpace(target) == "" {
		writeJSONErrorKey(w, r, "sharing.userIDRequired", http.StatusBadRequest)
		return
	}
	if target == meta.OwnerID {
		writeJSONErrorKey(w, r, "sharing.cannotKickOwner", http.StatusBadRequest)
		return
	}
	room, _ := a.SharingRegistry.Get(meta.SessionID)
	if room == nil {
		writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusNotFound)
		return
	}
	if err := a.sharingService().KickParticipant(room, meta.SessionID, target); err != nil {
		if errors.Is(err, sharing.ErrParticipantMissing) {
			writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("session_participant_kicked", auditFields{
		"user_id":    meta.OwnerID,
		"target_id":  target,
		"session_id": meta.SessionID,
	})
	// Name the kicked user explicitly: they are already out of the room's
	// participant list, so they would otherwise never get this event.
	a.publishSharingEventForSession(meta.SessionID, meta.OwnerID, sharing.EventParticipantLeft, target, "", map[string]interface{}{
		"reason": "kicked",
	}, target)
	w.WriteHeader(http.StatusNoContent)
}

// Thin wrappers for VNC routes.
func (a *App) handleVNCInvitationOptions(w http.ResponseWriter, r *http.Request) {
	a.handleKindInvitationOptions(w, r, sharing.KindVNC)
}
func (a *App) handleVNCListInvitations(w http.ResponseWriter, r *http.Request) {
	a.handleKindListInvitations(w, r, sharing.KindVNC)
}
func (a *App) handleVNCRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	a.handleKindRevokeInvitation(w, r, sharing.KindVNC)
}
func (a *App) handleVNCRegenerateInvitationJoinURL(w http.ResponseWriter, r *http.Request) {
	a.handleKindRegenerateInvitationJoinURL(w, r, sharing.KindVNC)
}
func (a *App) handleVNCListParticipants(w http.ResponseWriter, r *http.Request) {
	a.handleKindListParticipants(w, r, sharing.KindVNC)
}

// Thin wrappers for RDP routes.
func (a *App) handleRDPInvitationOptions(w http.ResponseWriter, r *http.Request) {
	a.handleKindInvitationOptions(w, r, sharing.KindRDP)
}
func (a *App) handleRDPListInvitations(w http.ResponseWriter, r *http.Request) {
	a.handleKindListInvitations(w, r, sharing.KindRDP)
}
func (a *App) handleRDPRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	a.handleKindRevokeInvitation(w, r, sharing.KindRDP)
}
func (a *App) handleRDPRegenerateInvitationJoinURL(w http.ResponseWriter, r *http.Request) {
	a.handleKindRegenerateInvitationJoinURL(w, r, sharing.KindRDP)
}
func (a *App) handleRDPListParticipants(w http.ResponseWriter, r *http.Request) {
	a.handleKindListParticipants(w, r, sharing.KindRDP)
}
func (a *App) handleRDPKickParticipant(w http.ResponseWriter, r *http.Request) {
	a.handleKindKickParticipant(w, r, sharing.KindRDP)
}
