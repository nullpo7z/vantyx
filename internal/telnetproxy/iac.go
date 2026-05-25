package telnetproxy

import "io"

// Telnet IAC and commands (RFC 854).
const (
	iac  = 255
	will = 251
	wont = 252
	do   = 253
	dont = 254
	sb   = 250
	se   = 240
)

// Telnet options (common).
const (
	optEcho            = 1
	optSuppressGoAhead = 3
	optNAWS            = 31
)

// sendClientNegotiation sends a minimal client option set after TCP connect.
func sendClientNegotiation(w io.Writer) error {
	_, err := w.Write([]byte{
		iac, will, optSuppressGoAhead,
		iac, wont, optEcho,
	})
	return err
}

// negotiateReply returns the IAC response to a server DO/WILL (RFC 854).
func negotiateReply(cmd, opt byte) []byte {
	switch cmd {
	case do:
		switch opt {
		case optEcho:
			// Decline local echo so the client only sees what the
			// remote server explicitly transmits.
			return []byte{iac, wont, optEcho}
		case optSuppressGoAhead:
			return []byte{iac, will, opt}
		case optNAWS:
			return []byte{iac, will, opt}
		default:
			return []byte{iac, wont, opt}
		}
	case dont:
		return []byte{iac, wont, opt}
	case will:
		switch opt {
		case optEcho, optSuppressGoAhead:
			return []byte{iac, do, opt}
		default:
			return []byte{iac, dont, opt}
		}
	case wont:
		return []byte{iac, dont, opt}
	default:
		return nil
	}
}

// iacStream buffers incomplete IAC sequences across TCP reads.
type iacStream struct {
	pending []byte
}

// Filter strips Telnet IAC from b, sends negotiation replies, and retains a trailing partial sequence.
func (s *iacStream) Filter(b []byte, replyTo func([]byte) error) []byte {
	if len(s.pending) > 0 {
		b = append(append([]byte(nil), s.pending...), b...)
		s.pending = s.pending[:0]
	}
	out, rest := filterIACBytes(b, replyTo)
	if len(rest) > 0 {
		s.pending = append(s.pending[:0], rest...)
	}
	return out
}

// filterIAC strips IAC sequences from a single buffer (tests). Use iacStream for live TCP.
func filterIAC(b []byte, replyTo func([]byte) error) []byte {
	out, _ := filterIACBytes(b, replyTo)
	return out
}

func filterIACBytes(b []byte, replyTo func([]byte) error) (out, rest []byte) {
	i := 0
	for i < len(b) {
		if b[i] != iac {
			out = append(out, b[i])
			i++
			continue
		}
		i++
		if i >= len(b) {
			rest = b[i-1:]
			return out, rest
		}
		// IAC IAC => literal 0xFF
		if b[i] == iac {
			out = append(out, iac)
			i++
			continue
		}
		cmd := b[i]
		i++
		switch cmd {
		case do, dont, will, wont:
			if i >= len(b) {
				rest = b[i-2:]
				return out, rest
			}
			opt := b[i]
			i++
			if replyTo != nil {
				if reply := negotiateReply(cmd, opt); len(reply) > 0 {
					_ = replyTo(reply)
				}
			}
		case sb:
			start := i - 2
			for i < len(b) {
				if b[i] == iac && i+1 < len(b) && b[i+1] == se {
					i += 2
					break
				}
				i++
			}
			if i >= len(b) {
				rest = b[start:]
				return out, rest
			}
		default:
		}
	}
	return out, nil
}

// encodeNAWS builds IAC SB NAWS width height IAC SE (RFC 1073, 16-bit big-endian each).
func encodeNAWS(cols, rows uint16) []byte {
	c, r := clampTerminalSize(int(cols), int(rows))
	// #nosec G115 -- NAWS wire format: high/low bytes of clamped uint16 dimensions
	return []byte{
		iac, sb, optNAWS,
		byte(c >> 8), byte(c),
		byte(r >> 8), byte(r),
		iac, se,
	}
}
