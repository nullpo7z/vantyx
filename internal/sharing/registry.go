package sharing

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// Role identifies how a participant is using a shared session.
type Role string

const (
	// RoleOwner is the user that started the session. The owner is
	// always present in the participant list and may invite/kick.
	RoleOwner Role = "owner"
	// RoleViewer is a participant attached via an accepted invitation.
	// Viewers receive output but their input is dropped at the
	// WebSocket layer unless they hold the write token.
	RoleViewer Role = "viewer"
)

// Sentinel errors returned by Registry / Room operations. Callers in
// internal/httpapi convert these into i18n-keyed HTTP responses.
var (
	ErrRoomNotFound       = errors.New("session room not found")
	ErrParticipantMissing = errors.New("participant is not in the session")
	ErrNotOwner           = errors.New("only the session owner may perform this action")
	ErrNotWriter          = errors.New("only the current writer may perform this action")
	ErrAlreadyWriter      = errors.New("requester already holds the write token")
	ErrRequestNotFound    = errors.New("write request not found")
	ErrRequestNotPending  = errors.New("write request is no longer pending")
	ErrCannotKickOwner    = errors.New("the session owner cannot be removed")
	ErrCannotKickSelf     = errors.New("you cannot kick yourself")
	ErrUserKicked         = errors.New("user was removed from the session and cannot rejoin")
	// ErrInviterNoTargetAccess is returned when the session owner has lost
	// access to the underlying target since the invitation was created
	// (stale-access guard). ErrJoinerNoTargetAccess is returned when the
	// joining user does not have target access of their own -- a valid
	// invitation token is never sufficient without it.
	ErrInviterNoTargetAccess = errors.New("session owner no longer has access to the target")
	ErrJoinerNoTargetAccess  = errors.New("user does not have access to the target")
)

// WriteRequestStatus tracks the lifecycle of a control-handoff request.
type WriteRequestStatus string

const (
	WriteRequestPending  WriteRequestStatus = "pending"
	WriteRequestGranted  WriteRequestStatus = "granted"
	WriteRequestDenied   WriteRequestStatus = "denied"
	WriteRequestCanceled WriteRequestStatus = "canceled"
)

// Participant captures a single user's presence inside a Room.
type Participant struct {
	UserID       string
	Username     string
	Role         Role
	JoinedAt     time.Time
	InvitationID string // invitation used to join; empty for owner
}

// WriteRequest tracks one pending or recently-decided handoff.
type WriteRequest struct {
	ID            string
	RequesterID   string
	RequesterName string
	Status        WriteRequestStatus
	RequestedAt   time.Time
	DecidedAt     time.Time
	DecidedBy     string
}

// Room is the state for one shareable terminal session. The owner
// always has access; other participants must hold an invitation that
// matches their user ID (named) or have consumed a link invitation.
type Room struct {
	mu         sync.RWMutex
	sessionID  string
	targetID   string
	ownerID    string
	writerID   string // user that currently holds stdin write capability
	createdAt  time.Time
	memberByID map[string]*Participant
	requests   map[string]*WriteRequest
	kicked     map[string]struct{}
}

// SessionID returns the bound session identifier.
func (r *Room) SessionID() string { return r.sessionID }

// TargetID returns the bound target identifier.
func (r *Room) TargetID() string { return r.targetID }

// OwnerID returns the user that started the session.
func (r *Room) OwnerID() string { return r.ownerID }

// WriterID returns the user that currently owns the write token.
func (r *Room) WriterID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.writerID
}

// HasWriter reports whether userID currently holds the write token.
func (r *Room) HasWriter(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.writerID == userID
}

// IsParticipant reports whether userID is a member of the room.
func (r *Room) IsParticipant(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.memberByID[userID]
	return ok
}

// Participants returns a stable copy of the participant list.
func (r *Room) Participants() []Participant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Participant, 0, len(r.memberByID))
	for _, p := range r.memberByID {
		out = append(out, *p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Owner first, then by JoinedAt ascending.
		if out[i].Role != out[j].Role {
			return out[i].Role == RoleOwner
		}
		return out[i].JoinedAt.Before(out[j].JoinedAt)
	})
	return out
}

