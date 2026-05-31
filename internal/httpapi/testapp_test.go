package httpapi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
