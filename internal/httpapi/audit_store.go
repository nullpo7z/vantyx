package httpapi

import (
	"sync"
	"time"
)

// AuditEntry is one structured audit event kept for UI display.
type AuditEntry struct {
	Time   time.Time   `json:"time"`
	Event  string      `json:"event"`
	Fields auditFields `json:"fields"`
}

// auditStore keeps a bounded in-memory ring buffer of recent audit entries.
// It is best-effort for UI/debugging; it is not intended to be a durable audit trail.
type auditStore struct {
	mu    sync.Mutex
	max   int
	items []AuditEntry
}

func newAuditStore(max int) *auditStore {
	if max <= 0 {
		max = 1000
	}
	return &auditStore{max: max}
}

func (s *auditStore) add(e AuditEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.max <= 0 {
		return
	}
	s.items = append(s.items, e)
	if len(s.items) > s.max {
		// drop oldest
		s.items = append([]AuditEntry(nil), s.items[len(s.items)-s.max:]...)
	}
}

func (s *auditStore) listNewestFirst(limit int, filter func(AuditEntry) bool) []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	out := make([]AuditEntry, 0, limit)
	for i := len(s.items) - 1; i >= 0 && len(out) < limit; i-- {
		e := s.items[i]
		if filter != nil && !filter(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}