// PendingRequests returns a snapshot of pending write requests.
func (r *Room) PendingRequests() []WriteRequest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WriteRequest, 0, len(r.requests))
	for _, req := range r.requests {
		if req.Status == WriteRequestPending {
			out = append(out, *req)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].RequestedAt.Before(out[j].RequestedAt)
	})
	return out
}

// IsKicked reports whether userID was removed by the owner and may not rejoin.
func (r *Room) IsKicked(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.kicked[userID]
	return ok
}

// Unkick lifts the rejoin block set by RemoveParticipant. The owner
// explicitly re-inviting the same user by name is treated as permission
// to come back; shareable links, tag and group invitations do not lift
// it, so a kick keeps its meaning against blanket invitations. Returns
// true when a block was actually removed.
func (r *Room) Unkick(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.kicked[userID]; !ok {
		return false
	}
	delete(r.kicked, userID)
	return true
}

// AddViewer records that userID has joined the room as a viewer.
// Re-adding an existing participant updates the username only.
func (r *Room) AddViewer(userID, username, invitationID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, blocked := r.kicked[userID]; blocked {
		return ErrUserKicked
	}
	if existing, ok := r.memberByID[userID]; ok {
		if username != "" {
			existing.Username = username
		}
		if invitationID != "" {
			existing.InvitationID = invitationID
		}
		return nil
	}
	r.memberByID[userID] = &Participant{
		UserID:       userID,
		Username:     username,
		Role:         RoleViewer,
		JoinedAt:     now,
		InvitationID: invitationID,
	}
	return nil
}

// ParticipantUserIDsForInvitation returns user IDs of viewers who joined
// via invitationID. The owner is never included.
func (r *Room) ParticipantUserIDsForInvitation(invitationID string) []string {
	if invitationID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0)
	for uid, p := range r.memberByID {
		if p.Role == RoleViewer && p.InvitationID == invitationID {
			out = append(out, uid)
		}
	}
	sort.Strings(out)
	return out
}

// RemoveParticipant detaches userID from the room. The owner cannot be
// removed; ErrCannotKickOwner is returned. Returns
// ErrParticipantMissing if userID is not in the room.
func (r *Room) RemoveParticipant(userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if userID == r.ownerID {
		return ErrCannotKickOwner
	}
	if _, ok := r.memberByID[userID]; !ok {
		return ErrParticipantMissing
	}
	delete(r.memberByID, userID)
	if r.kicked == nil {
		r.kicked = map[string]struct{}{}
	}
	r.kicked[userID] = struct{}{}
	// Drop pending requests originating from the leaver.
	for id, req := range r.requests {
		if req.RequesterID == userID && req.Status == WriteRequestPending {
			req.Status = WriteRequestCanceled
			req.DecidedAt = time.Now().UTC()
			req.DecidedBy = userID
			_ = id
		}
	}
	// If the leaver held the write token, hand it back to the owner so
	// the session is never input-orphaned.
	if r.writerID == userID {
		r.writerID = r.ownerID
	}
	return nil
}

// RequestWrite creates a new pending write request. Owners and the
// current writer cannot file requests against themselves. The room
// must contain the requester.
func (r *Room) RequestWrite(reqID, userID, username string, now time.Time) (WriteRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.memberByID[userID]
	if !ok {
		return WriteRequest{}, ErrParticipantMissing
	}
	if r.writerID == userID {
		return WriteRequest{}, ErrAlreadyWriter
	}
	// Cancel any prior pending request from this user.
	for _, prior := range r.requests {
		if prior.RequesterID == userID && prior.Status == WriteRequestPending {
			prior.Status = WriteRequestCanceled
			prior.DecidedAt = now
			prior.DecidedBy = userID
		}
	}
	wr := &WriteRequest{
		ID:            reqID,
		RequesterID:   userID,
		RequesterName: p.Username,
		Status:        WriteRequestPending,
		RequestedAt:   now,
	}
	if wr.RequesterName == "" {
		wr.RequesterName = username
	}
	r.requests[reqID] = wr
	return *wr, nil
}

