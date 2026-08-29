package httpapi

import (
	"bytes"
	"context"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackups_CreateListDownloadRestore(t *testing.T) {
	bdir := t.TempDir()
	t.Setenv(backupEnvDir, bdir)
	t.Setenv(backupEnvKeep, "2")
	app := newTestApp(t)
	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	bobSess, _ := app.SessionStore.Create("bob")

	if w := (func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/settings/backups", nil, bobSess.ID))
		return w
	})(); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d", w.Code)
	}

	// Three backups with keep=2 -> the oldest is pruned.
	var names []string
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/settings/backups", nil, adminSess.ID))
		if w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		info := decodeJSON(t, w)
		name, _ := info["name"].(string)
		if !backupNameRe.MatchString(name) || info["sha256"] == "" {
			t.Fatalf("info = %v", info)
		}
		names = append(names, name)
	}
	list, err := app.listBackups()
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v (want 2 after prune)", list, err)
	}
	if err := verifySQLiteFile(filepath.Join(bdir, list[0].Name)); err != nil {
		t.Fatalf("backup not a valid db: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/settings/backups", nil, adminSess.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"keep":2`) || !strings.Contains(w.Body.String(), list[0].Name) {
		t.Fatalf("list api: %d %s", w.Code, w.Body.String())
	}
	// Download.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/settings/backups/"+list[0].Name, nil, adminSess.ID))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), list[0].Name) || !bytes.HasPrefix(w.Body.Bytes(), []byte("SQLite format 3")) {
		t.Fatalf("download: %d %q", w.Code, w.Header().Get("Content-Disposition"))
	}
	// Path traversal / bad names are 404.
	for _, bad := range []string{"..%2Fvantyx.db", "evil.db", "vantyx-20260101-000000.db.bak"} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/settings/backups/"+bad, nil, adminSess.ID))
		if w.Code != http.StatusNotFound {
			t.Fatalf("bad name %q: %d", bad, w.Code)
		}
	}

	// Stage a restore from a stored backup, then cancel it.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/settings/backups/restore", map[string]string{"name": list[0].Name}, adminSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("stage restore: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(app.pendingRestorePath()); err != nil {
		t.Fatal("pending restore file missing")
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/settings/backups", nil, adminSess.ID))
	if !strings.Contains(w.Body.String(), `"restore_pending":true`) {
		t.Fatalf("restore_pending flag: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/settings/backups/restore", nil, adminSess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("cancel restore: %d", w.Code)
	}
	if _, err := os.Stat(app.pendingRestorePath()); !os.IsNotExist(err) {
		t.Fatal("pending restore file still present after cancel")
	}

	// Upload path: garbage is rejected, a real backup is accepted.
	upload := func(name string, content []byte) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(content)
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/settings/backups/restore", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := upload("x.db", []byte("not a database at all")); w.Code != http.StatusBadRequest {
		t.Fatalf("garbage upload: %d %s", w.Code, w.Body.String())
	}
	good, _ := os.ReadFile(filepath.Join(bdir, list[0].Name))
	if w := upload("good.db", good); w.Code != http.StatusOK {
		t.Fatalf("good upload: %d %s", w.Code, w.Body.String())
	}
	_ = os.Remove(app.pendingRestorePath())

	// Delete a backup.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodDelete, "/api/settings/backups/"+list[1].Name, nil, adminSess.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if l, _ := app.listBackups(); len(l) != 1 {
		t.Fatalf("after delete: %v", l)
	}
	_ = context.Background()
}

func TestApplyPendingRestore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vantyx.db")
	// Build a real database to act as the snapshot, and a "current" db.
	t.Setenv("VANTYX_SQLITE_PATH", dbPath)
	app := newTestAppWithDB(t, "restore-src.db")
	src := os.Getenv("VANTYX_SQLITE_PATH")
	_, _ = app.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	snapshot := filepath.Join(dir, "snapshot.db")
	if _, err := app.DB.Exec(`VACUUM INTO ?`, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Nothing staged -> no-op.
	if _, applied, err := ApplyPendingRestore(dbPath); err != nil || applied {
		t.Fatalf("no-op: applied=%v err=%v", applied, err)
	}
	// Garbage staged -> rejected and renamed, current untouched.
	if err := os.WriteFile(dbPath+restorePendingExt, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, applied, err := ApplyPendingRestore(dbPath); err == nil || applied {
		t.Fatalf("junk: applied=%v err=%v", applied, err)
	}
	if b, _ := os.ReadFile(dbPath); string(b) != "current" {
		t.Fatal("current db modified by rejected restore")
	}
	// Valid snapshot staged -> swapped in, old moved aside, WAL removed.
	data, _ := os.ReadFile(snapshot)
	if err := os.WriteFile(dbPath+restorePendingExt, data, 0o600); err != nil {
		t.Fatal(err)
	}
	movedTo, applied, err := ApplyPendingRestore(dbPath)
	if err != nil || !applied || movedTo == "" {
		t.Fatalf("apply: movedTo=%q applied=%v err=%v", movedTo, applied, err)
	}
	if b, _ := os.ReadFile(movedTo); string(b) != "current" {
		t.Fatal("previous db not preserved")
	}
	if _, err := os.Stat(dbPath + "-wal"); !os.IsNotExist(err) {
		t.Fatal("stale WAL not removed")
	}
	if err := verifySQLiteFile(dbPath); err != nil {
		t.Fatalf("restored db invalid: %v", err)
	}
	_ = src
}

// Snapshots and staged restores never carry the sessions table: a backup
// file must not be a session-hijack kit, and restoring an old one must
// not resurrect sessions revoked since.
func TestBackups_StripSessions(t *testing.T) {
	bdir := t.TempDir()
	t.Setenv(backupEnvDir, bdir)
	app := newTestAppWithDB(t, "strip-sessions.db")
	if _, err := app.SessionStore.Create("admin"); err != nil {
		t.Fatal(err)
	}
	var live int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&live); err != nil || live == 0 {
		t.Fatalf("live sessions = %d, %v", live, err)
	}
	info, err := app.createBackup(context.Background(), "test")
	if err != nil {
		t.Fatalf("createBackup: %v", err)
	}
	countSessions := func(path string) int {
		db, err := dbsqlite.Open(dbsqlite.Config{Path: path, MaxOpenConns: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := countSessions(filepath.Join(bdir, info.Name)); n != 0 {
		t.Fatalf("backup carries %d sessions", n)
	}
	// The live database is untouched.
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&live); err != nil || live == 0 {
		t.Fatalf("live sessions after backup = %d, %v", live, err)
	}
	// Staging a file that does contain sessions scrubs the staged copy.
	src := filepath.Join(t.TempDir(), "with-sessions.db")
	if _, err := app.DB.Exec(`VACUUM INTO ?`, src); err != nil {
		t.Fatal(err)
	}
	if countSessions(src) == 0 {
		t.Fatal("test fixture should contain sessions")
	}
	if err := app.stageRestore(src, "admin", "test"); err != nil {
		t.Fatalf("stageRestore: %v", err)
	}
	if n := countSessions(a_pending(app)); n != 0 {
		t.Fatalf("staged restore carries %d sessions", n)
	}
}

func a_pending(app *App) string { return app.pendingRestorePath() }
