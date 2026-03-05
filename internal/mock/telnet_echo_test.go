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
