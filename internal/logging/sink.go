package logging

import "sync/atomic"

// auditSink holds the registered sink. Stored as atomic.Value to allow
// late registration from packages that initialise after process start
// (for example httpapi.NewApp opens the database before it can wire up
// the audit_logs table).
var auditSink atomic.Pointer[sinkHolder]

type sinkHolder struct{ s AuditSink }

// RegisterAuditSink installs the process-wide audit sink. The previous
// sink, if any, is replaced. Passing a nil sink resets the registration.
//
// Most callers should register exactly once during startup. Tests may
// register a capturing sink and reset it on teardown.
func RegisterAuditSink(s AuditSink) {
	if s == nil {
		auditSink.Store(nil)
		return
	}
	auditSink.Store(&sinkHolder{s: s})
}

// loadSink returns the currently registered sink, or nil if none is set.
func loadSink() AuditSink {
	h := auditSink.Load()
	if h == nil {
		return nil
	}
	return h.s
}
