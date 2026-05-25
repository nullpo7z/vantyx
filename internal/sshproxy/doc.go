// Package sshproxy bridges a browser PTY (delivered over a WebSocket
// in [internal/httpapi]) or a CLI gateway PTY (in [internal/sshd]) to
// a remote SSH server.
//
// It owns the lifecycle of the upstream [golang.org/x/crypto/ssh]
// client, manages window-size (NAWS) updates, and surfaces
// authentication failures via the [internal/proxyerrors] helpers so
// the UI can show actionable messages.
package sshproxy
