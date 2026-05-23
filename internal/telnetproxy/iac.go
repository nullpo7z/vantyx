package telnetproxy

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

// filterIAC strips IAC sequences from server data and writes required replies to replyTo.
func filterIAC(b []byte, replyTo func([]byte) error) []byte {
	var out []byte
	i := 0
	for i < len(b) {
		if b[i] != iac {
			out = append(out, b[i])
			i++
			continue
		}
		i++
		if i >= len(b) {
			break
		}
		cmd := b[i]
		i++
		switch cmd {
		case do, dont:
			if i < len(b) {
				opt := b[i]
				i++
				if cmd == do && replyTo != nil {
					_ = replyTo([]byte{iac, wont, opt})
				}
			}
		case will, wont:
			if i < len(b) {
				i++
			}
		case sb:
			for i < len(b) && b[i] != iac {
				i++
			}
			if i+1 < len(b) && b[i] == iac && b[i+1] == se {
				i += 2
			}
		default:
		}
	}
	return out
}
