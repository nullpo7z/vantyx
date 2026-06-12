package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
	"github.com/nullpo7z/vantyx/internal/vncproxy"
)

// VNCSessionItem is one entry in GET /api/vnc/sessions response.
type VNCSessionItem struct {
	SessionID   string    `json:"session_id"`
	TargetID    string    `json:"target_id"`
	TargetName  string    `json:"target_name"`
	TargetPath  string    `json:"target_path,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
	Idle        bool      `json:"idle"`
	IdleSeconds int       `json:"idle_seconds,omitempty"`
}

func vncSessionItemFrom(s *session.Session, mgr *session.Manager) VNCSessionItem {
	item := VNCSessionItem{
		SessionID:  string(s.ID()),
		TargetID:   s.TargetID,
		TargetName: s.TargetName,
		CreatedAt:  s.CreatedAt(),
		LastSeen:   s.LastSeen(),
	}
	if mgr != nil && mgr.IsIdle(s) {
		item.Idle = true
		item.IdleSeconds = int(mgr.IdleDuration(s).Seconds())
	}
	return item
}

// handleVNCSessions returns active direct-VNC sessions for the current user.
func (a *App) handleVNCSessions(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.VNCSessionManager == nil {
		writeJSON(w, map[string]interface{}{"items": []VNCSessionItem{}})
		return
	}
	ctx := r.Context()
	allowed, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowedSet := make(map[access.TargetID]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	active := a.VNCSessionManager.ActiveSessionsForUser(userID)
	items := make([]VNCSessionItem, 0, len(active))
	for _, s := range active {
		if _, ok := allowedSet[access.TargetID(s.TargetID)]; !ok {
			continue
		}
		target, err := a.TargetStore.Get(ctx, access.TargetID(s.TargetID))
		if err != nil {
			continue
		}
		if target.Protocol != access.ProtocolVNC {
			continue
		}
		item := vncSessionItemFrom(s, a.VNCSessionManager)
		item.TargetName = target.Name
		item.TargetPath = target.Path
		items = append(items, item)
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

func (a *App) loadOwnedVNCSession(w http.ResponseWriter, r *http.Request, sessionID string) (*session.Session, string, bool) {
	if a.VNCSessionManager == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return nil, "", false
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return nil, "", false
	}
	vncSess, ok := a.VNCSessionManager.Get(session.ID(sessionID))
	if !ok || vncSess.UserID != userID {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return nil, "", false
	}
	canAccess, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(vncSess.TargetID))
	if err != nil {
		writeInternalError(w, err)
		return nil, "", false
	}
	if !canAccess {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return nil, "", false
	}
	return vncSess, userID, true
}

func (a *App) ensureVNCRoomFor(vncSess *session.Session) *sharing.Room {
	if a.SharingRegistry == nil {
		return nil
	}
	ownerName := vncSess.UserID
	if a.UserStore != nil {
		if u, err := a.UserStore.GetByID(vncSess.UserID); err == nil && u != nil && u.Username != "" {
			ownerName = u.Username
		}
	}
	return a.SharingRegistry.EnsureRoom(string(vncSess.ID()), vncSess.TargetID, vncSess.UserID, ownerName)
}

func (a *App) createVNCInvitationRecord(ctx context.Context, vncSess *session.Session, ownerID, invitee, groupID, inviteTag string, mode sharing.Mode, ttl time.Duration, linkMaxUses *int) (sharing.Invitation, sharing.Token, error) {
	tok, err := sharing.GenerateToken()
	if err != nil {
		return sharing.Invitation{}, sharing.Token{}, err
	}
	now := time.Now().UTC()
	id, err := newSharingID()
	if err != nil {
		return sharing.Invitation{}, sharing.Token{}, err
	}
	inv := sharing.Invitation{
		ID:            id,
		SessionID:     string(vncSess.ID()),
		SessionKind:   sharing.KindVNC,
		TargetID:      vncSess.TargetID,
		OwnerUserID:   ownerID,
		InviteeUserID: invitee,
		InviteGroupID: groupID,
		InviteTag:     inviteTag,
		Mode:          mode,
		MaxUses:       linkMaxUses,
		ExpiresAt:     now.Add(ttl),
		CreatedAt:     now,
	}
	if err := a.SharingStore.Create(ctx, inv, tok.Hash); err != nil {
		return sharing.Invitation{}, sharing.Token{}, err
	}
	return inv, tok, nil
}

func (a *App) runDetachableVNCBridge(ctx context.Context, vncSess *session.Session, id session.ID, targetAddr string, initial session.AttachReq) {
	touch := func() { a.VNCSessionManager.Touch(id) }
	if a.SharingRegistry != nil {
		a.ensureVNCRoomFor(vncSess)
		defer a.SharingRegistry.Remove(string(id))
	}
	var sink vncproxy.BridgeControlSink
	if a.SharingBridges != nil {
		defer a.SharingBridges.Unregister(id)
		sink = vncBridgeSink{id: id, br: a.SharingBridges}
	}
	_ = vncproxy.RunBridgeDetachable(ctx, targetAddr, vncSess.AttachCh, initial, touch, sink)
}

// handleVNCCreateInvitation issues a viewer invitation for a VNC session.
func (a *App) handleVNCCreateInvitation(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	vncSess, ownerID, ok := a.loadOwnedVNCSession(w, r, sessionID)
	if !ok {
		return
	}
	meta := &sharingSessionMeta{
		SessionID: string(vncSess.ID()),
		TargetID:  vncSess.TargetID,
		OwnerID:   ownerID,
		Kind:      sharing.KindVNC,
	}
	a.handleSharingCreateInvitation(w, r, meta, func(ctx context.Context, owner, invitee, groupID, inviteTag string, mode sharing.Mode, ttl time.Duration, linkMaxUses *int) (sharing.Invitation, sharing.Token, error) {
		return a.createVNCInvitationRecord(ctx, vncSess, owner, invitee, groupID, inviteTag, mode, ttl, linkMaxUses)
	})
}

func (a *App) sharingService() *sharing.Service {
	return &sharing.Service{
		Store:    a.SharingStore,
		Registry: a.SharingRegistry,
		Bridges: func(sessionID string) (sharing.BridgeControl, bool) {
			if a.SharingBridges == nil {
				return nil, false
			}
			c, ok := a.SharingBridges.Get(session.ID(sessionID))
			return c, ok
		},
	}
}

