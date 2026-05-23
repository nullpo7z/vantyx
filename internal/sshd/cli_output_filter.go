package sshd

import (
	"bytes"
	"io"
)

// ansiRedrawHeader is a sentinel returned by rewriteCSI to trigger header redraw.
var ansiRedrawHeader = []byte("\x00vantyx-redraw-header")

// ansiSuppress drops a remote sequence without writing it.
var ansiSuppress = []byte("\x00vantyx-suppress")

// cliOutputFilter protects the fixed header region from remote cursor/clear sequences.
type cliOutputFilter struct {
	w           io.Writer
	headerLines int
	buf         []byte
	onFullClear func() error
}

func newCLIOutputFilter(w io.Writer, headerLines int) *cliOutputFilter {
	return &cliOutputFilter{w: w, headerLines: headerLines}
}

func (f *cliOutputFilter) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	for {
		idx := bytes.IndexByte(f.buf, 0x1b)
		if idx < 0 {
			if len(f.buf) > 0 {
				if _, err := f.w.Write(f.buf); err != nil {
					return len(p), err
				}
				f.buf = nil
			}
			break
		}
		if idx > 0 {
			if _, err := f.w.Write(f.buf[:idx]); err != nil {
				return len(p), err
			}
			f.buf = f.buf[idx:]
		}
		rem, esc := consumeANSI(f.buf, f.headerLines)
		if esc == nil {
			break
		}
		if bytes.Equal(esc, ansiRedrawHeader) {
			if f.onFullClear != nil {
				if err := f.onFullClear(); err != nil {
					return len(p), err
				}
			}
		} else if !bytes.Equal(esc, ansiSuppress) {
			if _, err := f.w.Write(esc); err != nil {
				return len(p), err
			}
		}
		f.buf = rem
	}
	return len(p), nil
}

func consumeANSI(b []byte, headerLines int) (rest []byte, out []byte) {
	if len(b) < 2 || b[0] != 0x1b {
		return b, nil
	}
	// CSI sequences: ESC [
	if b[1] == '[' {
		return consumeCSI(b, headerLines)
	}
	// Single-char ESC (e.g. ESC M): pass through
	if len(b) >= 2 {
		return b[2:], b[:2]
	}
	return b, nil
}

func consumeCSI(b []byte, headerLines int) (rest []byte, out []byte) {
	end := 2
	for end < len(b) {
		c := b[end]
		if c >= 0x40 && c <= 0x7e {
			end++
			seq := b[:end]
			return b[end:], rewriteCSI(seq, headerLines)
		}
		end++
	}
	return b, nil
}

func rewriteCSI(seq []byte, headerLines int) []byte {
	if len(seq) < 3 {
		return seq
	}
	body := string(seq[2 : len(seq)-1])
	final := seq[len(seq)-1]
	first := headerLines + 1

	switch final {
	case 'H', 'f': // CUP
		row, col, ok := parseCSIRowCol(body)
		if !ok {
			return seq
		}
		if row > 0 && row < first {
			row = first
		}
		return []byte("\033[" + formatCSIRowCol(row, col) + string(final))
	case 'r': // DECSTBM — ignore remote reset of scroll region
		return ansiSuppress
	case 'J': // ED — full screen clear from remote: redraw header via callback
		if body == "2" || body == "3" {
			return ansiRedrawHeader
		}
		return seq
	case 'K':
		return seq
	default:
		return seq
	}
}

func parseCSIRowCol(body string) (row, col int, ok bool) {
	if body == "" {
		return 0, 0, false
	}
	parts := bytes.Split([]byte(body), []byte{';'})
	row = 1
	col = 1
	if len(parts) >= 1 && len(parts[0]) > 0 {
		row = atoiBytes(parts[0])
	}
	if len(parts) >= 2 && len(parts[1]) > 0 {
		col = atoiBytes(parts[1])
	}
	return row, col, true
}

func formatCSIRowCol(row, col int) string {
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	if row == 1 && col == 1 {
		return ""
	}
	return itoa(row) + ";" + itoa(col)
}

func atoiBytes(b []byte) int {
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
