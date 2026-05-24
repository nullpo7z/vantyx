package httpapi

import "testing"

func TestAuditStore_ListNewestFirstFilters(t *testing.T) {
	s := newAuditStore(5)
	s.add(AuditEntry{Event: "a", Fields: auditFields{"user_id": "u1"}})
	s.add(AuditEntry{Event: "b", Fields: auditFields{"user_id": "u2"}})
	s.add(AuditEntry{Event: "c", Fields: auditFields{"user_id": "u1"}})

	items := s.listNewestFirst(10, 0, func(e AuditEntry) bool {
		return e.Fields["user_id"] == "u1"
	})
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
	if items[0].Event != "c" || items[1].Event != "a" {
		t.Fatalf("unexpected order: %+v", items)
	}
}

func TestAuditStore_ListNewestFirstAfterID(t *testing.T) {
	s := newAuditStore(10)
	for i := 0; i < 5; i++ {
		s.add(AuditEntry{Event: "e", Fields: auditFields{"n": i}})
	}
	all := s.listNewestFirst(10, 0, nil)
	if len(all) != 5 {
		t.Fatalf("expected 5, got %d", len(all))
	}
	cursor := all[2].ID
	page2 := s.listNewestFirst(10, cursor, nil)
	if len(page2) != 2 {
		t.Fatalf("expected 2 older items, got %d", len(page2))
	}
	if page2[0].ID >= cursor || page2[1].ID >= cursor {
		t.Fatalf("after_id not applied: cursor=%d items=%+v", cursor, page2)
	}
}

func TestAuditNextCursor(t *testing.T) {
	items := []AuditEntry{{ID: 3}, {ID: 2}, {ID: 1}}
	if got := auditNextCursor(items, 2); got != "2" {
		t.Fatalf("got %q", got)
	}
	if got := auditNextCursor(items, 3); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
