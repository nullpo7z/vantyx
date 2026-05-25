// Package logging centralises the slog-based logger configuration and the
// audit event sink used by every Vantyx subsystem.
//
// Use [Default] for general structured logs, [WithComponent] when a single
// subsystem wants to tag every record it emits with a component name, and
// [Audit] to record security-relevant events that should also be persisted
// to the audit store.
//
// The package is intentionally light on policy: it does not configure log
// levels or output destinations on its own. [cmd/vantyx-server] (or any
// other entrypoint) is responsible for installing a [slog.Handler] via
// [slog.SetDefault] before the application starts.
//
// Recommended attribute names live in docs/development.md (logging keys).
// Reuse them where applicable so log queries stay consistent.
package logging
