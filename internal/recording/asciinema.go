package recording

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// RecordInput records stdin input (call when bytes are sent to the
// target). Disabled by default because raw stdin can capture sudo
// passwords and other interactive secrets (CWE-532). Set
// VANTYX_RECORD_INPUT=1 to opt in for environments that need
// keystroke-level forensics.
func (a *AsciinemaWriter) RecordInput(p []byte) {
	if len(p) == 0 {
		return
	}
	if os.Getenv("VANTYX_RECORD_INPUT") != "1" {
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

// RecordResize records a terminal resize ("r") event so playback can
// replay the session at the terminal geometry it actually ran at. Without
// this, output written for a size that differs from the cast header's
// fixed width/height (e.g. after a client resizes mid-session) is
// misinterpreted by the player: full-screen redraws from curses apps like
// vim can come out with wrapping/cursor-position collapsed onto a single
// line once the size drifts from what the header declared.
// See https://github.com/asciinema/asciinema/blob/main/doc/asciicast-v2.md#r-event
func (a *AsciinemaWriter) RecordResize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.writeHeader(); err != nil {
		return
	}
	ts := time.Since(a.start).Seconds()
	line, err := json.Marshal([]interface{}{ts, "r", fmt.Sprintf("%dx%d", cols, rows)})
	if err != nil {
		return
	}
	_, _ = a.w.Write(append(line, '\n'))
}

// StartedAt returns the recording start time.
func (a *AsciinemaWriter) StartedAt() time.Time {
	return a.start
}
