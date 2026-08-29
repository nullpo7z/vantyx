package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
)

func TestRetention_PurgesOldDataOnly(t *testing.T) {
	recDir := t.TempDir()
	t.Setenv("VANTYX_RECORDINGS_DIR", recDir)
	t.Setenv(retentionEnvRecordings, "720h")
	t.Setenv(retentionEnvAudit, "48h")
	t.Setenv(retentionEnvCommandLogs, "")
	t.Setenv(retentionEnvMemberships, "24h")
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	p := app.retentionPolicy()
	if p.Recordings != 720*time.Hour || p.Audit != 48*time.Hour || p.CommandLogs != 48*time.Hour || p.Memberships != 24*time.Hour {
		t.Fatalf("policy = %+v", p)
	}

	// Recordings: one old (with file), one recent, one still running.
	oldFile := filepath.Join(recDir, "old.cast")
	newFile := filepath.Join(recDir, "new.cast")
	for _, f := range []string{oldFile, newFile} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ins := func(id, path, started, ended string) {
		if _, err := app.DB.ExecContext(ctx, `INSERT INTO recordings (id, user_id, target_id, session_id, channel_type, file_path, started_at, ended_at) VALUES (?, 'admin', 't', ?, 'browser', ?, ?, ?)`, id, id, path, started, ended); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-40 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	recent := time.Now().Add(-2 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	ins("rec-old", oldFile, old, old)
	ins("rec-new", newFile, recent, recent)
	if _, err := app.DB.ExecContext(ctx, `INSERT INTO recordings (id, user_id, target_id, session_id, channel_type, file_path, started_at, ended_at) VALUES ('rec-live', 'admin', 't', 'rec-live', 'browser', ?, ?, NULL)`, filepath.Join(recDir, "live.cast"), old); err != nil {
		t.Fatal(err)
	}
	// Audit + command rows.
	for _, tc := range []struct {
		event string
		at    time.Time
	}{{"old_event", time.Now().Add(-72 * time.Hour)}, {"new_event", time.Now().Add(-time.Hour)}} {
		if _, err := app.DB.ExecContext(ctx, `INSERT INTO audit_logs (time, event) VALUES (?, ?)`, tc.at.UTC(), tc.event); err != nil {
			t.Fatal(err)
		}
		if _, err := app.DB.ExecContext(ctx, `INSERT INTO command_logs (session_id, user_id, target_id, time, line_text) VALUES ('s', 'admin', 't', ?, ?)`, tc.at.UTC(), tc.event); err != nil {
			t.Fatal(err)
		}
	}
	// Memberships: expired long ago, expired recently, live.
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"g-old", "g-recent", "g-live"} {
		if _, err := app.AccessGroupStore.Create(ctx, access.GroupID(g), g); err != nil {
			t.Fatal(err)
		}
	}
	longAgo := time.Now().Add(-48 * time.Hour)
	justNow := time.Now().Add(-time.Hour)
	_ = app.AccessGroupStore.AddUserToGroupUntil(ctx, "bob", "g-old", &longAgo)
	_ = app.AccessGroupStore.AddUserToGroupUntil(ctx, "bob", "g-recent", &justNow)
	_ = app.AccessGroupStore.AddUserToGroup(ctx, "bob", "g-live")

	rep := app.runRetention(ctx, "test")
	if len(rep.Errors) != 0 {
		t.Fatalf("errors: %v", rep.Errors)
	}
	if rep.RecordingsDeleted != 1 || rep.RecordingFilesGone != 1 || rep.AuditRowsDeleted != 1 || rep.CommandRowsDeleted != 1 || rep.MembershipsPurged != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("old recording file still exists")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatal("recent recording file was removed")
	}
	var n int
	_ = app.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM recordings`).Scan(&n)
	if n != 2 {
		t.Fatalf("recordings left = %d, want 2 (recent + live)", n)
	}
	_ = app.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE event = 'old_event'`).Scan(&n)
	if n != 0 {
		t.Fatal("old audit row survived")
	}
	_ = app.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE event = 'new_event'`).Scan(&n)
	if n != 1 {
		t.Fatal("recent audit row removed")
	}
	rows, _ := app.AccessGroupStore.MembershipsForGroup(ctx, "g-recent")
	if len(rows) != 1 {
		t.Fatal("recently expired membership purged too early")
	}
	rows, _ = app.AccessGroupStore.MembershipsForGroup(ctx, "g-old")
	if len(rows) != 0 {
		t.Fatal("long-expired membership survived")
	}

	// Admin endpoints.
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create("bob")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/settings/retention", nil, bobSess.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/settings/retention", nil, adminSess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	got := decodeJSON(t, w)
	if got["recordings"] != "720h0m0s" || got["enabled"] != true || got["last_run"] == nil {
		t.Fatalf("policy response = %v", got)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/settings/retention/run", nil, adminSess.ID))
	if w.Code != http.StatusOK || decodeJSON(t, w)["trigger"] != "manual:admin" {
		t.Fatalf("run: %d %s", w.Code, w.Body.String())
	}
}

func TestRetention_DisabledByDefault(t *testing.T) {
	for _, e := range []string{retentionEnvRecordings, retentionEnvAudit, retentionEnvCommandLogs, retentionEnvMemberships} {
		t.Setenv(e, "")
	}
	p := retentionPolicyFromEnv()
	if p.Recordings != 0 || p.Audit != 0 || p.CommandLogs != 0 || p.Memberships != retentionDefaultMembers {
		t.Fatalf("defaults = %+v", p)
	}
	t.Setenv(retentionEnvMemberships, "0")
	t.Setenv(retentionEnvAudit, "10m") // below the floor -> 1h
	p = retentionPolicyFromEnv()
	if p.Memberships != 0 || p.Audit != time.Hour || p.CommandLogs != time.Hour {
		t.Fatalf("parsed = %+v", p)
	}
}
