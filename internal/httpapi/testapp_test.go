package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

// newTestAppWithDB boots an App backed by a copy of the pre-migrated template DB
// (see TestMain). Without the template, each test pays a full schema migration.
func newTestAppWithDB(t *testing.T, dbFile string) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), dbFile)
	if len(httpapiTestDBTemplate) > 0 {
		if err := os.WriteFile(dbPath, httpapiTestDBTemplate, 0o600); err != nil {
			t.Fatalf("write template db: %v", err)
		}
	}
	t.Setenv("VANTYX_SQLITE_PATH", dbPath)
	t.Setenv(initialAdminPasswordEnv, "Admin123!")
	app := NewApp()
	if app != nil && app.UserStore != nil {
		_ = app.UserStore.SetForcePasswordChange("admin", false)
	}
	t.Cleanup(func() {
		_ = closeAuditSink()
		// Allow bridge / WebSocket goroutines to flush before SQLite teardown.
		time.Sleep(50 * time.Millisecond)
		if app != nil && app.DB != nil {
			_, _ = app.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			_ = app.DB.Close()
		}
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	})
	return app
}

func newTestApp(t *testing.T) *App {
	return newTestAppWithDB(t, "httpapi.db")
}

func newTestAppForTerminal(t *testing.T) *App {
	return newTestAppWithDB(t, "terminal.db")
}

func newTestAppForVNC(t *testing.T) *App {
	return newTestAppWithDB(t, "vnc.db")
}

// seedAdminDemoSSHTarget grants admin access to a demo SSH target via group g1.
func seedAdminDemoSSHTarget(t *testing.T, app *App) {
	t.Helper()
	ctx := context.Background()
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1"); err != nil {
		t.Fatalf("Create group: %v", err)
	}
	if err := app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1")); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if _, err := app.TargetStore.CreateWithPath(ctx, access.TargetID("demo"), "Demo host", "127.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	if err := app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("demo")); err != nil {
		t.Fatalf("AddTargetToGroup: %v", err)
	}
	ids, err := app.AccessGroupStore.TargetIDsForUser(ctx, access.UserID("admin"), nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	for _, id := range ids {
		if id == access.TargetID("demo") {
			return
		}
	}
	t.Fatalf("admin cannot access demo target; ids=%v", ids)
}

// startTestWSServer binds an httptest server to 127.0.0.1 so Origin checks stay
// stable across IPv4/IPv6 CI runners.
func startTestWSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = ln
	srv.Start()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}

func wsDialHeaders(t *testing.T, srv *httptest.Server, sessionID string) http.Header {
	t.Helper()
	header := http.Header{}
	header.Set("Origin", "http://"+srv.Listener.Addr().String())
	if sessionID != "" {
		header.Add("Cookie", (&http.Cookie{Name: "vantyx_session", Value: sessionID, Path: "/"}).String())
	}
	return header
}

func withTerminalSessionIDGen(t *testing.T, gen func() session.ID) {
	t.Helper()
	terminalSessionIDGen = gen
	t.Cleanup(func() { terminalSessionIDGen = nil })
}
