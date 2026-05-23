package sshd

import (
	"fmt"
	"io"
	"sync"

	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// cliSessionFrame pins the navigation header during a target session (DECSTBM).
type cliSessionFrame struct {
	mu          sync.Mutex
	w           io.Writer
	cols        int
	rows        int
	headerLines int
	bar         cliSessionBarState
	filter      *cliOutputFilter
	onResize    func(sessionCols, sessionRows int)
}

func newCLISessionFrame(w io.Writer, cols, rows int) *cliSessionFrame {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return &cliSessionFrame{w: w, cols: cols, rows: rows}
}

func (f *cliSessionFrame) SetOnResize(fn func(sessionCols, sessionRows int)) {
	f.mu.Lock()
	f.onResize = fn
	f.mu.Unlock()
}

func (f *cliSessionFrame) Enter(bar cliSessionBarState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	bar.Cols = f.cols
	f.bar = bar
	return f.applyLocked()
}

func (f *cliSessionFrame) Resize(cols, rows int) error {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cols, f.rows = cols, rows
	f.bar.Cols = cols
	if err := f.applyLocked(); err != nil {
		return err
	}
	if f.onResize != nil {
		sc, sr := f.sessionSizeLocked()
		f.onResize(sc, sr)
	}
	return nil
}

func (f *cliSessionFrame) Leave() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.filter = nil
	_, err := fmt.Fprintf(f.w, "\033[r")
	return err
}

func (f *cliSessionFrame) SessionWriter() io.Writer {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.filter != nil {
		return f.filter
	}
	return f.w
}

func (f *cliSessionFrame) SessionRows() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, sr := f.sessionSizeLocked()
	return sr
}

func (f *cliSessionFrame) SessionCols() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols
}

func (f *cliSessionFrame) applyLocked() error {
	if _, err := fmt.Fprintf(f.w, "%s", cliClearScreen); err != nil {
		return err
	}
	if err := writeCLISessionBar(f.w, f.bar); err != nil {
		return err
	}
	f.headerLines = countCLISessionBarLines(f.bar)
	sessionTop := f.headerLines + 1
	if sessionTop > f.rows {
		sessionTop = f.rows
	}
	if _, err := fmt.Fprintf(f.w, "\033[%d;%dr", sessionTop, f.rows); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f.w, "\033[%d;1H", sessionTop); err != nil {
		return err
	}
	f.filter = newCLIOutputFilter(f.w, f.headerLines)
	f.filter.onFullClear = func() error {
		return f.redrawHeaderLocked()
	}
	return nil
}

func (f *cliSessionFrame) redrawHeaderLocked() error {
	layout := buildCLISessionBarLayout(f.bar)
	for i, line := range layout.lines {
		if _, err := fmt.Fprintf(f.w, "\033[%d;1H\033[K%s\r\n", i+1, line); err != nil {
			return err
		}
	}
	sessionTop := f.headerLines + 1
	if sessionTop > f.rows {
		sessionTop = f.rows
	}
	if _, err := fmt.Fprintf(f.w, "\033[%d;%dr", sessionTop, f.rows); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f.w, "\033[%d;1H", sessionTop); err != nil {
		return err
	}
	return nil
}

func (f *cliSessionFrame) sessionSizeLocked() (cols, rows int) {
	cols = f.cols
	rows = f.rows - f.headerLines
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

// watchCLIResize forwards PTY resize events to the session frame during a bridge.
func watchCLIResize(stop <-chan struct{}, resizeChan <-chan sshproxy.TerminalSize, frame *cliSessionFrame) {
	for {
		select {
		case <-stop:
			return
		case sz, ok := <-resizeChan:
			if !ok {
				return
			}
			if sz.Cols > 0 && sz.Rows > 0 {
				_ = frame.Resize(sz.Cols, sz.Rows)
			}
		}
	}
}
