package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
)

// invitationDefaultTTL is the fallback validity window when a request
// omits ttl_seconds. Short by design so a leaked URL has a small
// blast radius (ASVS V2.7 / CWE-613).
const invitationDefaultTTL = 15 * time.Minute

// invitationMaxTTL caps the TTL an owner can request. Operators can
// override via VANTYX_INVITATION_MAX_TTL_SECONDS.
const invitationMaxTTLDefault = 4 * time.Hour

func invitationMaxTTL() time.Duration {
	v := strings.TrimSpace(getEnv("VANTYX_INVITATION_MAX_TTL_SECONDS"))
	if v == "" {
		return invitationMaxTTLDefault
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return invitationMaxTTLDefault
	}
	return time.Duration(n) * time.Second
}

// getEnv is wrapped in a small indirection so tests can override the
// invitation TTL ceiling.
func getEnv(k string) string {
	return strings.TrimSpace(envLookup(k))
}

// sharingResponseInvitation is the JSON shape for an invitation row.
// The plain token is only ever returned in [createSharingInvitation]'s
// response body; subsequent reads expose just the metadata.
type sharingResponseInvitation struct {
	ID              string `json:"id"`
	Mode            string `json:"mode"`
	OwnerUserID     string `json:"owner_user_id"`
	InviteeUserID   string `json:"invitee_user_id,omitempty"`
	InviteeUsername string `json:"invitee_username,omitempty"`
	InviteGroupID   string `json:"invite_group_id,omitempty"` // legacy
	InviteTag       string `json:"invite_tag,omitempty"`
	IsLink          bool   `json:"is_link"`
	LinkUnlimited   bool   `json:"link_unlimited,omitempty"`
	MaxUses         *int   `json:"max_uses,omitempty"`
	UseCount        int    `json:"use_count,omitempty"`
	ExpiresAt       int64  `json:"expires_at"`
	CreatedAt       int64  `json:"created_at"`
	UsedAt          int64  `json:"used_at,omitempty"`
	RevokedAt       int64  `json:"revoked_at,omitempty"`
	Token           string `json:"token,omitempty"` // only populated on POST response
	JoinURL         string `json:"join_url,omitempty"`
}

// sharingResponseParticipant is the JSON shape for participant lists.
type sharingResponseParticipant struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	JoinedAt int64  `json:"joined_at"`
	IsWriter bool   `json:"is_writer"`
}

// sharingResponseWriteRequest is the JSON shape for write requests.
type sharingResponseWriteRequest struct {
	ID            string `json:"id"`
	RequesterID   string `json:"requester_id"`
	RequesterName string `json:"requester_name"`
	Status        string `json:"status"`
	RequestedAt   int64  `json:"requested_at"`
	DecidedAt     int64  `json:"decided_at,omitempty"`
	DecidedBy     string `json:"decided_by,omitempty"`
}

// loadOwnedTerminalSession returns the session if it exists, the
// caller is the owner, and they still have access to the target.
// Otherwise it writes the appropriate response and returns false.
func (a *App) loadOwnedTerminalSession(w http.ResponseWriter, r *http.Request, sessionID string) (*session.Session, string, bool) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return nil, "", false
	}
	if a.TerminalSessionManager == nil {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return nil, "", false
	}
	termSess, ok := a.TerminalSessionManager.Get(session.ID(sessionID))
	if !ok || termSess.UserID != userID {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return nil, "", false
	}
	canAccess, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(termSess.TargetID))
	if err != nil {
		writeInternalError(w, err)
		return nil, "", false
	}
	if !canAccess {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return nil, "", false
	}
	return termSess, userID, true
}

// loadParticipatingTerminalSession returns the session if the caller
// is either the owner or a registered participant via the sharing
// registry. Used by participant-list / write-request endpoints that
// any room member may call.
func (a *App) loadParticipatingTerminalSession(w http.ResponseWriter, r *http.Request, sessionID string) (*session.Session, *sharing.Room, string, bool) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return nil, nil, "", false
	}
	if a.TerminalSessionManager == nil {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return nil, nil, "", false
	}
	termSess, ok := a.TerminalSessionManager.Get(session.ID(sessionID))
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return nil, nil, "", false
	}
	room, _ := a.SharingRegistry.Get(string(termSess.ID()))
	if termSess.UserID != userID && (room == nil || !room.IsParticipant(userID)) {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return nil, nil, "", false
	}
	return termSess, room, userID, true
}

func (a *App) ensureRoomFor(termSess *session.Session) *sharing.Room {
	if a.SharingRegistry == nil {
		return nil
	}
	owner := termSess.UserID
	ownerName := owner
	if a.UserStore != nil {
		if u, err := a.UserStore.GetByID(owner); err == nil && u != nil && u.Username != "" {
			ownerName = u.Username
		}
	}
	return a.SharingRegistry.EnsureRoom(string(termSess.ID()), termSess.TargetID, owner, ownerName)
}

