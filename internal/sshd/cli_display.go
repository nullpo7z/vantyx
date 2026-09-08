package sshd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

const cliClearScreen = "\033[2J\033[H"

const cliSessionDetachHint = "Press Ctrl+\\ or Ctrl+] to return to the menu (session continues)."

const cliSessionEndHint = "Press Ctrl+D to end the session."

// cliSessionBarState is the compact status bar shown during a target session.
type cliSessionBarState struct {
	TargetName         string
	Protocol           access.Protocol
	Cols               int
	ShowEndSessionHint bool // false on resume (Ctrl+D is sent to the remote)
}

func cliSeparatorLine(cols int) string {
	if cols < 1 {
		cols = 80
	}
	return strings.Repeat("-", cols)
}

// cliScreenState drives the IM7200-style header (PWD / Groups / Hosts).
type cliScreenState struct {
	AllGroups []cliGroupEntry
	Location  cliNavLocation
	Cols      int // terminal width in columns
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

func cliPWDPath(all []cliGroupEntry, loc cliNavLocation) string {
	_ = all
	return loc.pwd()
}

type cliScreenLayout struct {
	lines []string
}

func buildCLISessionBarLayout(bar cliSessionBarState) cliScreenLayout {
	lines := []string{fmt.Sprintf("Connected to: %s [%s]", bar.TargetName, bar.Protocol)}
	lines = append(lines, cliSessionDetachHint)
	if bar.ShowEndSessionHint {
		lines = append(lines, cliSessionEndHint)
	}
	lines = append(lines, cliSeparatorLine(bar.Cols))
	return cliScreenLayout{lines: lines}
}

func countCLISessionBarLines(bar cliSessionBarState) int {
	return buildCLISessionBarLayout(bar).lineCount()
}

func writeCLISessionBar(w io.Writer, bar cliSessionBarState) error {
	return buildCLISessionBarLayout(bar).writeTo(w)
}

func buildCLIScreenLayout(st cliScreenState, sessionLines []string, extraLines []string) cliScreenLayout {
	termCols := cliTermWidth(st.Cols)
	numCols := cliNumColumns(termCols)
	view := buildCLINavView(st.AllGroups, st.Location)
	var lines []string

	lines = append(lines, "PWD: "+cliPWDPath(st.AllGroups, st.Location), "")
	lines = append(lines, "Groups")
	if len(st.AllGroups) == 0 {
		lines = append(lines, "  (no groups assigned)")
	} else if len(view.Items) == 0 {
		if st.Location.atRoot() {
			lines = append(lines, "  (cd <group#> to browse — e.g. cd 1)")
		} else {
			lines = append(lines, "  (no subgroups here — cd .. to go up)")
		}
	} else {
		groupEntries := make([]string, len(view.Items))
		for i, item := range view.Items {
			groupEntries[i] = formatCLIEntry(i+1, item.Label, "")
		}
		lines = append(lines, formatCLIColumns(groupEntries, numCols)...)
	}

	lines = append(lines, "", "Hosts")
	if st.Location.atRoot() {
		lines = append(lines, "  (cd <group#> to list servers — e.g. cd 1)")
	} else if len(view.Hosts) == 0 {
		lines = append(lines, "  (no SSH/Telnet servers at this path)")
	} else {
		hostEntries := make([]string, len(view.Hosts))
		for i, t := range view.Hosts {
			hostEntries[i] = formatCLIEntry(i+1, formatCLITargetLabel(t), "")
		}
		lines = append(lines, formatCLIColumns(hostEntries, numCols)...)
	}

	if len(sessionLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, sessionLines...)
	}
	lines = append(lines, "", cliSeparatorLine(st.Cols))
	lines = append(lines, extraLines...)
	return cliScreenLayout{lines: lines}
}

func (l cliScreenLayout) lineCount() int {
	return len(l.lines)
}

func (l cliScreenLayout) writeTo(w io.Writer) error {
	for _, line := range l.lines {
		if _, err := fmt.Fprintf(w, "%s\r\n", line); err != nil {
			return err
		}
	}
	return nil
}

// writeCLIScreen renders PWD, Groups, Hosts, active sessions, separator, then optional extra lines (menu mode / status).
func writeCLIScreen(w io.Writer, st cliScreenState, sessionLines []string, extraLines []string) error {
	return buildCLIScreenLayout(st, sessionLines, extraLines).writeTo(w)
}

// formatCLIActiveSessionLines formats active session rows below the header.
func formatCLIActiveSessionLines(sessions []*session.Session, mgr *session.Manager) []string {
	if len(sessions) == 0 {
		return []string{"Active sessions", "  (none)"}
	}
	lines := []string{"Active sessions"}
	for i, sess := range sessions {
		name := sess.Name
		if name == "" {
			name = "(no name)"
		}
		idleNote := cliSessionIdleSuffix(sess, mgr)
		if sess.Description != "" {
			lines = append(lines, fmt.Sprintf("  %d: %s — %s (%s)%s", i+1, name, sess.Description, sess.TargetName, idleNote))
		} else {
			lines = append(lines, fmt.Sprintf("  %d: %s (%s)%s", i+1, name, sess.TargetName, idleNote))
		}
	}
	if w := formatCLIIdleSessionsWarning(sessions, mgr); w != "" {
		lines = append(lines, w)
	}
	return lines
}

func cliSessionIdleSuffix(sess *session.Session, mgr *session.Manager) string {
	if mgr == nil || !mgr.IsIdle(sess) {
		return ""
	}
	d := mgr.IdleDuration(sess).Round(time.Minute)
	if d < time.Minute {
		d = time.Minute
	}
	return fmt.Sprintf(" [idle %s]", d)
}

// formatCLIIdleSessionsWarning returns a summary line when any session exceeds the idle threshold.
func formatCLIIdleSessionsWarning(sessions []*session.Session, mgr *session.Manager) string {
	if mgr == nil || mgr.IdleWarnAfter() == 0 {
		return ""
	}
	n := 0
	for _, sess := range sessions {
		if mgr.IsIdle(sess) {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	if n == 1 {
		return "  Warning: 1 session has been idle for a long time (sessions are not auto-stopped)."
	}
	return fmt.Sprintf("  Warning: %d sessions have been idle for a long time (sessions are not auto-stopped).", n)
}
