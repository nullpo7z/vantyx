package sshd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
)

func TestFormatCLIColumns_fourColumns(t *testing.T) {
	items := []string{"1: A", "2: B", "3: C", "4: D", "5: E"}
	lines := formatCLIColumns(items, 4)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "1: A") || !strings.Contains(lines[0], "2: B") {
		t.Fatalf("row0: %q", lines[0])
	}
	if !strings.Contains(lines[1], "5: E") {
		t.Fatalf("row1: %q", lines[1])
	}
}

func TestFormatCLIEntry_marker(t *testing.T) {
	got := formatCLIEntry(1, "Home", " *")
	if got != "1: Home *" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteCLIScreen_rootHostsHint(t *testing.T) {
	var buf bytes.Buffer
	st := cliScreenState{
		Entries: []cliGroupEntry{
			{Group: &access.AccessGroup{ID: access.GroupID("g1"), Name: "Home"}, Targets: nil},
		},
		CurrentGroupIndex: 0,
		Cols:              120,
	}
	if err := writeCLIScreen(&buf, st, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "PWD: /\r\n\r\nGroups") {
		t.Fatalf("PWD/Groups spacing missing: %q", out)
	}
	if !strings.Contains(out, "\r\nHosts\r\n") {
		t.Fatalf("Groups/Hosts spacing missing: %q", out)
	}
	sep := cliSeparatorLine(120)
	if !strings.Contains(out, sep) {
		t.Fatalf("separator missing (want %d dashes): %q", len(sep), out)
	}
	if strings.Index(out, sep) < strings.Index(out, "Hosts") {
		t.Fatalf("separator should follow Hosts section: %q", out)
	}
	if !strings.Contains(out, "1: Home") {
		t.Fatalf("group missing: %q", out)
	}
	if !strings.Contains(out, "cd <group#> to list servers") {
		t.Fatalf("hosts hint missing: %q", out)
	}
	if strings.Contains(out, "[ssh]") {
		t.Fatalf("should not list hosts at root: %q", out)
	}
}

func TestWriteCLIScreen_inGroupShowsHosts(t *testing.T) {
	var buf bytes.Buffer
	st := cliScreenState{
		Entries: []cliGroupEntry{
			{
				Group: &access.AccessGroup{ID: access.GroupID("g1"), Name: "Home"},
				Targets: []*access.Target{
					{Name: "sw1", Protocol: access.ProtocolSSH, Host: "10.0.0.1", Port: 22},
				},
			},
		},
		CurrentGroupIndex: 1,
		Cols:              120,
	}
	if err := writeCLIScreen(&buf, st, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "PWD: /Home") {
		t.Fatalf("PWD: %q", out)
	}
	if !strings.Contains(out, "1: Home *") {
		t.Fatalf("marker: %q", out)
	}
	if !strings.Contains(out, "1: sw1 [ssh]") {
		t.Fatalf("host: %q", out)
	}
}

func TestWriteCLIScreen_emptyGroupStillListed(t *testing.T) {
	var buf bytes.Buffer
	st := cliScreenState{
		Entries: []cliGroupEntry{
			{Group: &access.AccessGroup{ID: access.GroupID("g1"), Name: "Lab"}, Targets: nil},
		},
		CurrentGroupIndex: 0,
		Cols:              120,
	}
	if err := writeCLIScreen(&buf, st, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "1: Lab") {
		t.Fatalf("empty group should appear in Groups: %q", buf.String())
	}
}

func TestCliNumColumns(t *testing.T) {
	if cliNumColumns(120) != 4 {
		t.Fatal("expected 4 cols at 120")
	}
	if cliNumColumns(60) != 2 {
		t.Fatal("expected 2 cols at 60")
	}
}
