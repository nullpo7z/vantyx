package access

import (
	"testing"
)

func TestInMemoryTargetStore_CreateAndGet(t *testing.T) {
	store := NewInMemoryTargetStore()

	target, err := store.Create("t1", "router1", "192.168.1.1", 22, ProtocolSSH)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if target.ID != "t1" || target.Name != "router1" || target.Host != "192.168.1.1" || target.Port != 22 || target.Protocol != ProtocolSSH {
		t.Fatalf("unexpected target: %+v", target)
	}

	got, err := store.Get("t1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.ID != target.ID {
		t.Fatalf("Get returned wrong target: %+v", got)
	}

	_, err = store.Get("missing")
	if err != ErrTargetNotFound {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestInMemoryTargetStore_Create_Duplicate(t *testing.T) {
	store := NewInMemoryTargetStore()

	_, err := store.Create("t1", "r1", "host", 22, ProtocolSSH)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	_, err = store.Create("t1", "r2", "host2", 23, ProtocolTelnet)
	if err != ErrTargetExists {
		t.Fatalf("expected ErrTargetExists, got %v", err)
	}
}

func TestInMemoryTargetStore_ListByIDs(t *testing.T) {
	store := NewInMemoryTargetStore()

	_, _ = store.Create("t1", "r1", "h1", 22, ProtocolSSH)
	_, _ = store.Create("t2", "r2", "h2", 23, ProtocolTelnet)

	list := store.ListByIDs([]string{"t1", "t2", "missing", "t1"})
	if len(list) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(list))
	}
}
