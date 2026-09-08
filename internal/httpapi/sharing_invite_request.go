package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/sharing"
)

type sharingInviteCreator func(ctx context.Context, ownerID, invitee, groupID, inviteTag string, mode sharing.Mode, ttl time.Duration, linkMaxUses *int) (sharing.Invitation, sharing.Token, error)

type createInvitationRequest struct {
	Mode          string `json:"mode"`
	InviteeUserID string `json:"invitee_user_id"`
	InviteTag     string `json:"invite_tag"`
	InviteGroupID string `json:"invite_group_id"`
	TTLSeconds    int    `json:"ttl_seconds"`
	LinkMaxUses   *int   `json:"link_max_uses"`
	LinkUnlimited bool   `json:"link_unlimited"`
}

func decodeCreateInvitationRequest(r *http.Request) (createInvitationRequest, error) {
	var req createInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return req, err
	}
	return req, nil
}

func (a *App) handleSharingCreateInvitation(w http.ResponseWriter, r *http.Request, meta *sharingSessionMeta, creator sharingInviteCreator) {
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	if meta == nil {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	if sharingRecordingRequired(w, r) {
		return
	}
	req, err := decodeCreateInvitationRequest(r)
	if err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	mode, err := normaliseViewOnlyInviteMode(req.Mode)
	if err != nil {
		writeJSONErrorKey(w, r, "sharing.modeInvalid", http.StatusBadRequest)
		return
	}
	ttl := invitationDefaultTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if max := invitationMaxTTL(); ttl > max {
		ttl = max
	}
	invitee := strings.TrimSpace(req.InviteeUserID)
	inviteTag := strings.TrimSpace(req.InviteTag)
	inviteGroup := strings.TrimSpace(req.InviteGroupID)
	if invitee != "" && inviteTag != "" {
		writeJSONErrorKey(w, r, "sharing.inviteeOrTagOnly", http.StatusBadRequest)
		return
	}
	if invitee != "" && inviteGroup != "" {
		writeJSONErrorKey(w, r, "sharing.inviteeOrGroupOnly", http.StatusBadRequest)
		return
	}
	if inviteTag != "" && inviteGroup != "" {
		writeJSONErrorKey(w, r, "sharing.tagOrGroupOnly", http.StatusBadRequest)
		return
	}
	if inviteGroup != "" {
		a.handleSharingCreateGroupInvitations(w, r, meta, inviteGroup, mode, ttl, creator)
		return
	}
	if inviteTag != "" {
		a.handleSharingCreateTagInvitations(w, r, meta, inviteTag, mode, ttl, creator)
		return
	}
	var linkMaxUses *int
	if invitee == "" {
		linkMaxUses, err = parseLinkMaxUses(req.LinkUnlimited, req.LinkMaxUses)
		if err != nil {
			writeJSONErrorKey(w, r, "sharing.linkMaxUsesInvalid", http.StatusBadRequest)
			return
		}
	} else {
		if invitee == meta.OwnerID {
			writeJSONErrorKey(w, r, "sharing.cannotInviteSelf", http.StatusBadRequest)
			return
		}
		if a.UserStore != nil {
			if u, err := a.UserStore.GetByID(invitee); err != nil || u == nil {
				writeJSONErrorKey(w, r, "sharing.inviteeNotFound", http.StatusBadRequest)
				return
			}
		}
		ok, err := a.userCanAccessTarget(r.Context(), invitee, access.TargetID(meta.TargetID))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if !ok {
			writeJSONErrorKey(w, r, "sharing.inviteeNoTargetAccess", http.StatusBadRequest)
			return
		}
	}
	inv, tok, err := creator(r.Context(), meta.OwnerID, invitee, "", "", mode, ttl, linkMaxUses)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	room := a.ensureRoomForMeta(meta)
	// A named re-invitation from the owner lifts a previous kick (E-16);
	// links/tags/groups leave the block in place.
	if invitee != "" && room != nil && room.Unkick(invitee) {
		audit("session_participant_unkicked", auditFields{
			"user_id":    meta.OwnerID,
			"session_id": meta.SessionID,
			"target_id":  invitee,
			"inv_id":     inv.ID,
		})
	}
	// Mirror the terminal path (sharing.go handleCreateInvitation) so
	// VNC/RDP invitations are auditable too: who invited whom, to which
	// session/target, and whether it was a shareable link.
	audit("session_invitation_created", auditFields{
		"user_id":    meta.OwnerID,
		"session_id": meta.SessionID,
		"target_id":  meta.TargetID,
		"kind":       string(meta.Kind),
		"inv_id":     inv.ID,
		"mode":       string(inv.Mode),
		"is_link":    invitee == "",
		"invitee":    invitee,
	})
	if invitee != "" {
		a.publishInvitationEventToInvitee(invitee, sharing.EventInvitationReceived, inv)
	}
	a.publishSharingEventForSession(meta.SessionID, meta.OwnerID, sharing.EventInvitationCreated, meta.OwnerID, "", map[string]interface{}{
		"invitation_id": inv.ID,
	})
	out := encodeInvitation(inv, a.usernameFor(r.Context(), inv.InviteeUserID))
	out.Token = tok.Plain
	out.JoinURL = invitationJoinURLForKind(meta.Kind, meta.SessionID, meta.TargetID, tok.Plain)
	writeJSON(w, out)
}

func (a *App) handleSharingCreateTagInvitations(w http.ResponseWriter, r *http.Request, meta *sharingSessionMeta, tag string, mode sharing.Mode, ttl time.Duration, creator sharingInviteCreator) {
	memberIDs, err := a.invitableUserIDsForTag(r.Context(), meta.TargetID, tag)
	if err != nil {
		if errors.Is(err, errInviteTagInvalid) {
			writeJSONErrorKey(w, r, "sharing.inviteTagInvalid", http.StatusBadRequest)
			return
		}
		if errors.Is(err, errInviteTagNotForTarget) {
			writeJSONErrorKey(w, r, "sharing.inviteTagNotForTarget", http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	if len(memberIDs) == 0 {
		writeJSON(w, map[string]interface{}{"items": []sharingResponseInvitation{}, "created": 0})
		return
	}
	a.ensureRoomForMeta(meta)
	items := make([]sharingResponseInvitation, 0, len(memberIDs))
	for _, uid := range memberIDs {
		if uid == meta.OwnerID {
			continue
		}
		inv, _, err := creator(r.Context(), meta.OwnerID, uid, "", tag, mode, ttl, nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		audit("session_invitation_created", auditFields{
			"user_id":    meta.OwnerID,
			"session_id": meta.SessionID,
			"target_id":  meta.TargetID,
			"kind":       string(meta.Kind),
			"inv_id":     inv.ID,
			"mode":       string(inv.Mode),
			"is_link":    false,
			"invitee":    uid,
			"tag":        tag,
		})
		a.publishInvitationEventToInvitee(uid, sharing.EventInvitationReceived, inv)
		items = append(items, encodeInvitation(inv, a.usernameFor(r.Context(), uid)))
	}
	a.publishSharingEventForSession(meta.SessionID, meta.OwnerID, sharing.EventInvitationCreated, meta.OwnerID, "", map[string]interface{}{
		"created": len(items),
	})
	writeJSON(w, map[string]interface{}{"items": items, "created": len(items)})
}

func (a *App) handleSharingCreateGroupInvitations(w http.ResponseWriter, r *http.Request, meta *sharingSessionMeta, groupID string, mode sharing.Mode, ttl time.Duration, creator sharingInviteCreator) {
	memberIDs, err := a.invitableUserIDsForGroup(r.Context(), meta.OwnerID, meta.TargetID, groupID)
	if err != nil {
		if errors.Is(err, errInviteGroupInvalid) {
			writeJSONErrorKey(w, r, "sharing.inviteGroupInvalid", http.StatusBadRequest)
			return
		}
		if errors.Is(err, errInviteGroupNotForTarget) {
			writeJSONErrorKey(w, r, "sharing.inviteGroupNotForTarget", http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	if len(memberIDs) == 0 {
		writeJSON(w, map[string]interface{}{"items": []sharingResponseInvitation{}, "created": 0})
		return
	}
	a.ensureRoomForMeta(meta)
	items := make([]sharingResponseInvitation, 0, len(memberIDs))
	for _, uid := range memberIDs {
		if uid == meta.OwnerID {
			continue
		}
		inv, _, err := creator(r.Context(), meta.OwnerID, uid, groupID, "", mode, ttl, nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		audit("session_invitation_created", auditFields{
			"user_id":    meta.OwnerID,
			"session_id": meta.SessionID,
			"target_id":  meta.TargetID,
			"kind":       string(meta.Kind),
			"inv_id":     inv.ID,
			"mode":       string(inv.Mode),
			"is_link":    false,
			"invitee":    uid,
			"group":      groupID,
		})
		a.publishInvitationEventToInvitee(uid, sharing.EventInvitationReceived, inv)
		items = append(items, encodeInvitation(inv, a.usernameFor(r.Context(), uid)))
	}
	a.publishSharingEventForSession(meta.SessionID, meta.OwnerID, sharing.EventInvitationCreated, meta.OwnerID, "", map[string]interface{}{
		"created": len(items),
	})
	writeJSON(w, map[string]interface{}{"items": items, "created": len(items)})
}
