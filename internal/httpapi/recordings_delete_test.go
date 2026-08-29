package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedRecordingFile(t *testing.T, app *App, id, owner string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VANTYX_RECORDINGS_DIR", dir)
	castPath := filepath.Join(dir, id+".cast")
	if err := os.WriteFile(castPath, []byte(`{"version":2,"width":80,"height":24}`+"\n"), 0o600); err != nil {
		t.Fatalf("write cast: %v", err)
	}
	if err := app.InsertRecording(context.Background(), id, owner, "t1", "s-"+id, "browser", castPath, time.Now().UTC().Format(time.RFC3339), "", ""); err != nil {
		t.Fatalf("InsertRecording: %v", err)
	}
	return castPath
}

func deleteRecordingAs(t *testing.T, app *App, actor, id string) *httptest.ResponseRecorder {
	t.Helper()
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create(actor)
	req := httptest.NewRequest(http.MethodDelete, "/api/recordings/"+id, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func recordingRowExists(t *testing.T, app *App, id string) bool {
	t.Helper()
	var n int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM recordings WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count recordings: %v", err)
	}
	return n > 0
}

// TestApp_DeleteRecording_RemovesRowAndFile is the E-13 regression guard:
// DELETE /api/recordings/{id} (previously 405) removes the DB row and the
// media file and leaves a recording_deleted audit entry.
func TestApp_DeleteRecording_RemovesRowAndFile(t *testing.T) {
	app := newTestApp(t)
	castPath := seedRecordingFile(t, app, "rec-del-1", "admin")

	w := deleteRecordingAs(t, app, "admin", "rec-del-1")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	if recordingRowExists(t, app, "rec-del-1") {
		t.Fatal("expected recordings row to be deleted")
	}
	if _, err := os.Stat(castPath); !os.IsNotExist(err) {
		t.Fatalf("expected media file to be removed, stat err=%v", err)
	}
	e, ok := latestAuditEvent("recording_deleted")
	if !ok {
		t.Fatal("expected a recording_deleted audit entry")
	}
	if e.Fields["recording_id"] != "rec-del-1" || e.Fields["user_id"] != "admin" || e.Fields["file_removed"] != true {
		t.Fatalf("unexpected recording_deleted fields: %v", e.Fields)
	}
}

// TestApp_DeleteRecording_AdminOnlyAndNotFound: ordinary users (even the
// owner) get 403, and an unknown id gets 404.
func TestApp_DeleteRecording_AdminOnlyAndNotFound(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.UserStore.CreateUser("bob", "bob", "BobPass1!", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	castPath := seedRecordingFile(t, app, "rec-del-2", "bob")

	if w := deleteRecordingAs(t, app, "bob", "rec-del-2"); w.Code != http.StatusForbidden {
		t.Fatalf("owner but non-admin: expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	if !recordingRowExists(t, app, "rec-del-2") {
		t.Fatal("row must survive a forbidden delete")
	}
	if _, err := os.Stat(castPath); err != nil {
		t.Fatalf("file must survive a forbidden delete: %v", err)
	}
	if w := deleteRecordingAs(t, app, "admin", "no-such-recording"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id: expected 404, got %d", w.Code)
	}
}

// TestApp_DeleteRecording_ContainmentRefusesOutsideDir: a row whose
// file_path escapes VANTYX_RECORDINGS_DIR must not cause a delete outside
// the recordings directory (the row is still removed; the file is left
// alone and the audit entry carries the error).
func TestApp_DeleteRecording_ContainmentRefusesOutsideDir(t *testing.T) {
	app := newTestApp(t)
	t.Setenv("VANTYX_RECORDINGS_DIR", t.TempDir())
	outside := filepath.Join(t.TempDir(), "outside.cast")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := app.InsertRecording(context.Background(), "rec-out", "admin", "t1", "s-out", "browser", outside, time.Now().UTC().Format(time.RFC3339), "", ""); err != nil {
		t.Fatalf("InsertRecording: %v", err)
	}

	w := deleteRecordingAs(t, app, "admin", "rec-out")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the recordings dir must not be deleted: %v", err)
	}
	e, ok := latestAuditEvent("recording_deleted")
	if !ok || e.Fields["file_removed"] != false {
		t.Fatalf("expected recording_deleted with file_removed=false, got ok=%v fields=%v", ok, e.Fields)
	}
}
