package logging

import (
	"context"
	"log/slog"
)

// Canonical attribute keys used across Vantyx. New events should reuse
// these whenever the data is a good match so log queries stay consistent.
const (
	KeyEvent       = "event"
	KeyComponent   = "component"
	KeyUserID      = "user_id"
	KeyUsername    = "username"
	KeyTargetID    = "target_id"
	KeySessionID   = "session_id"
	KeyProtocol    = "protocol"
	KeyMethod      = "method"
	KeyPath        = "path"
	KeyStatus      = "status"
	KeyRemote      = "remote"
	KeyDurationMS  = "duration_ms"
	KeyQuery       = "query"
	KeyUserAgent   = "user_agent"
	KeyContentType = "content_type"
	KeyError       = "error"
)

// Default returns the process-wide [slog.Logger]. It is a thin wrapper
// around [slog.Default] kept for symmetry with [WithComponent].
func Default() *slog.Logger {
	return slog.Default()
}

// WithComponent returns a logger that adds a "component" attribute to
// every record. Example: logger := logging.WithComponent("httpapi.router").
func WithComponent(name string) *slog.Logger {
	return slog.Default().With(slog.String(KeyComponent, name))
}

// AuditEvent is the protocol-independent shape of an audit log entry.
//
// The Fields map carries arbitrary string-keyed metadata. Callers should
// prefer the canonical keys defined in this package; non-canonical keys
// are accepted but make queries harder.
type AuditEvent struct {
	// Event is a short snake_case name describing what happened
	// (for example "login_success", "terminal_session_start").
	Event string

	// Fields is the structured payload attached to the event. Values are
	// rendered with %v by [slog]'s text handler and serialised verbatim by
	// the JSON handler.
	Fields map[string]any
}

// AuditSink receives audit events after they have been logged through
// [slog]. Implementations are expected to be cheap and non-blocking; if
// they need to perform I/O they should buffer internally.
type AuditSink interface {
	Write(ctx context.Context, evt AuditEvent)
}

// Audit is a high-level helper that emits a structured audit log line
// AND forwards the event to the registered [AuditSink] (if any).
//
// The slog record is always written at info level under
// [Default] with the canonical "event" key, so audit lines are visible
// in normal log output even when no persistent sink is configured.
func Audit(ctx context.Context, event string, fields map[string]any) {
	attrs := make([]any, 0, len(fields)+1)
	attrs = append(attrs, slog.String(KeyEvent, event))
	for k, v := range fields {
		attrs = append(attrs, slog.Any(k, v))
	}
	slog.Default().LogAttrs(ctx, slog.LevelInfo, "audit", toAttrs(attrs)...)

	if sink := loadSink(); sink != nil {
		sink.Write(ctx, AuditEvent{Event: event, Fields: fields})
	}
}

func toAttrs(in []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(in))
	for _, a := range in {
		if attr, ok := a.(slog.Attr); ok {
			out = append(out, attr)
		}
	}
	return out
}
