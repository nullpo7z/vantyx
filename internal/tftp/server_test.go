package tftp

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestTargetStore(t *testing.T) access.TargetStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tftp.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatal(err)
	}
	groups := access.NewSQLiteAccessGroupStore(db, nil)
	store := access.NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()
	if _, err := groups.Create(ctx, "g1", "G1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWithPath(ctx, access.TargetID("t1"), "T1", "192.168.1.50", 69, access.ProtocolTFTP, "g1", "g1", "admin", "", "", "", true, false, false); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	return store
}

func TestOpenWriteWindow_HasOpenWindow(t *testing.T) {
	s := NewServer(newTestTargetStore(t), t.TempDir())
	target := access.TargetID("t1")
	client := net.ParseIP("192.168.1.99")

	if s.hasOpenWindow(target, client) {
		t.Fatal("expected no window before OpenWriteWindow")
	}
	s.OpenWriteWindow(target, client, time.Minute)
	if !s.hasOpenWindow(target, client) {
		t.Fatal("expected window open for the authorized client IP")
	}
	if s.hasOpenWindow(target, net.ParseIP("10.0.0.1")) {
		t.Fatal("expected window to reject a different client IP")
	}
	if s.hasOpenWindow(access.TargetID("other-target"), client) {
		t.Fatal("expected window to be scoped to its target")
	}
}

func TestOpenWriteWindow_TTLExpiry(t *testing.T) {
	s := NewServer(newTestTargetStore(t), t.TempDir())
	target := access.TargetID("t1")
	client := net.ParseIP("192.168.1.99")

	s.OpenWriteWindow(target, client, 10*time.Millisecond)
	if !s.hasOpenWindow(target, client) {
		t.Fatal("expected window open immediately after OpenWriteWindow")
	}
	time.Sleep(50 * time.Millisecond)
	if s.hasOpenWindow(target, client) {
		t.Fatal("expected window to have expired")
	}
}

func TestOpenWriteWindow_NonPositiveTTLActsAsClose(t *testing.T) {
	s := NewServer(newTestTargetStore(t), t.TempDir())
	target := access.TargetID("t1")
	client := net.ParseIP("192.168.1.99")

	s.OpenWriteWindow(target, client, time.Minute)
	s.OpenWriteWindow(target, client, 0)
	if s.hasOpenWindow(target, client) {
		t.Fatal("expected ttl<=0 to close the window")
	}
}

func TestCloseWriteWindow(t *testing.T) {
	s := NewServer(newTestTargetStore(t), t.TempDir())
	target := access.TargetID("t1")
	client := net.ParseIP("192.168.1.99")

	s.OpenWriteWindow(target, client, time.Minute)
	if !s.hasOpenWindow(target, client) {
		t.Fatal("expected window open")
	}
	s.CloseWriteWindow(target)
	if s.hasOpenWindow(target, client) {
		t.Fatal("expected window closed")
	}
	// Closing an already-closed window must not panic.
	s.CloseWriteWindow(target)
}

func TestWriteWindowStatus(t *testing.T) {
	s := NewServer(newTestTargetStore(t), t.TempDir())
	target := access.TargetID("t1")
	client := net.ParseIP("192.168.1.99")

	if _, ok := s.WriteWindowStatus(target); ok {
		t.Fatal("expected no status before any window is opened")
	}

	s.OpenWriteWindow(target, client, time.Minute)
	info, ok := s.WriteWindowStatus(target)
	if !ok {
		t.Fatal("expected status to report the open window")
	}
	if !info.ClientIP.Equal(client) {
		t.Fatalf("expected ClientIP %v, got %v", client, info.ClientIP)
	}
	if info.ExpiresAt.Before(time.Now()) {
		t.Fatalf("expected ExpiresAt in the future, got %v", info.ExpiresAt)
	}

	s.CloseWriteWindow(target)
	if _, ok := s.WriteWindowStatus(target); ok {
		t.Fatal("expected no status after the window is closed")
	}
}

func TestWriteWindowStatus_ExpiresAndPrunes(t *testing.T) {
	s := NewServer(newTestTargetStore(t), t.TempDir())
	target := access.TargetID("t1")
	client := net.ParseIP("192.168.1.99")

	s.OpenWriteWindow(target, client, 10*time.Millisecond)
	if _, ok := s.WriteWindowStatus(target); !ok {
		t.Fatal("expected status to report the open window immediately")
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok := s.WriteWindowStatus(target); ok {
		t.Fatal("expected status to report expired window as closed")
	}
	s.windowMu.Lock()
	_, stillPresent := s.windows[target]
	s.windowMu.Unlock()
	if stillPresent {
		t.Fatal("expected the expired window entry to be pruned as a side effect")
	}
}

func TestAuthorize_WriteRequiresOpenWindow(t *testing.T) {
	s := NewServer(newTestTargetStore(t), t.TempDir())
	client := net.ParseIP("192.168.1.50") // matches t1's stored Host

	if _, _, err := s.authorize(client, "t1/config.txt", true); err == nil {
		t.Fatal("expected write to be denied before a window is opened")
	}

	s.OpenWriteWindow(access.TargetID("t1"), client, time.Minute)

	target, relPath, err := s.authorize(client, "t1/config.txt", true)
	if err != nil {
		t.Fatalf("expected write to be allowed once the window is open: %v", err)
	}
	if target.ID != "t1" || relPath != "config.txt" {
		t.Fatalf("unexpected authorize result: target=%+v relPath=%q", target, relPath)
	}

	// A write from a different client IP must still be denied even though
	// a window is open for the target, since the window is IP-pinned.
	if _, _, err := s.authorize(net.ParseIP("192.168.1.51"), "t1/config.txt", true); err == nil {
		t.Fatal("expected write from a different client IP to be denied")
	}
}
