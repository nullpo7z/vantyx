package sharing

import (
	"context"
	"strings"
	"time"
)

// BridgeControl is the runtime hook used to disconnect participants and
// sync the write token. Implemented by terminal/VNC detachable bridges.
type BridgeControl interface {
	SetWriter(userID string)
	DetachUser(userID string)
}

// BridgeLookup resolves a live bridge for a session ID.
type BridgeLookup func(sessionID string) (BridgeControl, bool)

// AccessCheck verifies a user may access a target.
type AccessCheck func(ctx context.Context, userID, targetID string) (bool, error)

// Service holds shared join/kick logic for HTTP and CLI callers.
type Service struct {
	Store    Store
	Registry *Registry
	Bridges  BridgeLookup
	// Access, when set, re-checks target ACL at join time against both
	// the inviter (stale-access guard) and the joining user. It is the
	// single enforcement point of the "a valid invitation token is not
	// sufficient without target access" guarantee, so every caller
	// (HTTP and the CLI SSH gateway) inherits it. Leaving it nil skips
	// the check and must only be done in tests.
	Access AccessCheck
}

// JoinRoom validates invitation state, records use, and adds the user as viewer.
func (s *Service) JoinRoom(ctx context.Context, inv Invitation, userID, username string, now time.Time) (*Room, error) {
	if s == nil || s.Store == nil || s.Registry == nil {
		return nil, ErrRoomNotFound
	}
	if !inv.Active(now) {
		return nil, ErrInvitationConsumed
	}
	if !inv.IsLink() && !strings.EqualFold(inv.InviteeUserID, userID) {
		return nil, ErrInvitationNotFound
	}
	if userID == inv.OwnerUserID {
		return nil, ErrInvitationNotFound
	}
	// Re-check target ACL before consuming the invitation. Both the
	// inviter (guards against an owner who has since lost access) and the
	// joining user must independently be able to reach the target: a
	// shared token never grants access on its own. This runs here, in the
	// shared service, so the CLI join path cannot bypass it the way it
	// would if the check lived only in the HTTP handler.
	if s.Access != nil {
		if ok, err := s.Access(ctx, inv.OwnerUserID, inv.TargetID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInviterNoTargetAccess
		}
		if ok, err := s.Access(ctx, userID, inv.TargetID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrJoinerNoTargetAccess
		}
	}
	if err := s.Store.RecordUse(ctx, inv.ID, userID, now); err != nil {
		return nil, err
	}
	room := s.Registry.EnsureRoom(inv.SessionID, inv.TargetID, inv.OwnerUserID, inv.OwnerUserID)
	if room.IsKicked(userID) {
		return nil, ErrUserKicked
	}
	if err := room.AddViewer(userID, username, inv.ID, now); err != nil {
		return nil, err
	}
	return room, nil
}

// KickParticipant removes userID from the room and detaches their bridge connection.
func (s *Service) KickParticipant(room *Room, sessionID, userID string) error {
	if room == nil {
		return ErrRoomNotFound
	}
	if err := room.RemoveParticipant(userID); err != nil {
		return err
	}
	if s.Bridges != nil {
		if ctrl, ok := s.Bridges(sessionID); ok {
			ctrl.DetachUser(userID)
			ctrl.SetWriter(room.WriterID())
		}
	}
	return nil
}

// DisconnectInvitationConsumers removes participants who joined via invitationID.
func (s *Service) DisconnectInvitationConsumers(room *Room, sessionID, invitationID string) []string {
	if room == nil || invitationID == "" {
		return nil
	}
	userIDs := room.ParticipantUserIDsForInvitation(invitationID)
	for _, uid := range userIDs {
		_ = s.KickParticipant(room, sessionID, uid)
	}
	return userIDs
}
