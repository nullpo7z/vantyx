package sharing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Mode is the privilege level granted to an invitee. Phase A only
// implements ModeViewer; ModeWriterEligible is reserved so the schema
// can grow without a migration when control transfer arrives.
type Mode string

const (
	// ModeViewer means the invitee can attach as a read-only viewer.
	ModeViewer Mode = "viewer"
	// ModeWriterEligible means the invitee may both view and request
	// the write token without the owner having to pre-authorize the
	// handoff. Currently treated identically to ModeViewer at the
	// auth layer because every participant can request the token.
	ModeWriterEligible Mode = "writer_eligible"
)

// Kind identifies which kind of session the invitation targets. The
// column is reserved for future RDP / VNC support; today every row
// uses KindTerminal.
type Kind string

const (
	KindTerminal Kind = "terminal"
)

// Invitation is the on-disk shape of an invitation row.
type Invitation struct {
	ID            string
	SessionID     string
	SessionKind   Kind
	TargetID      string
	OwnerUserID   string
	InviteeUserID string // empty -> link invitation
	InviteGroupID string // optional group scope for named invitations
	InviteTag     string // set when issued to all users with a tag (audit)
	Mode          Mode
	MaxUses       *int // link only: nil = unlimited, 1 = single-use, N = capped
	UseCount      int
	ExpiresAt     time.Time
	UsedAt        time.Time // zero -> not fully consumed
	RevokedAt     time.Time // zero -> not revoked
	CreatedAt     time.Time
}

// Used reports whether the invitation has been consumed.
func (i Invitation) Used() bool { return !i.UsedAt.IsZero() }

// Revoked reports whether the invitation has been revoked.
func (i Invitation) Revoked() bool { return !i.RevokedAt.IsZero() }

// Expired reports whether the invitation has expired (relative to now).
func (i Invitation) Expired(now time.Time) bool {
	return !i.ExpiresAt.IsZero() && now.After(i.ExpiresAt)
}

// IsLink reports whether this is a token-based (anonymous) invitation.
func (i Invitation) IsLink() bool { return strings.TrimSpace(i.InviteeUserID) == "" }

// LinkUnlimited reports whether a link invitation has no join cap
// (max_uses NULL in the database for newly created unlimited links).
func (i Invitation) LinkUnlimited() bool {
	return i.IsLink() && i.MaxUses == nil
}

// Exhausted reports whether no more joins are allowed.
func (i Invitation) Exhausted() bool {
	if i.IsLink() {
		if i.LinkUnlimited() {
			return false
		}
		if i.MaxUses != nil {
			return i.UseCount >= *i.MaxUses
		}
		return i.Used()
	}
	return i.Used()
}

// Active reports whether the invitation is still usable: not revoked,
// not fully consumed, and not expired.
func (i Invitation) Active(now time.Time) bool {
	return !i.Exhausted() && !i.Revoked() && !i.Expired(now)
}

// ErrInvitationNotFound is returned when a token / id does not match.
var (
	ErrInvitationNotFound       = errors.New("invitation not found")
	ErrInvitationExpired        = errors.New("invitation has expired")
	ErrInvitationConsumed       = errors.New("invitation has already been used")
	ErrInvitationRevoked        = errors.New("invitation has been revoked")
	ErrInvitationModeInvalid    = errors.New("invitation mode is not supported")
	ErrInvitationKindInvalid    = errors.New("invitation kind is not supported")
	ErrInvitationOwnerMismatch  = errors.New("invitation belongs to a different session owner")
	ErrInvitationTargetMismatch = errors.New("invitation does not match the consuming user")
)

// CreateInvitationOpts captures the inputs that the HTTP layer derives
// from the request body and the authenticated owner.
type CreateInvitationOpts struct {
	SessionID     string
	SessionKind   Kind
	TargetID      string
	OwnerUserID   string
	InviteeUserID string // optional; empty for link invitations
	Mode          Mode
	TTL           time.Duration
}

// Token wraps the freshly-generated link-invitation secret. The plain
// token is returned to the caller exactly once (so it can be embedded
// in a join URL). Only the SHA-256 hash is persisted.
type Token struct {
	Plain string
	Hash  string
}

// Store defines the persistence behaviour required for invitations.
// SQLite implements it; tests can substitute an in-memory map.
type Store interface {
	Create(ctx context.Context, inv Invitation, tokenHash string) error
	GetByID(ctx context.Context, id string) (Invitation, string, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (Invitation, error)
	ListBySession(ctx context.Context, sessionID string) ([]Invitation, error)
	// ListPendingForInvitee returns active named invitations addressed to
	// inviteeUserID (link invitations are excluded).
	ListPendingForInvitee(ctx context.Context, inviteeUserID string, now time.Time) ([]Invitation, error)
	// RecordUse increments use_count and marks the row consumed when
	// the cap is reached (named invites always exhaust on first use).
	RecordUse(ctx context.Context, id, consumerUserID string, when time.Time) error
	MarkRevoked(ctx context.Context, id string, when time.Time) error
	// UpdateTokenHash replaces the stored token hash for a pending
	// invitation so a fresh join URL can be issued without creating a
	// new row. The previous plain token stops working immediately.
	UpdateTokenHash(ctx context.Context, id, tokenHash string) error
}

// GenerateToken returns a 32-byte URL-safe random token along with its
// SHA-256 hash for storage. The plain value must never reach the DB.
func GenerateToken() (Token, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return Token{}, err
	}
	plain := base64.RawURLEncoding.EncodeToString(buf)
	return Token{Plain: plain, Hash: HashToken(plain)}, nil
}

// HashToken returns the canonical SHA-256 hex digest of plain.
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// NormaliseMode returns the canonical Mode for raw, or
// ErrInvitationModeInvalid for unknown values. An empty raw maps to
// ModeViewer to keep the API forgiving for older clients.
func NormaliseMode(raw string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ModeViewer):
		return ModeViewer, nil
	case string(ModeWriterEligible):
		return ModeWriterEligible, nil
	default:
		return "", ErrInvitationModeInvalid
	}
}

// NormaliseKind validates the session kind. Empty defaults to
// KindTerminal.
func NormaliseKind(raw string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(KindTerminal):
		return KindTerminal, nil
	default:
		return "", ErrInvitationKindInvalid
	}
}