// handleCreateInvitation issues a new invitation. Body fields:
//   - mode: "viewer" | "writer_eligible" (default viewer)
//   - invitee_user_id: named invitation (select one user)
//   - invite_tag: create one named invite per user with the tag who can access the target
//   - link_unlimited + link_max_uses: link invitation usage cap
//     (single = 1, limited = N>1, unlimited = link_unlimited true)
//   - ttl_seconds: optional; must fit the configured ceiling
func (a *App) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	termSess, ownerID, ok := a.loadOwnedTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	var req struct {
		Mode          string `json:"mode"`
		InviteeUserID string `json:"invitee_user_id"`
		InviteTag     string `json:"invite_tag"`
		TTLSeconds    int    `json:"ttl_seconds"`
		LinkMaxUses   *int   `json:"link_max_uses"`
		LinkUnlimited bool   `json:"link_unlimited"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	mode, err := sharing.NormaliseMode(req.Mode)
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
	if invitee != "" && inviteTag != "" {
		writeJSONErrorKey(w, r, "sharing.inviteeOrTagOnly", http.StatusBadRequest)
		return
	}
	if inviteTag != "" {
		a.handleCreateTagInvitations(w, r, termSess, ownerID, inviteTag, mode, ttl)
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
		if u, err := a.UserStore.GetByID(invitee); err != nil || u == nil {
			writeJSONErrorKey(w, r, "sharing.inviteeNotFound", http.StatusBadRequest)
			return
		}
		if invitee == ownerID {
			writeJSONErrorKey(w, r, "sharing.cannotInviteSelf", http.StatusBadRequest)
			return
		}
		ok, err := a.userCanAccessTarget(r.Context(), invitee, access.TargetID(termSess.TargetID))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if !ok {
			writeJSONErrorKey(w, r, "sharing.inviteeNoTargetAccess", http.StatusBadRequest)
			return
		}
	}
	inv, tok, err := a.createInvitationRecord(r.Context(), termSess, ownerID, invitee, "", "", mode, ttl, linkMaxUses)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	a.ensureRoomFor(termSess)
	audit("session_invitation_created", auditFields{
		"user_id":    ownerID,
		"session_id": string(termSess.ID()),
		"inv_id":     inv.ID,
		"mode":       string(inv.Mode),
		"is_link":    invitee == "",
		"invitee":    invitee,
	})
	if invitee != "" {
		a.publishInvitationEventToInvitee(invitee, sharing.EventInvitationReceived, inv)
	}
	a.publishSharingEvent(termSess, sharing.EventInvitationCreated, ownerID, "", map[string]interface{}{
		"invitation_id": inv.ID,
	})
	out := encodeInvitation(inv, a.usernameFor(r.Context(), inv.InviteeUserID))
	out.Token = tok.Plain
	out.JoinURL = invitationJoinURL(sessionID, tok.Plain)
	writeJSON(w, out)
}

func (a *App) handleCreateTagInvitations(w http.ResponseWriter, r *http.Request, termSess *session.Session, ownerID, tag string, mode sharing.Mode, ttl time.Duration) {
	memberIDs, err := a.invitableUserIDsForTag(r.Context(), termSess.TargetID, tag)
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
	a.ensureRoomFor(termSess)
	items := make([]sharingResponseInvitation, 0, len(memberIDs))
	for _, uid := range memberIDs {
		if uid == ownerID {
			continue
		}
		inv, _, err := a.createInvitationRecord(r.Context(), termSess, ownerID, uid, "", tag, mode, ttl, nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		audit("session_invitation_created", auditFields{
			"user_id":    ownerID,
			"session_id": string(termSess.ID()),
			"inv_id":     inv.ID,
			"mode":       string(inv.Mode),
			"is_link":    false,
			"invitee":    uid,
			"tag":        tag,
		})
		a.publishInvitationEventToInvitee(uid, sharing.EventInvitationReceived, inv)
		out := encodeInvitation(inv, a.usernameFor(r.Context(), uid))
		items = append(items, out)
	}
	a.publishSharingEvent(termSess, sharing.EventInvitationCreated, ownerID, "", map[string]interface{}{
		"created": len(items),
	})
	writeJSON(w, map[string]interface{}{"items": items, "created": len(items)})
}

var (
	errInviteTagInvalid      = errors.New("invite tag invalid")
	errInviteTagNotForTarget = errors.New("tag does not grant access to target")
)

func parseLinkMaxUses(linkUnlimited bool, maxUses *int) (*int, error) {
	if linkUnlimited {
		return nil, nil
	}
	if maxUses == nil {
		one := 1
		return &one, nil
	}
	if *maxUses < 1 {
		return nil, errors.New("invalid max uses")
	}
	return maxUses, nil
}

func (a *App) createInvitationRecord(ctx context.Context, termSess *session.Session, ownerID, invitee, groupID, inviteTag string, mode sharing.Mode, ttl time.Duration, linkMaxUses *int) (sharing.Invitation, sharing.Token, error) {
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
		SessionID:     string(termSess.ID()),
		SessionKind:   sharing.KindTerminal,
		TargetID:      termSess.TargetID,
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

// invitableUserIDsForTarget returns user IDs that may access targetID
// (same rules as TargetIDsForUser, inverted).
func (a *App) invitableUserIDsForTarget(ctx context.Context, targetID string) ([]string, error) {
	if a.AccessGroupStore == nil || targetID == "" {
		return nil, nil
	}
	ids, err := a.AccessGroupStore.UserIDsForTarget(ctx, access.TargetID(targetID), &access.ListOpts{Limit: 1000})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, uid := range ids {
		out = append(out, string(uid))
	}
	return out, nil
}

// invitableUserIDsForTag returns users who have tag and can access targetID.
func (a *App) invitableUserIDsForTag(ctx context.Context, targetID, tag string) ([]string, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, errInviteTagInvalid
	}
	accessTags, err := a.tagsGrantingTargetAccess(ctx, targetID)
	if err != nil {
		return nil, err
	}
	tagOK := false
	for _, t := range accessTags {
		if t == tag {
			tagOK = true
			break
		}
	}
	if !tagOK {
		return nil, errInviteTagNotForTarget
	}
	all, err := a.invitableUserIDsForTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if a.UserStore == nil {
		return nil, nil
	}
	var out []string
	for _, uid := range all {
		userTags, err := a.UserStore.TagsForUser(uid)
		if err != nil {
			continue
		}
		for _, ut := range userTags {
			if ut == tag {
				out = append(out, uid)
				break
			}
		}
	}
	return out, nil
}

func (a *App) tagsGrantingTargetAccess(ctx context.Context, targetID string) ([]string, error) {
	if a.AccessGroupStore == nil || targetID == "" {
		return nil, nil
	}
	return a.AccessGroupStore.TagsGrantingTargetAccess(ctx, access.TargetID(targetID))
}

// invitationOptionsTag is a tag that grants access to the session target.
type invitationOptionsTag struct {
	Tag       string `json:"tag"`
	UserCount int    `json:"user_count"`
}

type invitationOptionsMember struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Tags     []string `json:"tags,omitempty"`
}

// collectInvitationOptions lists users who can access the session target
// (tag-based ACL) and tags usable for batch invitations.
func (a *App) collectInvitationOptions(ctx context.Context, ownerID, targetID string) ([]invitationOptionsTag, []invitationOptionsMember, error) {
	if targetID == "" {
		return nil, nil, nil
	}
	userIDs, err := a.invitableUserIDsForTarget(ctx, targetID)
	if err != nil {
		return nil, nil, err
	}
	accessTags, err := a.tagsGrantingTargetAccess(ctx, targetID)
	if err != nil {
		return nil, nil, err
	}
	accessTagSet := make(map[string]struct{}, len(accessTags))
	for _, t := range accessTags {
		accessTagSet[t] = struct{}{}
	}
	seenUsers := make(map[string]*invitationOptionsMember)
	tagCounts := make(map[string]int)
	for _, uid := range userIDs {
		if uid == ownerID {
			continue
		}
		m := a.invitationMember(ctx, uid, seenUsers)
		if a.UserStore == nil {
			continue
		}
		userTags, err := a.UserStore.TagsForUser(uid)
		if err != nil {
			continue
		}
		for _, t := range userTags {
			if _, ok := accessTagSet[t]; !ok {
				continue
			}
			m.Tags = append(m.Tags, t)
			tagCounts[t]++
		}
	}
	users := make([]invitationOptionsMember, 0, len(seenUsers))
	for _, m := range seenUsers {
		sort.Strings(m.Tags)
		users = append(users, *m)
	}
	sort.Slice(users, func(i, j int) bool {
		li := users[i].Username
		if li == "" {
			li = users[i].ID
		}
		lj := users[j].Username
		if lj == "" {
			lj = users[j].ID
		}
		if li == lj {
			return users[i].ID < users[j].ID
		}
		return li < lj
	})
	tags := make([]invitationOptionsTag, 0, len(tagCounts))
	for tag, n := range tagCounts {
		tags = append(tags, invitationOptionsTag{Tag: tag, UserCount: n})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Tag < tags[j].Tag })
	return tags, users, nil
}

func (a *App) invitationMember(ctx context.Context, uid string, seen map[string]*invitationOptionsMember) *invitationOptionsMember {
	if m, ok := seen[uid]; ok {
		return m
	}
	m := &invitationOptionsMember{
		ID:       uid,
		Username: a.usernameFor(ctx, uid),
	}
	seen[uid] = m
	return m
}

// handleInvitationOptions lists groups and users the session owner can invite.
func (a *App) handleInvitationOptions(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	termSess, ownerID, ok := a.loadOwnedTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	tags, users, err := a.collectInvitationOptions(r.Context(), ownerID, termSess.TargetID)
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
	writeJSON(w, map[string]interface{}{
		"tags":  tags,
		"users": users,
	})
}

// handleRegenerateInvitationJoinURL issues a fresh plain token for a
// pending invitation and returns the join URL. The previous link
// stops working. Owner only; invitation must still be active.
func (a *App) handleRegenerateInvitationJoinURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	termSess, ownerID, ok := a.loadOwnedTerminalSession(w, r, sessionID)
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
	if inv.SessionID != string(termSess.ID()) {
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
		"user_id":    ownerID,
		"session_id": string(termSess.ID()),
		"inv_id":     invID,
	})
	writeJSON(w, map[string]interface{}{
		"join_url": invitationJoinURL(sessionID, tok.Plain),
		"token":    tok.Plain,
	})
}

// handleListInvitations returns this session's invitations. Owner only.
func (a *App) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	if a.SharingStore == nil {
		writeJSON(w, map[string]interface{}{"items": []sharingResponseInvitation{}})
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	termSess, _, ok := a.loadOwnedTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	invs, err := a.SharingStore.ListBySession(r.Context(), string(termSess.ID()))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]sharingResponseInvitation, 0, len(invs))
	for _, inv := range invs {
		inviteeName := ""
		if !inv.IsLink() {
			inviteeName = a.usernameFor(r.Context(), inv.InviteeUserID)
		}
		out = append(out, encodeInvitation(inv, inviteeName))
	}
	writeJSON(w, map[string]interface{}{"items": out})
}

// handleRevokeInvitation marks the invitation revoked. Owner only.
func (a *App) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	termSess, ownerID, ok := a.loadOwnedTerminalSession(w, r, sessionID)
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
	if inv.SessionID != string(termSess.ID()) {
		writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
		return
	}
	if err := a.SharingStore.MarkRevoked(r.Context(), invID, time.Now().UTC()); err != nil {
		writeInternalError(w, err)
		return
	}
	audit("session_invitation_revoked", auditFields{
		"user_id":    ownerID,
		"session_id": string(termSess.ID()),
		"inv_id":     invID,
	})
	a.publishSharingEvent(termSess, sharing.EventInvitationRevoked, ownerID, "", map[string]interface{}{
		"invitation_id": invID,
	})
	if !inv.IsLink() && strings.TrimSpace(inv.InviteeUserID) != "" {
		a.publishInvitationEventToInvitee(inv.InviteeUserID, sharing.EventInvitationRevoked, inv)
	}
	w.WriteHeader(http.StatusNoContent)
}

// incomingInvitationItem is returned by GET /api/invitations/incoming for
// the authenticated invitee's pending named invitations.
type incomingInvitationItem struct {
	ID            string `json:"id"`
	SessionID     string `json:"session_id"`
	TargetID      string `json:"target_id"`
	TargetName    string `json:"target_name,omitempty"`
	OwnerUserID   string `json:"owner_user_id"`
	OwnerUsername string `json:"owner_username"`
	Mode          string `json:"mode"`
	ExpiresAt     int64  `json:"expires_at"`
}

// handleListIncomingInvitations lists active named invitations for the
// current user so the home page can show join prompts without a link.
func (a *App) handleListIncomingInvitations(w http.ResponseWriter, r *http.Request) {
	if a.SharingStore == nil {
		writeJSON(w, map[string]interface{}{"items": []incomingInvitationItem{}})
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	now := time.Now().UTC()
	invs, err := a.SharingStore.ListPendingForInvitee(r.Context(), userID, now)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]incomingInvitationItem, 0, len(invs))
	for _, inv := range invs {
		if inv.IsLink() || !inv.Active(now) {
			continue
		}
		if a.TerminalSessionManager != nil {
			if _, ok := a.TerminalSessionManager.Get(session.ID(inv.SessionID)); !ok {
				continue
			}
		}
		item := incomingInvitationItem{
			ID:            inv.ID,
			SessionID:     inv.SessionID,
			TargetID:      inv.TargetID,
			OwnerUserID:   inv.OwnerUserID,
			OwnerUsername: a.usernameFor(r.Context(), inv.OwnerUserID),
			Mode:          string(inv.Mode),
			ExpiresAt:     timestamp(inv.ExpiresAt),
		}
		if a.TargetStore != nil {
			if t, err := a.TargetStore.Get(r.Context(), access.TargetID(inv.TargetID)); err == nil && t != nil && t.Name != "" {
				item.TargetName = t.Name
			}
		}
		out = append(out, item)
	}
	writeJSON(w, map[string]interface{}{"items": out})
}

// handleJoinSession consumes an invitation and registers the caller
// as a participant in the room. Body must contain either
// `invitation_token` (link invitations) or `invitation_id` (named).
func (a *App) handleJoinSession(w http.ResponseWriter, r *http.Request) {
	if a.SharingStore == nil {
		writeJSONErrorKey(w, r, "sharing.unavailable", http.StatusServiceUnavailable)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONErrorKey(w, r, "sessions.idRequired", http.StatusBadRequest)
		return
	}
	termSess, ok := a.TerminalSessionManager.Get(session.ID(sessionID))
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	var body struct {
		InvitationID    string `json:"invitation_id"`
		InvitationToken string `json:"invitation_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	body.InvitationID = strings.TrimSpace(body.InvitationID)
	body.InvitationToken = strings.TrimSpace(body.InvitationToken)
	if body.InvitationID == "" && body.InvitationToken == "" {
		writeJSONErrorKey(w, r, "sharing.tokenOrIDRequired", http.StatusBadRequest)
		return
	}
	var inv sharing.Invitation
	var lookupErr error
	if body.InvitationToken != "" {
		inv, lookupErr = a.SharingStore.GetByTokenHash(r.Context(), sharing.HashToken(body.InvitationToken))
	} else {
		inv, _, lookupErr = a.SharingStore.GetByID(r.Context(), body.InvitationID)
	}
	if lookupErr != nil {
		if errors.Is(lookupErr, sharing.ErrInvitationNotFound) {
			writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, lookupErr)
		return
	}
	if inv.SessionID != string(termSess.ID()) {
		writeJSONErrorKey(w, r, "sharing.invitationNotFound", http.StatusNotFound)
		return
	}
	now := time.Now().UTC()
	if !inv.Active(now) {
		writeJSONErrorKey(w, r, "sharing.invitationInactive", http.StatusForbidden)
		return
	}
	// Named invitations bind to a specific user.
	if !inv.IsLink() && !strings.EqualFold(inv.InviteeUserID, userID) {
		writeJSONErrorKey(w, r, "sharing.invitationOtherUser", http.StatusForbidden)
		return
	}
	// Owners cannot consume their own invitation; they are already in.
	if userID == inv.OwnerUserID {
		writeJSONErrorKey(w, r, "sharing.cannotInviteSelf", http.StatusBadRequest)
		return
	}
	// Re-confirm the inviter is still allowed to share that target;
	// if access was revoked between issue and consumption the
	// invitation must fail closed.
	if ok, err := a.userCanAccessTarget(r.Context(), inv.OwnerUserID, access.TargetID(inv.TargetID)); err != nil {
		writeInternalError(w, err)
		return
	} else if !ok {
		writeJSONErrorKey(w, r, "sharing.invitationStaleAccess", http.StatusForbidden)
		return
	}
	if err := a.SharingStore.RecordUse(r.Context(), inv.ID, userID, now); err != nil {
		writeInternalError(w, err)
		return
	}
	updatedInv := inv
	if fresh, _, err := a.SharingStore.GetByID(r.Context(), inv.ID); err == nil {
		updatedInv = fresh
	}
	if !inv.IsLink() && strings.TrimSpace(inv.InviteeUserID) != "" {
		a.publishInvitationEventToInvitee(inv.InviteeUserID, sharing.EventInvitationConsumed, updatedInv)
	}
	room := a.ensureRoomFor(termSess)
	username := userID
	if a.UserStore != nil {
		if u, err := a.UserStore.GetByID(userID); err == nil && u != nil && u.Username != "" {
			username = u.Username
		}
	}
	if room != nil {
		room.AddViewer(userID, username, now)
	}
	audit("session_invitation_consumed", auditFields{
		"user_id":    userID,
		"session_id": string(termSess.ID()),
		"inv_id":     inv.ID,
		"is_link":    inv.IsLink(),
	})
	audit("session_participant_joined", auditFields{
		"user_id":    userID,
		"session_id": string(termSess.ID()),
		"username":   username,
	})
	invExtra := map[string]interface{}{
		"invitation_id": updatedInv.ID,
		"use_count":     updatedInv.UseCount,
	}
	if updatedInv.MaxUses != nil {
		invExtra["max_uses"] = *updatedInv.MaxUses
	}
	if updatedInv.IsLink() {
		stillActive := updatedInv.LinkUnlimited() ||
			(updatedInv.MaxUses != nil && updatedInv.UseCount < *updatedInv.MaxUses)
		if stillActive {
			a.publishSharingEvent(termSess, sharing.EventInvitationUpdated, userID, username, invExtra)
		}
	}
	a.publishSharingEvent(termSess, sharing.EventInvitationConsumed, userID, username, invExtra)
	a.publishSharingEvent(termSess, sharing.EventParticipantJoined, userID, username, nil)
	writeJSON(w, map[string]interface{}{
		"session_id": string(termSess.ID()),
		"role":       string(sharing.RoleViewer),
	})
}

