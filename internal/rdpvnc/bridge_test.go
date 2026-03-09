package rdpvnc

import (
	"testing"
)

func TestNextDisplay(t *testing.T) {
	d1 := nextDisplay()
	d2 := nextDisplay()
	if d2 <= d1 {
		t.Fatalf("expected d2 > d1, got d1=%d d2=%d", d1, d2)
	}
}

func TestFreePort(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if port <= 0 {
		t.Fatalf("expected positive port, got %d", port)
	}
}

func TestManagerRegisterRemove(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	m.Remove("nonexistent")
	m.StopAll()
}

func TestKillProcNil(t *testing.T) {
	killProc(nil)
}
