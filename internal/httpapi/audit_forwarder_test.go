package httpapi

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditForwarder_UnixgramSendsSyslogLine(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "syslog.sock")

	pc, err := net.ListenPacket("unixgram", sock)
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer pc.Close()

	f := newAuditForwarder(auditForwarderConfig{
		Enabled: true,
		Proto:   "unixgram",
		Addr:    sock,
		App:     "vantyx-test",
		Buffer:  10,
	})
	if f == nil {
		t.Fatal("expected forwarder")
	}
	defer f.close()

	f.sendJSONL([]byte(`{"event":"x"}`))

	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n == 0 {
		t.Fatal("expected non-empty syslog payload")
	}
}

func TestAuditForwarder_UnixgramDefaultDevLog(t *testing.T) {
	// Just ensure config normalization doesn't crash.
	_ = os.Setenv("VANTYX_AUDIT_FORWARD_PROTO", "unixgram")
	_ = os.Unsetenv("VANTYX_AUDIT_FORWARD_ADDR")
	f := newAuditForwarderFromEnv()
	// In unit tests /dev/log may not exist; forwarder may be nil. That's fine.
	if f != nil {
		f.close()
	}
}

func TestStrconvAtoiSafe(t *testing.T) {
	if n, err := strconvAtoiSafe("123"); err != nil || n != 123 {
		t.Fatalf("expected 123, got %d err=%v", n, err)
	}
	if _, err := strconvAtoiSafe("x"); err == nil {
		t.Fatal("expected error")
	}
}

