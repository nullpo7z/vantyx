package recording

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// AsciinemaWriter writes asciinema v2 format (newline-delimited JSON).
// See https://github.com/asciinema/asciinema/blob/main/doc/asciicast-v2.md
type AsciinemaWriter struct {
	mu       sync.Mutex
	w        io.Writer
	start    time.Time
	width    int
	height   int
	headerOk bool
}

// NewAsciinemaWriter creates a writer that emits asciinema v2 events to w.
func NewAsciinemaWriter(w io.Writer, width, height int) *AsciinemaWriter {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return &AsciinemaWriter{
		w:      w,
		start:  time.Now(),
		width:  width,
		height: height,
	}
}

func (a *AsciinemaWriter) writeHeader() error {
	if a.headerOk {
		return nil
	}
	header := map[string]interface{}{
		"version":   2,
		"width":     a.width,
		"height":    a.height,
		"timestamp": a.start.Unix(),
	}
	b, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := a.w.Write(append(b, '\n')); err != nil {
		return err
	}
	a.headerOk = true
	return nil
}

// Write implements io.Writer for stdout output (RecordOutput).
func (a *AsciinemaWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.writeHeader(); err != nil {
		return 0, err
	}
	ts := time.Since(a.start).Seconds()
	line, err := json.Marshal([]interface{}{ts, "o", string(p)})
	if err != nil {
		return 0, err
	}
	if _, err := a.w.Write(append(line, '\n')); err != nil {
		return 0, err
	}
	return len(p), nil
}

// RecordInput records stdin input (call when bytes are sent to the target).
func (a *AsciinemaWriter) RecordInput(p []byte) {
	if len(p) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.writeHeader()
	ts := time.Since(a.start).Seconds()
	line, err := json.Marshal([]interface{}{ts, "i", string(p)})
	if err != nil {
		return
	}
	_, _ = a.w.Write(append(line, '\n'))
}

// StartedAt returns the recording start time.
func (a *AsciinemaWriter) StartedAt() time.Time {
	return a.start
}
