package httpapi

import "testing"

func TestAuditStore_ListNewestFirstFilters(t *testing.T) {
	s := newAuditStore(5)
	s.add(AuditEntry{Event: "a", Fields: auditFields{"user_id": "u1"}})
	s.add(AuditEntry{Event: "b", Fields: auditFields{"user_id": "u2"}})
	s.add(AuditEntry{Event: "c", Fields: auditFields{"user_id": "u1"}})

	items := s.listNewestFirst(10, func(e AuditEntry) bool {
		return e.Fields["user_id"] == "u1"
	})
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
	if items[0].Event != "c" || items[1].Event != "a" {
		t.Fatalf("unexpected order: %+v", items)
	}
}

