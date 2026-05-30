package httpapi

import (
	"regexp"
	"strings"
)

// promptNoisePattern matches shell/window-title redraws that are not commands.
var promptNoisePattern = regexp.MustCompile(`^(?:\d+;)?[^\s]*@[^\s:]*:[^\s$#%]*[$#%>]\s*$`)

// commandLineTracker approximates the current input line on a PTY from remote stdout,
// so tab completion and line redraws are reflected when logging commands.
type commandLineTracker struct {
	line   []byte
	col    int
	escBuf []byte // incomplete escape sequence (starts with ESC)
}

func (t *commandLineTracker) reset() {
	t.line = t.line[:0]
	t.col = 0
	t.escBuf = t.escBuf[:0]
}

func (t *commandLineTracker) currentLine() string {
	return strings.TrimSpace(string(t.line))
}

func (t *commandLineTracker) feed(p []byte) {
	data := p
	if len(t.escBuf) > 0 {
		combined := append(t.escBuf, p...)
		t.escBuf = t.escBuf[:0]
		data = combined
	}

	for i := 0; i < len(data); {
		if data[i] == 0x1b {
			n, ok := t.consumeEscape(data[i:])
			if !ok {
				t.escBuf = append(t.escBuf[:0], data[i:]...)
				return
			}
			i += n
			continue
		}
		t.feedByte(data[i])
		i++
	}
}

// consumeEscape parses one escape sequence at the start of seq.
// Returns bytes consumed and whether the sequence is complete.
func (t *commandLineTracker) consumeEscape(seq []byte) (int, bool) {
	if len(seq) == 0 || seq[0] != 0x1b {
		return 0, true
	}
	if len(seq) == 1 {
		return 0, false
	}
	switch seq[1] {
	case ']': // OSC — window title etc.; never part of the input line
		for j := 2; j < len(seq); j++ {
			if seq[j] == 0x07 {
				return j + 1, true
			}
			if seq[j] == 0x1b && j+1 < len(seq) && seq[j+1] == '\\' {
				return j + 2, true
			}
		}
		return 0, false
	case '[':
		for j := 2; j < len(seq); j++ {
			if isCSIFinal(seq[j]) {
				t.applyCSI(seq[j], string(seq[2:j]))
				return j + 1, true
			}
		}
		return 0, false
	default:
		// ESC + single final byte (e.g. ESC M)
		if isCSIFinal(seq[1]) {
			return 2, true
		}
		return 0, false
	}
}

func isCSIFinal(b byte) bool {
	return b >= 0x40 && b <= 0x7e
}

func (t *commandLineTracker) feedByte(b byte) {
	switch b {
	case '\r':
		t.line = t.line[:0]
		t.col = 0
	case '\n', '\a':
	case '\b', 0x7f:
		if t.col > 0 {
			t.line = append(t.line[:t.col-1], t.line[t.col:]...)
			t.col--
		}
	case '\t':
	default:
		if b < 32 {
			return
		}
		t.insertByte(b)
	}
}

func (t *commandLineTracker) applyCSI(cmd byte, params string) {
	switch cmd {
	case 'K': // EL — erase in line
		mode := csiParam(params, 0)
		switch mode {
		case 1:
			if t.col > 0 {
				copy(t.line, t.line[t.col:])
				t.line = t.line[:len(t.line)-t.col]
				t.col = 0
			}
		case 2:
			t.reset()
		default:
			if t.col < len(t.line) {
				t.line = t.line[:t.col]
			}
		}
	case 'G': // CHA — cursor horizontal absolute (1-based column)
		col := csiParam(params, 1) - 1
		if col < 0 {
			col = 0
		}
		if col > len(t.line) {
			t.col = len(t.line)
		} else {
			t.col = col
		}
	case 'C': // CUF — cursor forward
		n := csiParam(params, 1)
		t.col += n
		if t.col > len(t.line) {
			t.col = len(t.line)
		}
	case 'D': // CUB — cursor back
		n := csiParam(params, 1)
		t.col -= n
		if t.col < 0 {
			t.col = 0
		}
	case 'P': // DCH — delete characters
		n := csiParam(params, 1)
		if n > 0 && t.col < len(t.line) {
			end := t.col + n
			if end > len(t.line) {
				end = len(t.line)
			}
			t.line = append(t.line[:t.col], t.line[end:]...)
		}
	case '@': // ICH — insert blank characters
		n := csiParam(params, 1)
		if n > 0 {
			padding := make([]byte, n)
			t.line = append(t.line[:t.col], append(padding, t.line[t.col:]...)...)
		}
	}
}

func csiParam(params string, defaultVal int) int {
	if params == "" {
		return defaultVal
	}
	if idx := strings.IndexByte(params, ';'); idx >= 0 {
		params = params[:idx]
	}
	n := 0
	for _, c := range params {
		if c < '0' || c > '9' {
			return defaultVal
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return defaultVal
	}
	return n
}

func (t *commandLineTracker) insertByte(b byte) {
	if t.col >= len(t.line) {
		t.line = append(t.line, b)
		t.col = len(t.line)
		return
	}
	t.line[t.col] = b
	t.col++
}

// mergeCommandLine prefers the PTY echo line when it extends stdin (e.g. tab completion).
// When stdin has bytes but the PTY did not echo them (password prompts), the line is
// dropped so secrets are not persisted (CWE-532).
func mergeCommandLine(stdinLine, echoLine string) string {
	stdinLine = strings.TrimSpace(stdinLine)
	echoLine = strings.TrimSpace(echoLine)
	if stdinLine != "" && echoLine == "" {
		return ""
	}
	if echoLine == "" {
		return stdinLine
	}
	if stdinLine == "" {
		candidate := stripShellPrompt(echoLine)
		if isNoiseCommandLogLine(candidate) {
			return ""
		}
		return candidate
	}
	if idx := strings.LastIndex(echoLine, stdinLine); idx >= 0 {
		candidate := strings.TrimSpace(echoLine[idx:])
		if isNoiseCommandLogLine(candidate) {
			return ""
		}
		return candidate
	}
	if len(echoLine) >= len(stdinLine) {
		if stripped := stripShellPrompt(echoLine); stripped != "" && !isNoiseCommandLogLine(stripped) {
			return stripped
		}
		if !isNoiseCommandLogLine(echoLine) {
			return echoLine
		}
		return ""
	}
	if isNoiseCommandLogLine(stdinLine) {
		return ""
	}
	return stdinLine
}

func isNoiseCommandLogLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	if strings.Contains(line, "\x1b") {
		return true
	}
	if promptNoisePattern.MatchString(line) {
		return true
	}
	// Device-style bare prompts (e.g. Z9100-ON#) with no command token.
	if !strings.Contains(line, " ") && strings.HasSuffix(line, "#") {
		return true
	}
	stripped := stripShellPrompt(line)
	if stripped == "" || stripped == line {
		if strings.Contains(line, "@") && strings.ContainsAny(line, "$#%>") {
			return true
		}
		if strings.HasPrefix(line, "0;") {
			return true
		}
	}
	return false
}

func stripShellPrompt(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"$ ", "# ", "> ", "% "} {
		if idx := strings.LastIndex(s, sep); idx >= 0 {
			return strings.TrimSpace(s[idx+len(sep):])
		}
	}
	return s
}