// handleListParticipants returns the current room membership.
func (a *App) handleListParticipants(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	termSess, room, _, ok := a.loadParticipatingTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	if room == nil {
		room = a.ensureRoomFor(termSess)
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
	pending := make([]sharingResponseWriteRequest, 0)
	if room != nil {
		for _, wr := range room.PendingRequests() {
			pending = append(pending, encodeWriteRequest(wr))
		}
	}
	writerID := ""
	writerName := ""
	if room != nil {
		writerID = room.WriterID()
		for _, p := range room.Participants() {
			if p.UserID == writerID {
				writerName = p.Username
				break
			}
		}
		if writerName == "" && writerID != "" && a.UserStore != nil {
			writerName = a.usernameFor(r.Context(), writerID)
		}
	}
	resp := map[string]interface{}{
		"items":            out,
		"writer_id":        roomWriterID(room),
		"writer_username":  writerName,
		"owner_id":         termSess.UserID,
		"pending_requests": pending,
	}
	writeJSON(w, resp)
}

// handleKickParticipant removes a participant from the room. Owner only.
func (a *App) handleKickParticipant(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	termSess, ownerID, ok := a.loadOwnedTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	target := chi.URLParam(r, "user_id")
	if strings.TrimSpace(target) == "" {
		writeJSONErrorKey(w, r, "sharing.userIDRequired", http.StatusBadRequest)
		return
	}
	if target == ownerID {
		writeJSONErrorKey(w, r, "sharing.cannotKickOwner", http.StatusBadRequest)
		return
	}
	room, _ := a.SharingRegistry.Get(string(termSess.ID()))
	if room == nil {
		writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusNotFound)
		return
	}
	if err := room.RemoveParticipant(target); err != nil {
		switch {
		case errors.Is(err, sharing.ErrParticipantMissing):
			writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusNotFound)
		case errors.Is(err, sharing.ErrCannotKickOwner):
			writeJSONErrorKey(w, r, "sharing.cannotKickOwner", http.StatusBadRequest)
		default:
			writeInternalError(w, err)
		}
		return
	}
	if a.SharingBridges != nil {
		if controller, ok := a.SharingBridges.Get(termSess.ID()); ok {
			controller.SetWriter(room.WriterID())
		}
	}
	audit("session_participant_kicked", auditFields{
		"user_id":    ownerID,
		"target_id":  target,
		"session_id": string(termSess.ID()),
	})
	a.publishSharingEvent(termSess, sharing.EventParticipantLeft, target, "", map[string]interface{}{
		"reason": "kicked",
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateWriteRequest creates a new write-token request for the
// caller. Caller must be a participant.
func (a *App) handleCreateWriteRequest(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	termSess, _, userID, ok := a.loadParticipatingTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	room := a.ensureRoomFor(termSess)
	if room == nil {
		writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusForbidden)
		return
	}
	username := userID
	if a.UserStore != nil {
		if u, err := a.UserStore.GetByID(userID); err == nil && u != nil && u.Username != "" {
			username = u.Username
		}
	}
	id, err := newSharingID()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	wr, err := room.RequestWrite(id, userID, username, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, sharing.ErrAlreadyWriter):
			writeJSONErrorKey(w, r, "sharing.alreadyWriter", http.StatusBadRequest)
		case errors.Is(err, sharing.ErrParticipantMissing):
			writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusForbidden)
		default:
			writeInternalError(w, err)
		}
		return
	}
	audit("session_write_request_created", auditFields{
		"user_id":    userID,
		"session_id": string(termSess.ID()),
		"request_id": wr.ID,
	})
	a.publishWriteRequestPending(termSess, room, userID, username, wr.ID)
	writeJSON(w, encodeWriteRequest(wr))
}