// GrantWrite transfers the write token to the request's requester.
// decidedBy must be the current writer.
func (r *Room) GrantWrite(reqID, decidedBy string, now time.Time) (WriteRequest, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wr, ok := r.requests[reqID]
	if !ok {
		return WriteRequest{}, "", ErrRequestNotFound
	}
	if wr.Status != WriteRequestPending {
		return WriteRequest{}, "", ErrRequestNotPending
	}
	if decidedBy != r.writerID {
		return WriteRequest{}, "", ErrNotWriter
	}
	if _, present := r.memberByID[wr.RequesterID]; !present {
		// Requester left while the request was pending.
		wr.Status = WriteRequestCanceled
		wr.DecidedAt = now
		wr.DecidedBy = decidedBy
		return *wr, "", ErrParticipantMissing
	}
	previous := r.writerID
	r.writerID = wr.RequesterID
	wr.Status = WriteRequestGranted
	wr.DecidedAt = now
	wr.DecidedBy = decidedBy
	return *wr, previous, nil
}

// DenyWrite marks the request as denied. decidedBy must be the current
// writer.
func (r *Room) DenyWrite(reqID, decidedBy string, now time.Time) (WriteRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wr, ok := r.requests[reqID]
	if !ok {
		return WriteRequest{}, ErrRequestNotFound
	}
	if wr.Status != WriteRequestPending {
		return WriteRequest{}, ErrRequestNotPending
	}
	if decidedBy != r.writerID {
		return WriteRequest{}, ErrNotWriter
	}
	wr.Status = WriteRequestDenied
	wr.DecidedAt = now
	wr.DecidedBy = decidedBy
	return *wr, nil
}

// ReleaseWrite hands the write token back to the owner. userID must be
// the current writer; the owner releasing is a no-op.
func (r *Room) ReleaseWrite(userID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writerID != userID {
		return "", ErrNotWriter
	}
	if userID == r.ownerID {
		return r.ownerID, nil
	}
	previous := r.writerID
	r.writerID = r.ownerID
	return previous, nil
}

// Registry holds rooms keyed by session ID.
type Registry struct {
	mu    sync.RWMutex
	rooms map[string]*Room
	now   func() time.Time
}

// NewRegistry constructs a Registry backed by time.Now.
func NewRegistry() *Registry {
	return &Registry{
		rooms: make(map[string]*Room),
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// EnsureRoom creates (or returns) the room for the given session.
// Owner participation is recorded automatically.
func (reg *Registry) EnsureRoom(sessionID, targetID, ownerID, ownerName string) *Room {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if room, ok := reg.rooms[sessionID]; ok {
		return room
	}
	now := reg.now()
	room := &Room{
		sessionID:  sessionID,
		targetID:   targetID,
		ownerID:    ownerID,
		writerID:   ownerID,
		createdAt:  now,
		memberByID: map[string]*Participant{},
		requests:   map[string]*WriteRequest{},
		kicked:     map[string]struct{}{},
	}
	room.memberByID[ownerID] = &Participant{
		UserID:   ownerID,
		Username: ownerName,
		Role:     RoleOwner,
		JoinedAt: now,
	}
	reg.rooms[sessionID] = room
	return room
}

// Get returns the room for sessionID, or nil if missing.
func (reg *Registry) Get(sessionID string) (*Room, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	r, ok := reg.rooms[sessionID]
	return r, ok
}

// Remove drops the room. Used when the underlying session terminates.
func (reg *Registry) Remove(sessionID string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	delete(reg.rooms, sessionID)
}

// RoomsForUser returns the session IDs where userID is a participant.
// Used by the SSE broker to fan out events only to relevant clients.
func (reg *Registry) RoomsForUser(userID string) []string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make([]string, 0)
	for sid, room := range reg.rooms {
		if room.IsParticipant(userID) {
			out = append(out, sid)
		}
	}
	sort.Strings(out)
	return out
}
