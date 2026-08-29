package sharing

import (
	"context"
	"errors"
	"testing"
	"time"
)

// recordUseStore is a minimal Store that only tracks RecordUse calls;
// JoinRoom is the only method under test here and it touches nothing else.
type recordUseStore struct {
	recordUseCalls int
}

func (s *recordUseStore) Create(context.Context, Invitation, string) error { return nil }
func (s *recordUseStore) GetByID(context.Context, string) (Invitation, string, error) {
	return Invitation{}, "", nil
}
func (s *recordUseStore) GetByTokenHash(context.Context, string) (Invitation, error) {
	return Invitation{}, nil
}
func (s *recordUseStore) ListBySession(context.Context, string) ([]Invitation, error) {
	return nil, nil
}
func (s *recordUseStore) ListPendingForInvitee(context.Context, string, time.Time) ([]Invitation, error) {
	return nil, nil
}
func (s *recordUseStore) RecordUse(context.Context, string, string, time.Time) error {
	s.recordUseCalls++
	return nil
}
func (s *recordUseStore) MarkRevoked(context.Context, string, time.Time) error  { return nil }
func (s *recordUseStore) UpdateTokenHash(context.Context, string, string) error { return nil }

func linkInvitationFixture() Invitation {
	return Invitation{
		ID:          "inv-1",
		SessionID:   "sess-1",
		SessionKind: KindTerminal,
		TargetID:    "target-x",
		OwnerUserID: "owner",
		// InviteeUserID empty -> link invitation (any authenticated non-owner).
		Mode:      ModeViewer,
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

// TestService_JoinRoom_EnforcesJoinerTargetAccess is the regression guard
// for the CLI-vs-HTTP authorization bypass: a valid link token must NOT be
// enough to join when the joining user lacks target access. The check must
// live in the shared service so every caller (HTTP and the CLI SSH
// gateway) inherits it, and it must run before the invitation is consumed.
func TestService_JoinRoom_EnforcesJoinerTargetAccess(t *testing.T) {
	store := &recordUseStore{}
	svc := &Service{
		Store:    store,
		Registry: NewRegistry(),
		// Owner has access; the joining user "mallory" does not.
		Access: func(_ context.Context, userID, targetID string) (bool, error) {
			if targetID != "target-x" {
				return false, nil
			}
			return userID == "owner", nil
		},
	}

	_, err := svc.JoinRoom(context.Background(), linkInvitationFixture(), "mallory", "mallory", time.Now().UTC())
	if !errors.Is(err, ErrJoinerNoTargetAccess) {
		t.Fatalf("expected ErrJoinerNoTargetAccess, got %v", err)
	}
	if store.recordUseCalls != 0 {
		t.Fatalf("invitation must not be consumed when the joining user lacks target access; RecordUse called %d times", store.recordUseCalls)
	}
	if room, ok := svc.Registry.Get("sess-1"); ok && room.IsParticipant("mallory") {
		t.Fatalf("mallory must not be added to the room when denied")
	}
}

// TestService_JoinRoom_EnforcesInviterStaleAccess guards the stale-access
// path: if the owner has lost access to the target since issuing the
// invitation, no one may join it.
func TestService_JoinRoom_EnforcesInviterStaleAccess(t *testing.T) {
	store := &recordUseStore{}
	svc := &Service{
		Store:    store,
		Registry: NewRegistry(),
		// Nobody has access -> the owner (stale) check fails first.
		Access: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	_, err := svc.JoinRoom(context.Background(), linkInvitationFixture(), "bob", "bob", time.Now().UTC())
	if !errors.Is(err, ErrInviterNoTargetAccess) {
		t.Fatalf("expected ErrInviterNoTargetAccess, got %v", err)
	}
	if store.recordUseCalls != 0 {
		t.Fatalf("invitation must not be consumed on stale inviter access; RecordUse called %d times", store.recordUseCalls)
	}
}

// TestService_JoinRoom_AllowsWhenBothHaveAccess confirms the check does not
// break the legitimate flow: when both the owner and the joining user can
// reach the target, the join succeeds and the user is added as a viewer.
func TestService_JoinRoom_AllowsWhenBothHaveAccess(t *testing.T) {
	store := &recordUseStore{}
	svc := &Service{
		Store:    store,
		Registry: NewRegistry(),
		Access:   func(context.Context, string, string) (bool, error) { return true, nil },
	}

	room, err := svc.JoinRoom(context.Background(), linkInvitationFixture(), "bob", "bob", time.Now().UTC())
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if room == nil || !room.IsParticipant("bob") {
		t.Fatalf("bob should be a participant after a permitted join")
	}
	if store.recordUseCalls != 1 {
		t.Fatalf("expected RecordUse to be called once, got %d", store.recordUseCalls)
	}
}

// TestService_JoinRoom_NilAccessSkipsCheck documents that a nil Access is
// treated as "no check" (used only by tests that don't exercise ACL).
func TestService_JoinRoom_NilAccessSkipsCheck(t *testing.T) {
	store := &recordUseStore{}
	svc := &Service{Store: store, Registry: NewRegistry()}
	room, err := svc.JoinRoom(context.Background(), linkInvitationFixture(), "bob", "bob", time.Now().UTC())
	if err != nil {
		t.Fatalf("JoinRoom with nil Access: %v", err)
	}
	if room == nil || !room.IsParticipant("bob") {
		t.Fatalf("bob should join when Access is nil")
	}
}
