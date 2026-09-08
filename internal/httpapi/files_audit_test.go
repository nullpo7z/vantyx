package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// latestAuditEvent returns the most recent in-memory audit entry with the
// given event name, or false when none was recorded.
func latestAuditEvent(event string) (AuditEntry, bool) {
	items := auditBuffer.listNewestFirst(1, 0, func(e AuditEntry) bool { return e.Event == event })
	if len(items) == 0 {
		return AuditEntry{}, false
	}
	return items[0], true
}

// TestApp_Files_Delete_AuditsSuccessWithUser is the E-4 regression guard:
// a successful file deletion must leave a files_remove_ok audit entry that
// names the acting user, the target and the path (previously only the
// failure path was audited).
func TestApp_Files_Delete_AuditsSuccessWithUser(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.AddFile("/audited-delete.txt", []byte("x"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/"+targetID+"/files?path=/audited-delete.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}

	e, ok := latestAuditEvent("files_remove_ok")
	if !ok {
		t.Fatal("expected a files_remove_ok audit entry after a successful delete")
	}
	if got := e.Fields["user_id"]; got != "admin" {
		t.Fatalf("files_remove_ok user_id = %v, want admin", got)
	}
	if got := e.Fields["path"]; got != "/audited-delete.txt" {
		t.Fatalf("files_remove_ok path = %v, want /audited-delete.txt", got)
	}
	if got := e.Fields["target_id"]; got == nil || got == "" {
		t.Fatalf("files_remove_ok target_id missing: %v", e.Fields)
	}
}

// TestApp_Files_Download_AuditsSuccessWithUser: a successful download must
// leave a files_download_ok audit entry naming the acting user.
func TestApp_Files_Download_AuditsSuccessWithUser(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.AddFile("/audited-download.txt", []byte("hello world"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files/download?path=/audited-download.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}

	e, ok := latestAuditEvent("files_download_ok")
	if !ok {
		t.Fatal("expected a files_download_ok audit entry after a successful download")
	}
	if got := e.Fields["user_id"]; got != "admin" {
		t.Fatalf("files_download_ok user_id = %v, want admin", got)
	}
	if got := e.Fields["path"]; got != "/audited-download.txt" {
		t.Fatalf("files_download_ok path = %v, want /audited-download.txt", got)
	}
}
