// Package telnetproxy bridges a browser PTY (delivered over a
// WebSocket in [internal/httpapi]) or a CLI gateway PTY (in
// [internal/sshd]) to a remote Telnet server.
//
// It implements the subset of the Telnet protocol Vantyx cares about:
// NAWS for terminal resize, optional wake-on-connect bytes to nudge
// dormant servers (toggleable via VANTYX_TELNET_WAKE_ON_CONNECT), and
// stored-credential autologin. Errors are surfaced via
// [internal/proxyerrors] so the UI can show actionable messages.
package telnetproxy
