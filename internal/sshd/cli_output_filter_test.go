package sshd

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIOutputFilter_clampsCursorUp(t *testing.T) {
	var buf bytes.Buffer
	f := newCLIOutputFilter(&buf, 5)
	if _, err := f.Write([]byte("hello\x1b[1;1Hworld")); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("output: %q", out)
	}
	if strings.Contains(out, "\x1b[1;1H") {
		t.Fatalf("cursor to row 1 should be rewritten: %q", out)
	}
}

func TestCLIOutputFilter_fullClearRedraws(t *testing.T) {
	var buf bytes.Buffer
	redrawn := false
	f := newCLIOutputFilter(&buf, 3)
	f.onFullClear = func() error {
		redrawn = true
		_, err := buf.WriteString("HEADER-REDREW\r\n")
		return err
	}
	if _, err := f.Write([]byte("\x1b[2J")); err != nil {
		t.Fatal(err)
	}
	if !redrawn {
		t.Fatal("expected header redraw on full clear")
	}
	if !strings.Contains(buf.String(), "HEADER-REDREW") {
		t.Fatalf("output: %q", buf.String())
	}
}
