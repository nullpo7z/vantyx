// Package filetransfer manages background upload and download jobs.
//
// Jobs flow through three phases: receiving (initial buffering on the
// HTTP request goroutine), running (the actual transfer to the remote
// target), and completed | failed (terminal states). The [Manager]
// keeps an in-memory registry and exposes the helpers used by the
// HTTP API to list, abort, and reap jobs.
//
// State is intentionally non-persistent: a server restart cancels
// in-flight jobs and [Manager.ReapOrphans] cleans up any leftover
// metadata on startup.
package filetransfer
