package sshd

import (
	"encoding/binary"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/session"
)

func TestNewServer_RequiresDeps(t *testing.T) {
	_, err := NewServer(Config{})
	if err == nil {
		t.Fatal("expected error when config is empty")
	}
	_, err = NewServer(Config{
		UserStore:      &auth.SQLiteUserStore{},
		TargetStore:    &access.SQLiteTargetStore{},
		GroupStore:     &access.SQLiteAccessGroupStore{},
		SessionManager: session.NewManager(),
	})
	if err != nil {
		t.Fatalf("expected nil error with full config (no DB): %v", err)
	}
}

func TestNewServer_GeneratesHostKeyWhenNil(t *testing.T) {
	// SessionStarter can be satisfied by *session.Manager without a real DB for NewServer.
	mgr := session.NewManager()
	_, err := NewServer(Config{
		UserStore:      nil,
		TargetStore:    nil,
		GroupStore:     nil,
		SessionManager: mgr,
		HostKey:        nil,
	})
	if err == nil {
		t.Fatal("expected error when UserStore is nil")
	}
	srv, err := NewServer(Config{
		UserStore:      &auth.SQLiteUserStore{},
		TargetStore:    &access.SQLiteTargetStore{},
		GroupStore:     &access.SQLiteAccessGroupStore{},
		SessionManager: mgr,
		HostKey:        nil,
	})
	if err != nil {
		t.Fatalf("NewServer with nil HostKey: %v", err)
	}
	if srv.config == nil {
		t.Fatal("expected server config to be set")
	}
}

func TestParsePtyReqPayload(t *testing.T) {
	// Too short
	if c, r, ok := parsePtyReqPayload([]byte{0, 0, 0, 0}); ok {
		t.Fatalf("expected !ok for short payload, got cols=%d rows=%d", c, r)
	}
	// Valid: term length 4 ("xterm"), then 4 bytes width, 4 bytes height (80, 24)
	payload := make([]byte, 0, 20)
	payload = binary.BigEndian.AppendUint32(payload, 4)
	payload = append(payload, 'x', 't', 'e', 'r')
	payload = binary.BigEndian.AppendUint32(payload, 80)
	payload = binary.BigEndian.AppendUint32(payload, 24)
	cols, rows, ok := parsePtyReqPayload(payload)
	if !ok || cols != 80 || rows != 24 {
		t.Fatalf("expected 80,24,true got cols=%d rows=%d ok=%v", cols, rows, ok)
	}
}

func TestParseWindowChangePayload(t *testing.T) {
	if c, r, ok := parseWindowChangePayload([]byte{0, 0, 0}); ok {
		t.Fatalf("expected !ok for short payload, got %d %d", c, r)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], 132)
	binary.BigEndian.PutUint32(payload[4:8], 40)
	cols, rows, ok := parseWindowChangePayload(payload)
	if !ok || cols != 132 || rows != 40 {
		t.Fatalf("expected 132,40,true got cols=%d rows=%d ok=%v", cols, rows, ok)
	}
}
