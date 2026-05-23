package telnetproxy

import (
	"bytes"
	"testing"
)

func TestLoginAutomater_SendsCredentials(t *testing.T) {
	var sent [][]byte
	write := func(b []byte) error {
		sent = append(sent, append([]byte(nil), b...))
		return nil
	}
	la := NewLoginAutomater("alice", "secret")
	la.OnOutput([]byte("Welcome\r\nLogin: "), write)
	if la.state != 1 {
		t.Fatalf("expected state 1 after login prompt, got %d", la.state)
	}
	if len(sent) != 1 || string(bytes.TrimSpace(sent[0])) != "alice" {
		t.Fatalf("username send: %v", sent)
	}
	la.OnOutput([]byte("Password: "), write)
	if la.state != 2 {
		t.Fatalf("expected state 2 after password prompt, got %d", la.state)
	}
	if len(sent) != 2 || string(bytes.TrimSpace(sent[1])) != "secret" {
		t.Fatalf("password send: %v", sent)
	}
}

func TestLoginAutomater_NoFalsePositiveOnBannerUser(t *testing.T) {
	var sent [][]byte
	write := func(b []byte) error {
		sent = append(sent, append([]byte(nil), b...))
		return nil
	}
	la := NewLoginAutomater("alice", "secret")
	la.OnOutput([]byte("This system is for authorized user: admins only.\r\nStill waiting...\r\n"), write)
	if la.state != 0 {
		t.Fatalf("expected no login on banner user:, state %d", la.state)
	}
	if len(sent) != 0 {
		t.Fatalf("unexpected send: %v", sent)
	}
	la.OnOutput([]byte("Username: "), write)
	if la.state != 1 {
		t.Fatalf("expected state 1 after Username:, got %d", la.state)
	}
}

func TestLoginAutomater_CiscoStyle(t *testing.T) {
	var sent [][]byte
	write := func(b []byte) error {
		sent = append(sent, append([]byte(nil), b...))
		return nil
	}
	la := NewLoginAutomater("cisco", "cisco123")
	la.OnOutput([]byte("\r\nUser Access Verification\r\n\r\nUsername: "), write)
	if la.state != 1 {
		t.Fatalf("expected state 1, got %d", la.state)
	}
	la.OnOutput([]byte("Password: "), write)
	if la.state != 2 {
		t.Fatalf("expected state 2, got %d", la.state)
	}
}

func TestLoginPromptSeen(t *testing.T) {
	if !loginPromptSeen([]byte("username:")) {
		t.Fatal("expected username prompt")
	}
	if loginPromptSeen([]byte("password:")) {
		t.Fatal("password alone should not match login")
	}
}

func TestPromptLineSuffix(t *testing.T) {
	s := promptLineSuffix([]byte("banner line\r\nlogin:"))
	if string(s) != "login:" {
		t.Fatalf("got %q", s)
	}
}
