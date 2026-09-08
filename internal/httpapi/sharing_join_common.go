package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/sharing"
)

type joinInvitationBody struct {
	InvitationID    string `json:"invitation_id"`
	InvitationToken string `json:"invitation_token"`
}

func decodeJoinInvitationBody(r *http.Request) (joinInvitationBody, error) {
	var body joinInvitationBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return body, err
	}
	body.InvitationID = strings.TrimSpace(body.InvitationID)
	body.InvitationToken = strings.TrimSpace(body.InvitationToken)
	return body, nil
}

func (a *App) lookupJoinInvitation(ctx context.Context, body joinInvitationBody) (sharing.Invitation, error) {
	if body.InvitationToken != "" {
		return a.SharingStore.GetByTokenHash(ctx, sharing.HashToken(body.InvitationToken))
	}
	if body.InvitationID != "" {
		inv, _, err := a.SharingStore.GetByID(ctx, body.InvitationID)
		if err != nil {
			return sharing.Invitation{}, err
		}
		if inv.IsLink() {
			return sharing.Invitation{}, errSharingLinkTokenRequired
		}
		return inv, nil
	}
	return sharing.Invitation{}, errSharingTokenOrIDRequired
}

var (
	errSharingTokenOrIDRequired = errors.New("token or invitation id required")
	errSharingLinkTokenRequired = errors.New("link invitation requires token")
)

// validateJoinInvitation checks invitation state and ACL before join.
// Returns false if a response was written to w.
func (a *App) validateJoinInvitation(
	w http.ResponseWriter,
	r *http.Request,
	inv sharing.Invitation,
	userID, expectedSessionID string,
	expectedKind sharing.Kind,
) bool {
	if inv.SessionID != expectedSessionID {
		writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
		return false
	}
	if expectedKind != "" && inv.SessionKind != expectedKind {
		writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
		return false
	}
	now := time.Now().UTC()
	if !inv.Active(now) {
		writeJSONErrorKey(w, r, "sharing.invitationInactive", http.StatusForbidden)
		return false
	}
	if !inv.IsLink() && !strings.EqualFold(inv.InviteeUserID, userID) {
		writeJSONErrorKey(w, r, "sharing.invitationOtherUser", http.StatusForbidden)
		return false
	}
	if userID == inv.OwnerUserID {
		writeJSONErrorKey(w, r, "sharing.cannotInviteSelf", http.StatusBadRequest)
		return false
	}
	if ok, err := a.userCanAccessTarget(r.Context(), inv.OwnerUserID, access.TargetID(inv.TargetID)); err != nil {
		writeInternalError(w, err)
		return false
	} else if !ok {
		writeJSONErrorKey(w, r, "sharing.invitationStaleAccess", http.StatusForbidden)
		return false
	}
	if ok, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(inv.TargetID)); err != nil {
		writeInternalError(w, err)
		return false
	} else if !ok {
		writeJSONErrorKey(w, r, "sharing.inviteeNoTargetAccess", http.StatusForbidden)
		return false
	}
	return true
}

func (a *App) joinUsername(ctx context.Context, userID string) string {
	if a.UserStore == nil {
		return userID
	}
	if u, err := a.UserStore.GetByID(userID); err == nil && u != nil && u.Username != "" {
		return u.Username
	}
	return userID
}

// completeSharingJoin records use, adds the viewer to the room, and writes the JSON response.
func (a *App) completeSharingJoin(
	w http.ResponseWriter,
	r *http.Request,
	inv sharing.Invitation,
	userID, sessionID, ownerID string,
) {
	now := time.Now().UTC()
	username := a.joinUsername(r.Context(), userID)
	if _, err := a.sharingService().JoinRoom(r.Context(), inv, userID, username, now); err != nil {
		mapSharingJoinError(w, r, err)
		return
	}
	audit("session_invitation_consumed", auditFields{
		"user_id":    userID,
		"session_id": sessionID,
		"inv_id":     inv.ID,
		"is_link":    inv.IsLink(),
	})
	audit("session_participant_joined", auditFields{
		"user_id":    userID,
		"session_id": sessionID,
		"username":   username,
	})
	if !inv.IsLink() && strings.TrimSpace(inv.InviteeUserID) != "" {
		if fresh, _, err := a.SharingStore.GetByID(r.Context(), inv.ID); err == nil {
			a.publishInvitationEventToInvitee(inv.InviteeUserID, sharing.EventInvitationConsumed, fresh)
		}
	}
	a.publishSharingEventForSession(sessionID, ownerID, sharing.EventInvitationConsumed, userID, username, map[string]interface{}{
		"invitation_id": inv.ID,
	})
	a.publishSharingEventForSession(sessionID, ownerID, sharing.EventParticipantJoined, userID, username, nil)
	writeJSON(w, map[string]interface{}{
		"session_id": sessionID,
		"role":       string(sharing.RoleViewer),
	})
}

func buildInvitationJoinURL(path string, query url.Values) string {
	if len(query) == 0 {
		return path
	}
	return path + "?" + query.Encode()
}

func invitationJoinURL(sessionID, plainToken string) string {
	q := url.Values{}
	q.Set("session_id", sessionID)
	q.Set("mode", "viewer")
	q.Set("invite", plainToken)
	return buildInvitationJoinURL("/terminal", q)
}

func invitationJoinURLVNC(sessionID, plainToken string) string {
	q := url.Values{}
	q.Set("session_id", sessionID)
	q.Set("mode", "viewer")
	q.Set("invite", plainToken)
	return buildInvitationJoinURL("/vnc", q)
}

func invitationJoinURLRDP(sessionID, targetID, plainToken string) string {
	q := url.Values{}
	if targetID != "" {
		q.Set("target_id", targetID)
	}
	q.Set("session_id", sessionID)
	q.Set("mode", "viewer")
	q.Set("invite", plainToken)
	return buildInvitationJoinURL("/rdp", q)
}

// normaliseViewOnlyInviteMode restricts VNC/RDP invitations to viewer mode.
func normaliseViewOnlyInviteMode(raw string) (sharing.Mode, error) {
	if strings.TrimSpace(raw) == "" {
		return sharing.ModeViewer, nil
	}
	mode, err := sharing.NormaliseMode(raw)
	if err != nil {
		return "", err
	}
	if mode != sharing.ModeViewer {
		return "", errViewOnlyModeRequired
	}
	return mode, nil
}

var errViewOnlyModeRequired = errors.New("view-only sessions only support viewer invitations")

func mapSharingJoinError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sharing.ErrInvitationConsumed):
		writeJSONErrorKey(w, r, "sharing.invitationInactive", http.StatusForbidden)
	case errors.Is(err, sharing.ErrUserKicked):
		writeJSONErrorKey(w, r, "sharing.userKicked", http.StatusForbidden)
	case errors.Is(err, sharing.ErrInvitationNotFound):
		writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
	case errors.Is(err, sharing.ErrInviterNoTargetAccess):
		writeJSONErrorKey(w, r, "sharing.invitationStaleAccess", http.StatusForbidden)
	case errors.Is(err, sharing.ErrJoinerNoTargetAccess):
		writeJSONErrorKey(w, r, "sharing.inviteeNoTargetAccess", http.StatusForbidden)
	default:
		writeInternalError(w, err)
	}
}
