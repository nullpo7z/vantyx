package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
	"github.com/nullpo7z/vantyx/internal/vncproxy"
)

func (a *App) loadOwnedRDPSession(w http.ResponseWriter, r *http.Request, sessionID string) (*rdpvnc.Session, string, bool) {
	if a.RDPVNCManager == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return nil, "", false
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return nil, "", false
	}
	rdpSess, ok := a.RDPVNCManager.GetSession(sessionID)
	if !ok || rdpSess.UserID != userID {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return nil, "", false
	}
	canAccess, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(rdpSess.TargetID))
	if err != nil {
		writeInternalError(w, err)
		return nil, "", false
	}
	if !canAccess {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return nil, "", false
	}
	return rdpSess, userID, true
}

func (a *App) ensureRDPRoomFor(rdpSess *rdpvnc.Session) *sharing.Room {
	if a.SharingRegistry == nil {
		return nil
	}
	ownerName := rdpSess.UserID
	if a.UserStore != nil {
		if u, err := a.UserStore.GetByID(rdpSess.UserID); err == nil && u != nil && u.Username != "" {
			ownerName = u.Username
		}
	}
	return a.SharingRegistry.EnsureRoom(rdpSess.ID, rdpSess.TargetID, rdpSess.UserID, ownerName)
}

func (a *App) startRDPBridgeProxyIfNeeded(rdpSess *rdpvnc.Session) {
	if rdpSess == nil || rdpSess.Bridge == nil || rdpSess.AttachCh == nil {
		return
	}
	targetAddr := fmt.Sprintf("127.0.0.1:%d", rdpSess.Bridge.VNCPort())
	go a.runRDPDetachableVNCBridge(rdpSess, targetAddr)
}

func (a *App) runRDPDetachableVNCBridge(rdpSess *rdpvnc.Session, targetAddr string) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-rdpSess.Bridge.Done()
		cancel()
	}()
	touch := func() { a.RDPVNCManager.Touch(rdpSess.ID) }
	if a.SharingRegistry != nil {
		a.ensureRDPRoomFor(rdpSess)
		defer a.SharingRegistry.Remove(rdpSess.ID)
	}
	var sink vncproxy.BridgeControlSink
	if a.SharingBridges != nil {
		defer a.SharingBridges.Unregister(session.ID(rdpSess.ID))
		sink = vncBridgeSink{id: session.ID(rdpSess.ID), br: a.SharingBridges}
	}
	_ = vncproxy.RunBridgeDetachable(ctx, targetAddr, rdpSess.AttachCh, nil, touch, sink)
}

func (a *App) createRDPInvitationRecord(ctx context.Context, rdpSess *rdpvnc.Session, ownerID, invitee, groupID, inviteTag string, mode sharing.Mode, ttl time.Duration, linkMaxUses *int) (sharing.Invitation, sharing.Token, error) {
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
		SessionID:     rdpSess.ID,
		SessionKind:   sharing.KindRDP,
		TargetID:      rdpSess.TargetID,
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

func (a *App) handleRDPCreateInvitation(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	rdpSess, ownerID, ok := a.loadOwnedRDPSession(w, r, sessionID)
	if !ok {
		return
	}
	meta := &sharingSessionMeta{
		SessionID: rdpSess.ID,
		TargetID:  rdpSess.TargetID,
		OwnerID:   ownerID,
		Kind:      sharing.KindRDP,
	}
	a.handleSharingCreateInvitation(w, r, meta, func(ctx context.Context, owner, invitee, groupID, inviteTag string, mode sharing.Mode, ttl time.Duration, linkMaxUses *int) (sharing.Invitation, sharing.Token, error) {
		return a.createRDPInvitationRecord(ctx, rdpSess, owner, invitee, groupID, inviteTag, mode, ttl, linkMaxUses)
	})
}

