package telnetproxy

import (
	"os"
	"strings"
)

// telnetWakeOnConnect reports whether to send CR after connect to wake silent devices.
// Set VANTYX_TELNET_WAKE_ON_CONNECT=0 or false to disable.
func telnetWakeOnConnect() bool {
	v := strings.TrimSpace(os.Getenv("VANTYX_TELNET_WAKE_ON_CONNECT"))
	if v == "0" || strings.EqualFold(v, "false") {
		return false
	}
	return true
}