// publishWriteRequestPending notifies only the user that currently
// holds the write token (the one who can grant or deny).
func (a *App) publishWriteRequestPending(termSess *session.Session, room *sharing.Room, requesterID, requesterName, requestID string) {
	if a.SessionEventBroker == nil || termSess == nil || room == nil {
		return
	}
	writerID := strings.TrimSpace(room.WriterID())
	if writerID == "" || writerID == requesterID {
		// No token holder to notify (or requester already holds the token).
		return
	}
	payload := sharing.Event{
		Type:      sharing.EventWriteRequestPending,
		SessionID: string(termSess.ID()),
		UserID:    requesterID,
		Username:  requesterName,
		Extra: map[string]interface{}{
			"request_id": requestID,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	a.SessionEventBroker.PublishToUsers(body, writerID)
}

// handleGrantWriteRequest gives the requester the write token. Caller
// must currently hold the token (typically the owner).
func (a *App) handleGrantWriteRequest(w http.ResponseWriter, r *http.Request) {
	a.decideWriteRequest(w, r, true)
}

// handleDenyWriteRequest rejects the request without changing the
// writer. Caller must hold the token.
func (a *App) handleDenyWriteRequest(w http.ResponseWriter, r *http.Request) {
	a.decideWriteRequest(w, r, false)
}

func (a *App) decideWriteRequest(w http.ResponseWriter, r *http.Request, grant bool) {
	sessionID := chi.URLParam(r, "session_id")
	termSess, room, userID, ok := a.loadParticipatingTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	if room == nil {
		writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusForbidden)
		return
	}
	reqID := chi.URLParam(r, "request_id")
	if strings.TrimSpace(reqID) == "" {
		writeJSONErrorKey(w, r, "sharing.requestIDRequired", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	var event string
	var extra map[string]interface{}
	if grant {
		wr, previous, err := room.GrantWrite(reqID, userID, now)
		if err != nil {
			a.respondWriteRequestErr(w, r, err)
			return
		}
		if a.SharingBridges != nil {
			if controller, ok := a.SharingBridges.Get(termSess.ID()); ok {
				controller.SetWriter(wr.RequesterID)
			}
		}
		audit("session_write_token_granted", auditFields{
			"user_id":      userID,
			"session_id":   string(termSess.ID()),
			"request_id":   reqID,
			"previous_id":  previous,
			"requester_id": wr.RequesterID,
		})
		event = sharing.EventWriteTokenTransferred
		extra = map[string]interface{}{
			"previous_id":  previous,
			"requester_id": wr.RequesterID,
			"request_id":   wr.ID,
			"status":       string(wr.Status),
		}
		a.publishSharingEvent(termSess, event, wr.RequesterID, wr.RequesterName, extra)
		writeJSON(w, encodeWriteRequest(wr))
		return
	}
	wr, err := room.DenyWrite(reqID, userID, now)
	if err != nil {
		a.respondWriteRequestErr(w, r, err)
		return
	}
	audit("session_write_token_denied", auditFields{
		"user_id":      userID,
		"session_id":   string(termSess.ID()),
		"request_id":   reqID,
		"requester_id": wr.RequesterID,
	})
	a.publishSharingEvent(termSess, sharing.EventWriteRequestDecided, wr.RequesterID, wr.RequesterName, map[string]interface{}{
		"request_id": wr.ID,
		"status":     string(wr.Status),
	})
	writeJSON(w, encodeWriteRequest(wr))
}

func (a *App) respondWriteRequestErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sharing.ErrRequestNotFound):
		writeJSONErrorKey(w, r, "sharing.writeRequestNotFound", http.StatusNotFound)
	case errors.Is(err, sharing.ErrRequestNotPending):
		writeJSONErrorKey(w, r, "sharing.writeRequestNotPending", http.StatusBadRequest)
	case errors.Is(err, sharing.ErrNotWriter):
		writeJSONErrorKey(w, r, "sharing.notWriter", http.StatusForbidden)
	case errors.Is(err, sharing.ErrParticipantMissing):
		writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusGone)
	default:
		writeInternalError(w, err)
	}
}

