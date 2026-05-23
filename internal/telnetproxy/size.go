package telnetproxy

import "sync"

type terminalSize struct {
	mu   sync.Mutex
	cols uint16
	rows uint16
}

func newTerminalSize(cols, rows int) *terminalSize {
	c, r := clampTerminalSize(cols, rows)
	return &terminalSize{cols: c, rows: r}
}

func clampTerminalSize(cols, rows int) (uint16, uint16) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	if cols > 65535 {
		cols = 65535
	}
	if rows > 65535 {
		rows = 65535
	}
	return uint16(cols), uint16(rows)
}

func (t *terminalSize) get() (uint16, uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}

func (t *terminalSize) set(cols, rows int) bool {
	c, r := clampTerminalSize(cols, rows)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cols == c && t.rows == r {
		return false
	}
	t.cols, t.rows = c, r
	return true
}
