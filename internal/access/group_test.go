package access

import (
	"testing"
)

func TestInMemoryAccessGroupStore_CreateAndMembership(t *testing.T) {
	store := NewInMemoryAccessGroupStore()

	g, err := store.Create("g1", "ops")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if g.ID != "g1" || g.Name != "ops" {
		t.Fatalf("unexpected group: %+v", g)
	}

	err = store.AddUserToGroup("user1", "g1")
	if err != nil {
		t.Fatalf("AddUserToGroup returned error: %v", err)
	}
	err = store.AddTargetToGroup("g1", "target1")
	if err != nil {
		t.Fatalf("AddTargetToGroup returned error: %v", err)
	}

	ids := store.TargetIDsForUser("user1")
	if len(ids) != 1 || ids[0] != "target1" {
		t.Fatalf("expected [target1], got %v", ids)
	}

	ids = store.TargetIDsForUser("unknown")
	if len(ids) != 0 {
		t.Fatalf("expected nil/empty for unknown user, got %v", ids)
	}
}

func TestInMemoryAccessGroupStore_TargetIDsForUser_Dedup(t *testing.T) {
	store := NewInMemoryAccessGroupStore()

	_, _ = store.Create("g1", "ops")
	_, _ = store.Create("g2", "dev")
	_ = store.AddUserToGroup("u1", "g1")
	_ = store.AddUserToGroup("u1", "g2")
	_ = store.AddTargetToGroup("g1", "t1")
	_ = store.AddTargetToGroup("g2", "t1")
	_ = store.AddTargetToGroup("g2", "t2")

	ids := store.TargetIDsForUser("u1")
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique targets, got %v", ids)
	}
}

func TestInMemoryAccessGroupStore_AddUserToGroup_UnknownGroup(t *testing.T) {
	store := NewInMemoryAccessGroupStore()

	err := store.AddUserToGroup("u1", "missing")
	if err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}
