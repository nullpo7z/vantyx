package telnetproxy

import (
	"os"
	"testing"
)

func TestTelnetWakeOnConnect_Default(t *testing.T) {
	_ = os.Unsetenv("VANTYX_TELNET_WAKE_ON_CONNECT")
	if !telnetWakeOnConnect() {
		t.Fatal("expected wake enabled by default")
	}
}

func TestTelnetWakeOnConnect_Disabled(t *testing.T) {
	t.Setenv("VANTYX_TELNET_WAKE_ON_CONNECT", "0")
	if telnetWakeOnConnect() {
		t.Fatal("expected wake disabled")
	}
}
