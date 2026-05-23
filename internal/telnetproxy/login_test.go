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

func TestLoginPromptSeen(t *testing.T) {
	if !loginPromptSeen([]byte("username:")) {
		t.Fatal("expected username prompt")
	}
	if loginPromptSeen([]byte("password:")) {
		t.Fatal("password alone should not match login")
	}
}
