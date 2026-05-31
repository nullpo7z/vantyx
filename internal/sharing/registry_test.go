package sharing

import (
	"errors"
	"testing"
	"time"
)

func TestRegistry_EnsureRoomIdempotent(t *testing.T) {
	reg := NewRegistry()
	r1 := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	r2 := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	if r1 != r2 {
		t.Fatalf("EnsureRoom must return the same instance for the same session id")
	}
	if r1.OwnerID() != "alice" {
		t.Fatalf("owner id mismatch: %s", r1.OwnerID())
	}
	if r1.WriterID() != "alice" {
		t.Fatalf("initial writer must be the owner, got %s", r1.WriterID())
	}
	if got := r1.Participants(); len(got) != 1 || got[0].UserID != "alice" {
		t.Fatalf("owner missing from participant list: %+v", got)
	}
}

func TestRoom_AddViewerAndRemove(t *testing.T) {
	reg := NewRegistry()
	room := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	now := time.Now().UTC()
	room.AddViewer("bob", "Bob", now)
	room.AddViewer("carol", "Carol", now.Add(time.Second))
	if !room.IsParticipant("bob") || !room.IsParticipant("carol") {
		t.Fatalf("viewers not attached")
	}
	if got := room.Participants(); len(got) != 3 {
		t.Fatalf("want 3 participants, got %d", len(got))
	}
	if err := room.RemoveParticipant("bob"); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
	if room.IsParticipant("bob") {
		t.Fatalf("bob should be gone after kick")
	}
	if err := room.RemoveParticipant("alice"); err != ErrCannotKickOwner {
		t.Fatalf("owner kick must fail with ErrCannotKickOwner, got %v", err)
	}
}

func TestRoom_WriteRequestGrant(t *testing.T) {
	reg := NewRegistry()
	room := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	now := time.Now().UTC()
	room.AddViewer("bob", "Bob", now)

	if _, err := room.RequestWrite("req-alice", "alice", "Alice", now); err != ErrAlreadyWriter {
		t.Fatalf("alice already holds the token; want ErrAlreadyWriter, got %v", err)
	}
	wr, err := room.RequestWrite("req1", "bob", "Bob", now)
	if err != nil {
		t.Fatalf("RequestWrite: %v", err)
	}
	if wr.Status != WriteRequestPending {
		t.Fatalf("status=%s", wr.Status)
	}
	// Only the current writer can decide.
	if _, _, err := room.GrantWrite("req1", "bob", now); err != ErrNotWriter {
		t.Fatalf("non-writer must not grant, got %v", err)
	}
	wr2, prev, err := room.GrantWrite("req1", "alice", now.Add(time.Second))
	if err != nil {
		t.Fatalf("GrantWrite: %v", err)
	}
	if wr2.Status != WriteRequestGranted {
		t.Fatalf("status=%s", wr2.Status)
	}
	if prev != "alice" {
		t.Fatalf("previous writer must be alice, got %s", prev)
	}
	if room.WriterID() != "bob" {
		t.Fatalf("writer not transferred, got %s", room.WriterID())
	}
}

func TestRoom_WriteRequestDeny(t *testing.T) {
	reg := NewRegistry()
	room := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	now := time.Now().UTC()
	room.AddViewer("bob", "Bob", now)
	wr, err := room.RequestWrite("req1", "bob", "Bob", now)
	if err != nil {
		t.Fatalf("RequestWrite: %v", err)
	}
	if _, err := room.DenyWrite(wr.ID, "alice", now.Add(time.Second)); err != nil {
		t.Fatalf("DenyWrite: %v", err)
	}
	if room.WriterID() != "alice" {
		t.Fatalf("writer should still be alice")
	}
	if _, err := room.DenyWrite(wr.ID, "alice", now.Add(2*time.Second)); err != ErrRequestNotPending {
		t.Fatalf("second decision must fail with ErrRequestNotPending, got %v", err)
	}
}

func TestRoom_ReleaseWrite(t *testing.T) {
	reg := NewRegistry()
	room := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	now := time.Now().UTC()
	room.AddViewer("bob", "Bob", now)
	wr, _ := room.RequestWrite("req1", "bob", "Bob", now)
	if _, _, err := room.GrantWrite(wr.ID, "alice", now); err != nil {
		t.Fatalf("GrantWrite: %v", err)
	}
	previous, err := room.ReleaseWrite("bob")
	if err != nil {
		t.Fatalf("ReleaseWrite: %v", err)
	}
	if previous != "bob" {
		t.Fatalf("previous=%s", previous)
	}
	if room.WriterID() != "alice" {
		t.Fatalf("writer should reset to owner, got %s", room.WriterID())
	}
}

func TestRoom_KickReclaimsWriteToken(t *testing.T) {
	reg := NewRegistry()
	room := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	now := time.Now().UTC()
	room.AddViewer("bob", "Bob", now)
	wr, _ := room.RequestWrite("req1", "bob", "Bob", now)
	if _, _, err := room.GrantWrite(wr.ID, "alice", now); err != nil {
		t.Fatalf("GrantWrite: %v", err)
	}
	if err := room.RemoveParticipant("bob"); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
	if room.WriterID() != "alice" {
		t.Fatalf("write token must be returned to the owner after kick, got %s", room.WriterID())
	}
}

func TestRoom_KickedUserCannotRejoin(t *testing.T) {
	reg := NewRegistry()
	room := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	now := time.Now().UTC()
	_ = room.AddViewer("bob", "Bob", now)
	if err := room.RemoveParticipant("bob"); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
	if err := room.AddViewer("bob", "Bob", now); !errors.Is(err, ErrUserKicked) {
		t.Fatalf("expected ErrUserKicked, got %v", err)
	}
}

func TestRegistry_RoomsForUser(t *testing.T) {
	reg := NewRegistry()
	r1 := reg.EnsureRoom("s1", "t1", "alice", "Alice")
	r2 := reg.EnsureRoom("s2", "t2", "alice", "Alice")
	r1.AddViewer("bob", "Bob", time.Now().UTC())
	rooms := reg.RoomsForUser("bob")
	if len(rooms) != 1 || rooms[0] != "s1" {
		t.Fatalf("unexpected rooms for bob: %v", rooms)
	}
	rooms = reg.RoomsForUser("alice")
	if len(rooms) != 2 {
		t.Fatalf("alice owns two rooms, got %v", rooms)
	}
	_ = r2
}
