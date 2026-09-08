package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/session"
)

// handleDeleteUser removes a user account. Admin only.
//
// Guard rails: an admin cannot delete their own account (that would
// orphan the request mid-flight and is almost always a mistake), and
// the last remaining admin cannot be deleted (there would be no one
// left who can manage the instance).
//
// Dependent rows (group membership, tags, login sessions, SSH public
// keys, file transfer jobs) are removed by ON DELETE CASCADE. Anything
// without a foreign key is cleaned up here: pending invitations the user
// issued or received are revoked, and their live terminal/VNC sessions
// are stopped so a deleted account cannot keep an open bridge.
func (a *App) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	targetUserID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if targetUserID == "" {
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
		return
	}
	actorID := strings.TrimSpace(a.currentUserID(r))
	if targetUserID == actorID {
		writeJSONErrorKey(w, r, "users.cannotDeleteSelf", http.StatusBadRequest)
		return
	}
	target, err := a.UserStore.GetByID(targetUserID)
	if err != nil || target == nil {
		writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
		return
	}
	if target.Role == auth.RoleAdmin {
		remaining, err := a.countAdmins()
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if remaining <= 1 {
			writeJSONErrorKey(w, r, "users.lastAdmin", http.StatusConflict)
			return
		}
	}

	// Stop live sessions first so the bridges don't outlive the account.
	stopped := a.stopSessionsOwnedBy(targetUserID)

	if err := a.UserStore.DeleteUser(targetUserID); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}

	revoked := a.revokeInvitationsForUser(r.Context(), targetUserID)

	audit("user_delete", auditFields{
		"user_id":             actorID,
		"deleted_user_id":     targetUserID,
		"deleted_username":    target.Username,
		"deleted_role":        target.Role,
		"sessions_stopped":    stopped,
		"invitations_revoked": revoked,
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
	w.WriteHeader(http.StatusNoContent)
}

// countAdmins returns how many admin accounts exist.
func (a *App) countAdmins() (int, error) {
	users, err := a.UserStore.ListUsers(1000, 0)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		if u != nil && u.Role == auth.RoleAdmin && !u.Disabled {
			n++
		}
	}
	return n, nil
}

// stopSessionsOwnedBy stops every live terminal / VNC session owned by
// userID and returns how many were stopped. Sessions are looked up
// through the same managers the session-list endpoints use.
// CLISessionCloser is implemented by the SSH CLI gateway so that
// disabling or deleting a user also drops their live CLI connections.
type CLISessionCloser interface {
	CloseConnectionsForUser(userID string) int
}

func (a *App) stopSessionsOwnedBy(userID string) int {
	n := 0
	if a.CLISessionCloser != nil {
		n += a.CLISessionCloser.CloseConnectionsForUser(userID)
	}
	if mgr, ok := a.TerminalSessionManager.(*session.Manager); ok && mgr != nil {
		for _, s := range mgr.ActiveSessionsForUser(userID) {
			if a.SharingRegistry != nil {
				a.SharingRegistry.Remove(string(s.ID()))
			}
			mgr.Stop(s.ID())
			n++
		}
	}
	if a.VNCSessionManager != nil {
		for _, s := range a.VNCSessionManager.ActiveSessionsForUser(userID) {
			if a.SharingRegistry != nil {
				a.SharingRegistry.Remove(string(s.ID()))
			}
			a.VNCSessionManager.Stop(s.ID())
			n++
		}
	}
	return n
}

// revokeInvitationsForUser revokes pending session invitations the user
// issued or was named on. session_invitations has no foreign key to
// users, so this is not covered by the cascade. Returns the number of
// rows revoked (0 when there is no DB).
func (a *App) revokeInvitationsForUser(ctx context.Context, userID string) int64 {
	if a.DB == nil {
		return 0
	}
	res, err := a.DB.ExecContext(ctx, `
		UPDATE session_invitations
		SET revoked_at = ?
		WHERE revoked_at IS NULL AND (owner_user_id = ? OR invitee_user_id = ?)
	`, time.Now().UTC().Unix(), userID, userID)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}
