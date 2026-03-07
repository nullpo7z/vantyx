package recording

import (
	"bytes"
	"encoding/json"
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
