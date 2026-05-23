package sshd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

func TestCLISessionBarLayout_order(t *testing.T) {
	layout := buildCLISessionBarLayout(cliSessionBarState{
		TargetName: "claude",
		Protocol:   access.ProtocolSSH,
		Cols:       40,
	})
	if len(layout.lines) != 4 {
		t.Fatalf("got %d lines: %v", len(layout.lines), layout.lines)
	}
	if !strings.HasPrefix(layout.lines[0], "Connected to: claude") {
		t.Fatalf("line0: %q", layout.lines[0])
	}
	if !strings.Contains(layout.lines[1], "Detach") {
		t.Fatalf("line1: %q", layout.lines[1])
	}
	if !strings.Contains(layout.lines[2], "End session") {
		t.Fatalf("line2: %q", layout.lines[2])
	}
	if len(layout.lines[3]) != 40 {
		t.Fatalf("separator width: %d", len(layout.lines[3]))
	}
}

func TestCLISessionFrame_EnterSetsScrollRegion(t *testing.T) {
	var buf bytes.Buffer
	frame := newCLISessionFrame(&buf, 80, 24)
	bar := cliSessionBarState{
		TargetName: "sw1",
		Protocol:   access.ProtocolSSH,
		Cols:       80,
	}
	if err := frame.Enter(bar); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "PWD:") || strings.Contains(out, "Groups") {
		t.Fatalf("session bar should not include menu header: %q", out)
	}
	if !strings.Contains(out, "Connected to: sw1 [ssh]") {
		t.Fatalf("missing connected line: %q", out)
	}
	if !strings.Contains(out, "Detach (keep session)") || !strings.Contains(out, "End session") {
		t.Fatalf("missing disconnect hints: %q", out)
	}
	if !strings.Contains(out, "\033[") || !strings.Contains(out, "r") {
		t.Fatalf("missing DECSTBM: %q", out)
	}
	if frame.SessionRows() >= 24 {
		t.Fatalf("session rows should be less than full terminal: %d", frame.SessionRows())
	}
	if err := frame.Leave(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\033[r") {
		t.Fatal("Leave should reset scroll region")
	}
}

func TestCLISessionFrame_ResizeUpdatesSessionRows(t *testing.T) {
	var buf bytes.Buffer
	frame := newCLISessionFrame(&buf, 80, 30)
	bar := cliSessionBarState{
		TargetName: "sw1",
		Protocol:   access.ProtocolTelnet,
		Cols:       80,
	}
	_ = frame.Enter(bar)
	before := frame.SessionRows()
	_ = frame.Resize(100, 40)
	after := frame.SessionRows()
	if after <= before {
		t.Fatalf("resize should increase session rows: before=%d after=%d", before, after)
	}
}
