package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
)

// CaptureBuffer is a [slog.Handler] backed by an in-memory buffer. It is
// intended for tests that want to assert against log output without
// touching global state any more than necessary.
//
// Lines are captured at info level by default; pass a different level to
// [NewCaptureBuffer] to widen the filter.
type CaptureBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	handler slog.Handler
}

// NewCaptureBuffer constructs a buffer-backed handler at the given level.
func NewCaptureBuffer(level slog.Level) *CaptureBuffer {
	c := &CaptureBuffer{}
	c.handler = slog.NewTextHandler(&c.buf, &slog.HandlerOptions{Level: level})
	return c
}

// Handler returns the underlying [slog.Handler] so callers can wrap it in
// a [slog.Logger].
func (c *CaptureBuffer) Handler() slog.Handler { return c }

// Enabled implements [slog.Handler].
func (c *CaptureBuffer) Enabled(ctx context.Context, level slog.Level) bool {
	return c.handler.Enabled(ctx, level)
}

// Handle implements [slog.Handler]. It serialises the record and stores it.
func (c *CaptureBuffer) Handle(ctx context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handler.Handle(ctx, r)
}

// WithAttrs implements [slog.Handler].
func (c *CaptureBuffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return c.handler.WithAttrs(attrs)
}

// WithGroup implements [slog.Handler].
func (c *CaptureBuffer) WithGroup(name string) slog.Handler {
	return c.handler.WithGroup(name)
}

// String returns the full buffered output, including newlines.
func (c *CaptureBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Contains is a convenience for tests: it reports whether the buffered
// output contains needle.
func (c *CaptureBuffer) Contains(needle string) bool {
	return strings.Contains(c.String(), needle)
}

// CapturingAuditSink is a thread-safe in-memory [AuditSink] for tests.
type CapturingAuditSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

// Write stores the event.
func (c *CapturingAuditSink) Write(_ context.Context, evt AuditEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, evt)
}

// Events returns a snapshot of every event written so far.
func (c *CapturingAuditSink) Events() []AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]AuditEvent, len(c.events))
	copy(out, c.events)
	return out
}

// Reset removes every stored event.
func (c *CapturingAuditSink) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}
