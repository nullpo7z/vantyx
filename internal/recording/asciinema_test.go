package recording

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestAsciinemaWriter_WriteAndRecordInput(t *testing.T) {
	var buf bytes.Buffer
	w := NewAsciinemaWriter(&buf, 80, 24)

	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w.RecordInput([]byte("input\r\n"))
	if _, err := w.Write([]byte("world\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 events), got %d", len(lines))
	}
	var header struct {
		Version int `json:"version"`
		Width   int `json:"width"`
		Height  int `json:"height"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("header JSON: %v", err)
	}
	if header.Version != 2 || header.Width != 80 || header.Height != 24 {
		t.Fatalf("header: %+v", header)
	}
	var event []interface{}
	if err := json.Unmarshal([]byte(lines[1]), &event); err != nil {
		t.Fatalf("event 1: %v", err)
	}
	if len(event) != 3 || event[1] != "o" {
		t.Fatalf("event 1: %v", event)
	}
	if err := json.Unmarshal([]byte(lines[2]), &event); err != nil {
		t.Fatalf("event 2: %v", err)
	}
	if len(event) != 3 || event[1] != "i" {
		t.Fatalf("event 2 (input): %v", event)
	}
}

func TestAsciinemaWriter_ZeroSizeUsesDefault(t *testing.T) {
	var buf bytes.Buffer
	w := NewAsciinemaWriter(&buf, 0, 0)
	if w.width != 80 || w.height != 24 {
		t.Fatalf("expected default 80x24, got %dx%d", w.width, w.height)
	}
}

func TestAsciinemaWriter_StartedAt(t *testing.T) {
	var buf bytes.Buffer
	w := NewAsciinemaWriter(&buf, 80, 24)
	t0 := w.StartedAt()
	if t0.IsZero() {
		t.Fatal("StartedAt should be non-zero")
	}
}

func TestAsciinemaWriter_WriteEmptyNoHeader(t *testing.T) {
	var buf bytes.Buffer
	w := NewAsciinemaWriter(&buf, 80, 24)
	n, err := w.Write(nil)
	if err != nil || n != 0 {
		t.Fatalf("Write(nil): n=%d err=%v", n, err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty Write, got %d bytes", buf.Len())
	}
}

func TestAsciinemaWriter_RecordInputEmpty(t *testing.T) {
	var buf bytes.Buffer
	w := NewAsciinemaWriter(&buf, 80, 24)
	w.RecordInput(nil)
	if buf.Len() != 0 {
		t.Fatalf("RecordInput(nil) should not write, got %d bytes", buf.Len())
	}
}

// TestAsciinemaWriter_SecondWriteSkipsHeader ensures the second Write uses headerOk path.
func TestAsciinemaWriter_SecondWriteSkipsHeader(t *testing.T) {
	var buf bytes.Buffer
	w := NewAsciinemaWriter(&buf, 80, 24)
	if _, err := w.Write([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("second")); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 events), got %d", len(lines))
	}
}

// errWriter is an io.Writer that always returns the given error.
type errWriter struct{ err error }

func (e *errWriter) Write([]byte) (int, error) { return 0, e.err }

// limitWriter accepts up to limit bytes then returns errAfter.
type limitWriter struct {
	w        io.Writer
	limit    int
	written  int
	errAfter error
}

func (l *limitWriter) Write(p []byte) (n int, err error) {
	if l.written >= l.limit {
		return 0, l.errAfter
	}
	allow := l.limit - l.written
	if allow > len(p) {
		allow = len(p)
	}
	n, err = l.w.Write(p[:allow])
	if err != nil {
		return n, err
	}
	l.written += n
	if l.written >= l.limit {
		return n, l.errAfter
	}
	return n, nil
}

// nthCallErrWriter delegates to w until the failOn-th Write (1-indexed)
// where it returns errAfter without writing. Used by tests that need to
// distinguish "first Write succeeds" from "second Write fails" without
// relying on byte counts (which are timestamp-dependent in JSON output).
type nthCallErrWriter struct {
	w        io.Writer
	failOn   int
	count    int
	errAfter error
}

func (n *nthCallErrWriter) Write(p []byte) (int, error) {
	n.count++
	if n.count >= n.failOn {
		return 0, n.errAfter
	}
	return n.w.Write(p)
}

func TestAsciinemaWriter_WriteHeaderWriteError(t *testing.T) {
	wantErr := errors.New("header write failed")
	w := NewAsciinemaWriter(&errWriter{err: wantErr}, 80, 24)
	_, err := w.Write([]byte("x"))
	if err != wantErr {
		t.Fatalf("Write: got err %v", err)
	}
}

func TestAsciinemaWriter_WriteEventWriteError(t *testing.T) {
	wantErr := errors.New("event write failed")
	var buf bytes.Buffer
	// First Write call writes the header (succeeds); the second Write
	// call would write the event line and must fail. Counting calls
	// rather than bytes avoids flakiness when the JSON length of the
	// event line varies with the floating-point timestamp.
	w := NewAsciinemaWriter(&nthCallErrWriter{w: &buf, failOn: 2, errAfter: wantErr}, 80, 24)
	_, err := w.Write([]byte("event"))
	if err != wantErr {
		t.Fatalf("Write: got err %v", err)
	}
}

func TestAsciinemaWriter_RecordInputWriteError(t *testing.T) {
	wantErr := errors.New("record write failed")
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, limit: 200, errAfter: wantErr}
	w := NewAsciinemaWriter(lw, 80, 24)
	_, _ = w.Write([]byte("out"))
	w.RecordInput([]byte("in"))
	// RecordInput ignores errors; we just ensure the code path runs (coverage).
	if buf.Len() == 0 {
		t.Fatal("expected some output before failure")
	}
}
