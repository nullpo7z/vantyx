package sshd

import (
	"fmt"
	"io"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

const cliClearScreen = "\033[2J\033[H"

func cliSeparatorLine(cols int) string {
	if cols < 1 {
		cols = 80
	}
	return strings.Repeat("-", cols)
}

// cliScreenState drives the IM7200-style header (PWD / Groups / Hosts).
type cliScreenState struct {
	Entries           []cliGroupEntry
	CurrentGroupIndex int // 0 = root; 1-based group index otherwise
	Cols              int // terminal width in columns
}

func cliTermWidth(cols int) int {
	if cols < 40 {
		return 80
	}
	return cols
}

func cliNumColumns(termCols int) int {
	switch {
	case termCols >= 120:
		return 4
	case termCols >= 80:
		return 3
	case termCols >= 50:
		return 2
	default:
		return 1
	}
}

// formatCLIEntry formats one indexed label (IM7200 style: "1: Name *").
func formatCLIEntry(index int, label string, marker string) string {
	if marker != "" {
		return fmt.Sprintf("%d: %s%s", index, label, marker)
	}
	return fmt.Sprintf("%d: %s", index, label)
}

// formatCLIColumns lays out preformatted entries into rows with up to numCols columns.
func formatCLIColumns(entries []string, numCols int) []string {
	if numCols < 1 {
		numCols = 1
	}
	if len(entries) == 0 {
		return nil
	}
	rows := (len(entries) + numCols - 1) / numCols
	lines := make([]string, 0, rows)
	for r := 0; r < rows; r++ {
		var parts []string
		for c := 0; c < numCols; c++ {
			i := r*numCols + c
			if i < len(entries) {
				parts = append(parts, entries[i])
			}
		}
		lines = append(lines, strings.Join(parts, "    "))
	}
	return lines
}

func formatCLITargetLabel(t *access.Target) string {
	return fmt.Sprintf("%s [%s]", t.Name, t.Protocol)
}

func cliPWDPath(entries []cliGroupEntry, currentGroupIndex int) string {
	if currentGroupIndex >= 1 && currentGroupIndex <= len(entries) {
		return "/" + entries[currentGroupIndex-1].Group.Name
	}
	return "/"
}

// writeCLIScreen renders PWD, Groups, and Hosts sections. extraLines are printed after Hosts.
func writeCLIScreen(w io.Writer, st cliScreenState, extraLines []string) error {
	termCols := cliTermWidth(st.Cols)
	numCols := cliNumColumns(termCols)

	write := func(format string, args ...interface{}) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}

	if err := write("PWD: %s\r\n\r\n", cliPWDPath(st.Entries, st.CurrentGroupIndex)); err != nil {
		return err
	}
	if err := write("Groups\r\n"); err != nil {
		return err
	}
	if len(st.Entries) == 0 {
		if err := write("  (no groups assigned)\r\n"); err != nil {
			return err
		}
	} else {
		groupEntries := make([]string, len(st.Entries))
		for i, e := range st.Entries {
			marker := ""
			if i+1 == st.CurrentGroupIndex {
				marker = " *"
			}
			groupEntries[i] = formatCLIEntry(i+1, e.Group.Name, marker)
		}
		for _, line := range formatCLIColumns(groupEntries, numCols) {
			if err := write("%s\r\n", line); err != nil {
				return err
			}
		}
	}

	if err := write("\r\nHosts\r\n"); err != nil {
		return err
	}
	if st.CurrentGroupIndex < 1 || st.CurrentGroupIndex > len(st.Entries) {
		if err := write("  (cd <group#> to list servers — e.g. cd 1)\r\n"); err != nil {
			return err
		}
	} else {
		targets := st.Entries[st.CurrentGroupIndex-1].Targets
		if len(targets) == 0 {
			if err := write("  (no SSH/Telnet servers in this group)\r\n"); err != nil {
				return err
			}
		} else {
			hostEntries := make([]string, len(targets))
			for i, t := range targets {
				hostEntries[i] = formatCLIEntry(i+1, formatCLITargetLabel(t), "")
			}
			for _, line := range formatCLIColumns(hostEntries, numCols) {
				if err := write("%s\r\n", line); err != nil {
					return err
				}
			}
		}
	}

	if err := write("\r\n%s\r\n", cliSeparatorLine(st.Cols)); err != nil {
		return err
	}
	for _, line := range extraLines {
		if err := write("%s\r\n", line); err != nil {
			return err
		}
	}
	return nil
}

// formatCLIActiveSessionLines formats active session rows below the header.
func formatCLIActiveSessionLines(sessions []*session.Session) []string {
	if len(sessions) == 0 {
		return []string{"Active sessions", "  (none)"}
	}
	lines := []string{"Active sessions"}
	for i, sess := range sessions {
		name := sess.Name
		if name == "" {
			name = "(no name)"
		}
		if sess.Description != "" {
			lines = append(lines, fmt.Sprintf("  %d: %s — %s (%s)", i+1, name, sess.Description, sess.TargetName))
		} else {
			lines = append(lines, fmt.Sprintf("  %d: %s (%s)", i+1, name, sess.TargetName))
		}
	}
	return lines
}
