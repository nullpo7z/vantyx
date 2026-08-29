package httpapi

import (
	"context"
	"time"

	"github.com/nullpo7z/vantyx/internal/logging"
)

// auditFields is the loose-typed payload accepted by [audit].
//
// Values may be any type that [fmt.Sprint] can handle. Prefer the
// canonical keys defined in [internal/logging] so log queries stay
// consistent across the codebase.
type auditFields map[string]interface{}

// audit records a structured audit event.
//
// The event is:
//
//   - Appended to the in-memory ring buffer that powers the audit UI.
//   - Forwarded to the registered [logging.AuditSink] (which, in this
//     package, fans out to the DB + log file + optional syslog forwarder).
//   - Emitted as a structured slog record so it is visible in normal
//     log output even when no sink is configured.
//
// Both the buffer and the sink receive the same [AuditEntry] with the
// event name copied into the fields map.
func audit(event string, fields auditFields) {
	if fields == nil {
		fields = auditFields{}
	}
	observeAuditMetrics(event)

	// Append to the in-memory ring used by the audit UI.
	if auditBuffer != nil {
		copied := auditFields{}
		for k, v := range fields {
			copied[k] = v
		}
		copied["event"] = event
		auditBuffer.add(AuditEntry{
			Time:   time.Now(), // server zone (VANTYX_TIMEZONE)
			Event:  event,
			Fields: copied,
		})
	}

	// Fan out to slog and the registered persistent sink (if any).
	logging.Audit(context.Background(), event, map[string]any(fields))
}

// auditBuffer stores recent audit events for the audit log UI. It is
// best-effort, in-memory, and bounded.
var auditBuffer = newAuditStore(2000)