// handleReleaseWriteToken hands the write token back to the owner.
// Owners releasing is a no-op.
func (a *App) handleReleaseWriteToken(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	termSess, room, userID, ok := a.loadParticipatingTerminalSession(w, r, sessionID)
	if !ok {
		return
	}
	if room == nil {
		writeJSONErrorKey(w, r, "sharing.participantNotFound", http.StatusForbidden)
		return
	}
	previous, err := room.ReleaseWrite(userID)
	if err != nil {
		switch {
		case errors.Is(err, sharing.ErrNotWriter):
			writeJSONErrorKey(w, r, "sharing.notWriter", http.StatusForbidden)
		default:
			writeInternalError(w, err)
		}
		return
	}
	if a.SharingBridges != nil {
		if controller, ok := a.SharingBridges.Get(termSess.ID()); ok {
			controller.SetWriter(room.WriterID())
		}
	}
	audit("session_write_token_released", auditFields{
		"user_id":     userID,
		"session_id":  string(termSess.ID()),
		"previous_id": previous,
		"new_id":      room.WriterID(),
	})
	a.publishSharingEvent(termSess, sharing.EventWriteTokenTransferred, room.WriterID(), "", map[string]interface{}{
		"previous_id":  previous,
		"requester_id": room.WriterID(),
	})
	w.WriteHeader(http.StatusNoContent)
}

