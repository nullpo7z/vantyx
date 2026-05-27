// Package sharing implements collaborative session participation for
// Vantyx terminal (SSH/Telnet) sessions.
//
// The package owns:
//
//   - In-memory rooms: each running terminal session can have an owner,
//     additional viewers, and exactly one writer at a time. State is
//     held in [Registry] / [Room] / [Participant].
//   - Persistent invitations: [Store] writes invitation rows (token
//     hash, expiry, mode) into SQLite so an owner can hand out a join
//     URL to another Vantyx user. Tokens themselves are never stored;
//     only their SHA-256 digest is.
//   - Write-token request/grant flow: a viewer can ask the current
//     writer for control via [Room.RequestWrite] and the writer either
//     grants or denies it via [Room.GrantWrite] / [Room.DenyWrite].
//
// The HTTP layer in internal/httpapi composes these primitives with
// access control, audit logging, and SSE notifications.
package sharing
