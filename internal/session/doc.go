// Package session manages interactive terminal sessions that survive
// the browser tab being closed.
//
// [Manager] owns the in-memory registry, multiplexes input and output
// across browser reconnects, and emits idle-warning events when no
// activity has been seen for the configured threshold.
//
// The HTTP layer is responsible for authorisation and recording; this
// package is intentionally protocol-agnostic so SSH, Telnet, and the
// CLI gateway can share the same session lifecycle.
package session