// publishSharingEvent fans an event out to every participant of the
// room (plus the owner). The payload is JSON-marshaled before
// dispatch so the SSE handler does not need to know about
// sharing.Event.
func (a *App) publishSharingEvent(termSess *session.Session, event, userID, username string, extra map[string]interface{}) {
	if a.SessionEventBroker == nil || termSess == nil {
		return
	}
	payload := sharing.Event{
		Type:      event,
		SessionID: string(termSess.ID()),
		UserID:    userID,
		Username:  username,
		Extra:     extra,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	users := []string{termSess.UserID}
	if a.SharingRegistry != nil {
		if room, ok := a.SharingRegistry.Get(string(termSess.ID())); ok {
			for _, p := range room.Participants() {
				users = append(users, p.UserID)
			}
		}
	}
	a.SessionEventBroker.PublishToUsers(body, users...)
	// session_change lets all tabs refresh lists even if a targeted event was dropped.
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
}

// publishInvitationEventToInvitee notifies a named invitee on the home
// page SSE stream (session events scoped to their user ID).
func (a *App) publishInvitationEventToInvitee(inviteeUserID, event string, inv sharing.Invitation) {
	if a.SessionEventBroker == nil {
		return
	}
	inviteeUserID = strings.TrimSpace(inviteeUserID)
	if inviteeUserID == "" {
		return
	}
	payload := sharing.Event{
		Type:      event,
		SessionID: inv.SessionID,
		UserID:    inv.OwnerUserID,
		Username:  a.usernameFor(context.Background(), inv.OwnerUserID),
		Extra: map[string]interface{}{
			"invitation_id": inv.ID,
			"target_id":     inv.TargetID,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	a.SessionEventBroker.PublishToUsers(body, inviteeUserID)
	a.SessionEventBroker.Broadcast()
}

func encodeInvitation(inv sharing.Invitation, inviteeUsername string) sharingResponseInvitation {
	out := sharingResponseInvitation{
		ID:          inv.ID,
		Mode:        string(inv.Mode),
		OwnerUserID: inv.OwnerUserID,
		IsLink:      inv.IsLink(),
		UseCount:    inv.UseCount,
		ExpiresAt:   timestamp(inv.ExpiresAt),
		CreatedAt:   timestamp(inv.CreatedAt),
		UsedAt:      timestamp(inv.UsedAt),
		RevokedAt:   timestamp(inv.RevokedAt),
	}
	if inv.IsLink() {
		out.LinkUnlimited = inv.LinkUnlimited()
		if !inv.LinkUnlimited() {
			out.MaxUses = inv.MaxUses
		}
	} else {
		out.InviteeUserID = inv.InviteeUserID
		out.InviteeUsername = inviteeUsername
	}
	if inv.InviteGroupID != "" {
		out.InviteGroupID = inv.InviteGroupID
	}
	if inv.InviteTag != "" {
		out.InviteTag = inv.InviteTag
	}
	return out
}

func encodeWriteRequest(wr sharing.WriteRequest) sharingResponseWriteRequest {
	return sharingResponseWriteRequest{
		ID:            wr.ID,
		RequesterID:   wr.RequesterID,
		RequesterName: wr.RequesterName,
		Status:        string(wr.Status),
		RequestedAt:   timestamp(wr.RequestedAt),
		DecidedAt:     timestamp(wr.DecidedAt),
		DecidedBy:     wr.DecidedBy,
	}
}

func roomWriterID(room *sharing.Room) string {
	if room == nil {
		return ""
	}
	return room.WriterID()
}

func timestamp(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func invitationJoinURL(sessionID, plainToken string) string {
	return "/terminal?session_id=" + sessionID + "&mode=viewer&invite=" + plainToken
}

// newSharingID returns a short, URL-safe random ID used for
// invitations and write-request IDs.
func newSharingID() (string, error) {
	tok, err := sharing.GenerateToken()
	if err != nil {
		return "", err
	}
	if len(tok.Plain) > 22 {
		return tok.Plain[:22], nil
	}
	return tok.Plain, nil
}

// terminalSessionByOwnerOrParticipant is a helper used by the
// WebSocket attach branch in terminal.go. It returns the session if
// the caller can attach in viewerMode (room participant) or
// writerMode (owner / current writer).
func (a *App) terminalSessionByOwnerOrParticipant(ctx context.Context, sessionID, userID string, viewer bool) (*session.Session, *sharing.Room, error) {
	if a.TerminalSessionManager == nil {
		return nil, nil, errors.New("terminal session manager not configured")
	}
	termSess, ok := a.TerminalSessionManager.Get(session.ID(sessionID))
	if !ok {
		return nil, nil, sharing.ErrRoomNotFound
	}
	room, _ := a.SharingRegistry.Get(string(termSess.ID()))
	if !viewer {
		// Writer attach: caller must be the owner or the current
		// holder of the write token.
		if termSess.UserID != userID && (room == nil || room.WriterID() != userID) {
			return nil, room, sharing.ErrNotWriter
		}
		// Plus they must still have target access.
		if ok, err := a.userCanAccessTarget(ctx, userID, access.TargetID(termSess.TargetID)); err != nil {
			return nil, room, err
		} else if !ok {
			return nil, room, sharing.ErrNotWriter
		}
		return termSess, room, nil
	}
	if room == nil || !room.IsParticipant(userID) {
		return nil, room, sharing.ErrParticipantMissing
	}
	return termSess, room, nil
}

// envLookup wraps os.Getenv so tests can override the invitation TTL
// ceiling without touching the real environment.
var envLookup = os.Getenv

// usernameFor returns the username for userID, falling back to the
// raw ID when the lookup fails.
func (a *App) usernameFor(_ context.Context, userID string) string {
	if a == nil || a.UserStore == nil {
		return userID
	}
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil || u.Username == "" {
		return userID
	}
	return u.Username
}
