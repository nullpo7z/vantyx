package sshd

import (
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
	srv, err := NewServer(Config{
		UserStore:      nil,
		TargetStore:    nil,
		GroupStore:     nil,
		SessionManager: mgr,
		HostKey:        nil,
	})
	if err == nil {
		t.Fatal("expected error when UserStore is nil")
	}
	srv, err = NewServer(Config{
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
