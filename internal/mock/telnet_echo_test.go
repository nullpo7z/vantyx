package mock

import (
	"net"
	"testing"
	"time"
)

func TestTelnetEchoServer_Echo(t *testing.T) {
	srv := NewTelnetEchoServer()
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	port := srv.Port()
	if port == 0 {
		t.Fatal("port is 0")
	}

	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Server may send IAC options first; read and discard until we send our data
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

	_, err = conn.Write([]byte("hi"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := TelnetReadEcho(conn, 2, nil)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(got) != "hi" {
		t.Fatalf("expected hi, got %q", string(got))
	}
}

func TestTelnetEchoServer_MultipleEchoes(t *testing.T) {
	srv := NewTelnetEchoServer()
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))

	for _, input := range []string{"a", "ab", "hello"} {
		_, err = conn.Write([]byte(input))
		if err != nil {
			t.Fatalf("write %q: %v", input, err)
		}
		got, err := TelnetReadEcho(conn, len(input), nil)
		if err != nil {
			t.Fatalf("read %q: %v", input, err)
		}
		if string(got) != input {
			t.Fatalf("expected %q, got %q", input, string(got))
		}
	}
}

func TestTelnetEchoServer_AddrAndPortBeforeStart(t *testing.T) {
	srv := NewTelnetEchoServer()
	if a := srv.Addr(); a != "" {
		t.Fatalf("Addr before Start expected empty, got %q", a)
	}
	if p := srv.Port(); p != 0 {
		t.Fatalf("Port before Start expected 0, got %d", p)
	}
}

func TestTelnetEchoServer_CloseIdempotent(t *testing.T) {
	srv := NewTelnetEchoServer()
	if err := srv.Close(); err != nil {
		t.Fatalf("Close on unstarted: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestProcessTelnet_IACSequences(t *testing.T) {
	srv := &TelnetEchoServer{}
	// IAC DO opt -> reply IAC WONT opt
	got := srv.processTelnet([]byte{IAC, DO, 1})
	if len(got) != 3 || got[0] != IAC || got[1] != WONT || got[2] != 1 {
		t.Fatalf("IAC DO 1: got %v", got)
	}
	// IAC DONT opt -> no reply (just consume)
	got = srv.processTelnet([]byte{IAC, DONT, 2})
	if len(got) != 0 {
		t.Fatalf("IAC DONT: expected empty, got %v", got)
	}
	// IAC WILL opt -> skip option
	got = srv.processTelnet([]byte{IAC, WILL, 3})
	if len(got) != 0 {
		t.Fatalf("IAC WILL: expected empty, got %v", got)
	}
	// IAC SB ... IAC SE
	got = srv.processTelnet([]byte{IAC, SB, 1, 2, IAC, SE})
	if len(got) != 0 {
		t.Fatalf("IAC SB SE: expected empty, got %v", got)
	}
	// IAC single-byte (default)
	got = srv.processTelnet([]byte{IAC, 241})
	if len(got) != 0 {
		t.Fatalf("IAC default: expected empty, got %v", got)
	}
	// mixed: data + IAC
	got = srv.processTelnet([]byte{'a', IAC, WONT, 0, 'b'})
	if string(got) != "ab" {
		t.Fatalf("mixed: got %q", got)
	}
}

func TestSkipIAC(t *testing.T) {
	// DO/DONT/WILL/WONT with option
	got := skipIAC([]byte{IAC, DO, 1})
	if len(got) != 0 {
		t.Fatalf("IAC DO: got %v", got)
	}
	got = skipIAC([]byte{IAC, SB, 1, IAC, SE})
	if len(got) != 0 {
		t.Fatalf("IAC SB SE: got %v", got)
	}
	got = skipIAC([]byte{'x', IAC, 240, 'y'})
	if string(got) != "xy" {
		t.Fatalf("default: got %q", got)
	}
}

func TestTelnetReadEcho_WithBuf(t *testing.T) {
	srv := NewTelnetEchoServer()
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()
	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _ = conn.Write([]byte("ab"))
	buf := make([]byte, 64)
	got, err := TelnetReadEcho(conn, 2, buf)
	if err != nil {
		t.Fatalf("TelnetReadEcho: %v", err)
	}
	if string(got) != "ab" {
		t.Fatalf("got %q", got)
	}
}
