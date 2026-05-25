// Package httpapi implements the Vantyx HTTP application: REST
// endpoints, WebSocket upgrades, middleware, and the audit pipeline.
//
// The package is organised by concern, not by URL:
//
//   - [App] holds long-lived dependencies and is constructed via
//     [NewApp]. Tests can build a fully wired instance with [NewTestApp].
//   - [App.NewRouter] returns the [chi]-based mux that powers both the
//     browser SPA and the CLI gateway.
//   - middleware.go centralises CSRF / CORS / max-body / panic-recovery.
//   - auth_helpers.go owns the canonical access-control helpers used by
//     every target-scoped handler.
//   - logging.go and audit_sink.go bridge slog and the persistent audit
//     log used by the audit UI and the optional SIEM forwarder.
//
// Domain-specific handlers (terminals, files, recordings, RDP, VNC,
// TFTP, etc.) live in their own files. Routes are registered in
// routes.go.
package httpapi
