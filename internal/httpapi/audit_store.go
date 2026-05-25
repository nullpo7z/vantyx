package httpapi

import (
	"strconv"
	"sync"
	"time"
)

// AuditEntry is one structured audit event kept for UI display.
type AuditEntry struct {
	ID     int64       `json:"id,omitempty"`
	Time   time.Time   `json:"time"`
	Event  string      `json:"event"`
	Fields auditFields `json:"fields"`
}

// auditStore keeps a bounded in-memory ring buffer of recent audit entries.
// It is best-effort for UI/debugging; it is not intended to be a durable audit trail.
type auditStore struct {
	mu     sync.Mutex
	max    int
	nextID int64
	items  []AuditEntry
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
	s.nextID++
	e.ID = s.nextID
	s.items = append(s.items, e)
	if len(s.items) > s.max {
		s.items = append([]AuditEntry(nil), s.items[len(s.items)-s.max:]...)
	}
}

// listNewestFirst returns matching entries newest-first. afterID>0 skips rows with id >= afterID.
// limit is the page size; up to limit+1 rows may be collected internally by the caller.
func (s *auditStore) listNewestFirst(limit int, afterID int64, filter func(AuditEntry) bool) []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	fetch := limit + 1
	out := make([]AuditEntry, 0, fetch)
	for i := len(s.items) - 1; i >= 0 && len(out) < fetch; i-- {
		e := s.items[i]
		if afterID > 0 && e.ID >= afterID {
			continue
		}
		if filter != nil && !filter(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func auditNextCursor(items []AuditEntry, pageLimit int) string {
	if len(items) <= pageLimit || pageLimit <= 0 {
		return ""
	}
	return strconv.FormatInt(items[pageLimit-1].ID, 10)
}

func trimAuditPage(items []AuditEntry, pageLimit int) []AuditEntry {
	if len(items) > pageLimit && pageLimit > 0 {
		return items[:pageLimit]
	}
	return items
}
