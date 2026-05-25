package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/filetransfer"
)

func TestFileTransferBackgroundDownload(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.AddFile("/bg.txt", []byte("background-data"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	startBody := []byte(`{"backend":"remote","target_id":"` + targetID + `","path":"/bg.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("start: got %d %s", w.Result().StatusCode, w.Body.String())
	}
	var snap filetransfer.JobSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.ID == "" {
		t.Fatal("missing transfer id")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+snap.ID, nil)
		req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Result().StatusCode != http.StatusOK {
			t.Fatalf("poll: %d", w.Result().StatusCode)
		}
		_ = json.NewDecoder(w.Body).Decode(&snap)
		if snap.State == string(filetransfer.StateCompleted) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if snap.State != string(filetransfer.StateCompleted) {
		t.Fatalf("state=%s err=%s", snap.State, snap.Error)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+snap.ID+"/content", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("content: %d", w.Result().StatusCode)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "background-data" {
		t.Fatalf("body=%q", body)
	}
}

func TestFileTransferBackgroundUpload(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("backend", "remote")
	_ = mw.WriteField("target_id", targetID)
	_ = mw.WriteField("path", "/uploaded.txt")
	fw, _ := mw.CreateFormFile("file", "uploaded.txt")
	_, _ = fw.Write([]byte("upload-payload"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("upload start: %d %s", w.Result().StatusCode, w.Body.String())
	}
	var snap filetransfer.JobSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+snap.ID, nil)
		req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		_ = json.NewDecoder(w.Body).Decode(&snap)
		if snap.State == string(filetransfer.StateCompleted) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if snap.State != string(filetransfer.StateCompleted) {
		t.Fatalf("state=%s err=%s", snap.State, snap.Error)
	}
}

func TestFileTransfersList(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.AddFile("/list.txt", []byte("x"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	startBody := []byte(`{"backend":"remote","target_id":"` + targetID + `","path":"/list.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("start: %d", w.Result().StatusCode)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", w.Result().StatusCode, w.Body.String())
	}
	var out struct {
		Items []filetransfer.JobSnapshot `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) == 0 {
		t.Fatal("expected at least one transfer in list")
	}
}

func TestFileTransferDelete(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.AddFile("/big.txt", bytes.Repeat([]byte("a"), 1024*1024))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	startBody := []byte(`{"backend":"remote","target_id":"` + targetID + `","path":"/big.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var snap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&snap)

	req = httptest.NewRequest(http.MethodDelete, "/api/file-transfers/"+snap.ID, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Result().StatusCode, w.Body.String())
	}
}

func TestFileTransfers_Unauthorized(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/file-transfers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestFileTransferTFTPServerBackgroundUploadDownload(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	// Upload via background job
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("backend", "tftp_server")
	_ = mw.WriteField("target_id", targetID)
	_ = mw.WriteField("path", "/bg-upload.txt")
	fw, _ := mw.CreateFormFile("file", "bg-upload.txt")
	_, _ = fw.Write([]byte("tftp-server-bg"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("upload: %d %s", w.Result().StatusCode, w.Body.String())
	}
	var upSnap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&upSnap)
	waitTransferDone(t, router, cookie, &upSnap)

	full := filepath.Join(root, targetID, "bg-upload.txt")
	if data, err := os.ReadFile(full); err != nil || string(data) != "tftp-server-bg" {
		t.Fatalf("file on disk: err=%v data=%q", err, data)
	}

	// Download via background job
	startBody := []byte(`{"backend":"tftp_server","target_id":"` + targetID + `","path":"/bg-upload.txt"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("download start: %d", w.Result().StatusCode)
	}
	var dlSnap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&dlSnap)
	waitTransferDone(t, router, cookie, &dlSnap)

	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+dlSnap.ID+"/content", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("content: %d", w.Result().StatusCode)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "tftp-server-bg" {
		t.Fatalf("body=%q", body)
	}
}

func TestFileTransferDelete_ForbiddenOtherUser(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.AddFile("/other.txt", []byte("x"))
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("u2", "user2", "User123!", "")
	adminSess, _ := app.SessionStore.Create("admin")
	user2Sess, err := app.SessionStore.Create("u2")
	if err != nil || user2Sess == nil {
		t.Fatalf("user2 session: %v", err)
	}

	startBody := []byte(`{"backend":"remote","target_id":"` + targetID + `","path":"/other.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var snap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&snap)

	req = httptest.NewRequest(http.MethodDelete, "/api/file-transfers/"+snap.ID, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: user2Sess.ID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d %s", w.Result().StatusCode, w.Body.String())
	}
}

func TestFileTransfersList_UserIsolation(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.AddFile("/iso.txt", []byte("x"))
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("u2", "user2", "User123!", "")
	adminSess, _ := app.SessionStore.Create("admin")
	user2Sess, err := app.SessionStore.Create("u2")
	if err != nil || user2Sess == nil {
		t.Fatalf("user2 session: %v", err)
	}

	startBody := []byte(`{"backend":"remote","target_id":"` + targetID + `","path":"/iso.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: user2Sess.ID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var out struct {
		Items []filetransfer.JobSnapshot `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&out)
	for _, item := range out.Items {
		if item.TargetID == targetID {
			t.Fatal("user2 should not see admin transfer jobs")
		}
	}
}

func TestFileTransferDelete_NotFound(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/file-transfers/not-a-real-id", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestFileTransferStartDownload_InvalidBackend(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"backend":"invalid","target_id":"` + targetID + `","path":"/x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d %s", w.Result().StatusCode, w.Body.String())
	}
}

func TestFileTransferGet_NotFound(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/file-transfers/does-not-exist", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestFileTransferContent_NotDownload(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("backend", "remote")
	_ = mw.WriteField("target_id", targetID)
	_ = mw.WriteField("path", "/up.txt")
	fw, _ := mw.CreateFormFile("file", "up.txt")
	_, _ = fw.Write([]byte("x"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var snap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&snap)

	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+snap.ID+"/content", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for upload job content, got %d", w.Result().StatusCode)
	}
}

func TestFileTransferBackgroundDownload_OpenFails(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.OpenErr = errors.New("mock open failed")
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	startBody := []byte(`{"backend":"remote","target_id":"` + targetID + `","path":"/missing.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var snap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&snap)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+snap.ID, nil)
		req.AddCookie(cookie)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		_ = json.NewDecoder(w.Body).Decode(&snap)
		if snap.State == string(filetransfer.StateFailed) {
			if snap.Error == "" {
				t.Fatal("expected error message")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("state=%s", snap.State)
}

func TestFileTransferBackgroundUpload_CreateFails(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.CreateErr = errors.New("mock create failed")
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("backend", "remote")
	_ = mw.WriteField("target_id", targetID)
	_ = mw.WriteField("path", "/fail.txt")
	fw, _ := mw.CreateFormFile("file", "fail.txt")
	_, _ = fw.Write([]byte("payload"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("upload start: %d", w.Result().StatusCode)
	}
	var snap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&snap)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+snap.ID, nil)
		req.AddCookie(cookie)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		_ = json.NewDecoder(w.Body).Decode(&snap)
		if snap.State == string(filetransfer.StateFailed) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected failed state, got %s err=%s", snap.State, snap.Error)
}

func TestFileTransferUpload_InvalidBackend(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("backend", "nope")
	_ = mw.WriteField("target_id", targetID)
	_ = mw.WriteField("path", "/bad.txt")
	fw, _ := mw.CreateFormFile("file", "bad.txt")
	_, _ = fw.Write([]byte("x"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", w.Result().StatusCode)
	}
}

func TestFileTransferReapOrphansOnRestart(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)

	// Seed an in-flight job directly into the DB to simulate a previous process
	// that exited mid-transfer.
	now := time.Now().UTC()
	if _, err := app.DB.Exec(`
		INSERT INTO file_transfer_jobs
			(id, user_id, target_id, target_name, backend, direction, remote_path, file_name,
			 state, progress, total, error, temp_path, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`, "stuck-1", "admin", "t1", "T1", "remote", "download", "/x", "x.bin",
		"running", 5, 100, "", "", now.Add(-time.Hour), now.Add(-time.Hour),
	); err != nil {
		t.Fatalf("seed stuck job: %v", err)
	}

	// Reap orphans (simulates what NewApp does on startup).
	if _, err := app.FileTransferManager.ReapOrphans(context.Background(), "サーバー再起動により中断"); err != nil {
		t.Fatalf("reap: %v", err)
	}

	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/file-transfers/stuck-1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("get after reap: %d %s", w.Result().StatusCode, w.Body.String())
	}
	var snap filetransfer.JobSnapshot
	if err := json.NewDecoder(w.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.State != string(filetransfer.StateFailed) {
		t.Fatalf("expected failed, got %s", snap.State)
	}
	if snap.Error == "" {
		t.Fatal("expected non-empty error after reap")
	}
}

func TestFileTransferHistoryPersistsAfterCompletion(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.AddFile("/persist.txt", []byte("hello"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	startBody := []byte(`{"backend":"remote","target_id":"` + targetID + `","path":"/persist.txt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file-transfers/download", bytes.NewReader(startBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var snap filetransfer.JobSnapshot
	_ = json.NewDecoder(w.Body).Decode(&snap)

	waitTransferDone(t, router, cookie, &snap)

	// After completion the entry must still be visible in the list endpoint.
	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var out struct {
		Items []filetransfer.JobSnapshot `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&out)
	found := false
	for _, it := range out.Items {
		if it.ID == snap.ID && it.State == string(filetransfer.StateCompleted) {
			found = true
		}
	}
	if !found {
		t.Fatalf("completed job missing from history: %+v", out.Items)
	}
}

// seedFileTransferJob inserts a finished/historic job row directly into the DB.
// This avoids the cost (and ordering noise) of going through the HTTP upload/download flow.
func seedFileTransferJob(t *testing.T, app *App, rec filetransfer.JobRecord) {
	t.Helper()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = rec.CreatedAt
	}
	if rec.Backend == "" {
		rec.Backend = filetransfer.BackendRemote
	}
	if rec.Direction == "" {
		rec.Direction = filetransfer.DirectionDownload
	}
	if rec.State == "" {
		rec.State = filetransfer.StateCompleted
	}
	if _, err := app.DB.Exec(`
		INSERT INTO file_transfer_jobs
			(id, user_id, target_id, target_name, backend, direction, remote_path, file_name,
			 state, progress, total, error, temp_path, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`, rec.ID, rec.UserID, rec.TargetID, rec.TargetName, string(rec.Backend), string(rec.Direction),
		rec.RemotePath, rec.FileName, string(rec.State), rec.Progress, rec.Total, rec.Error,
		rec.TempPath, rec.CreatedAt, rec.UpdatedAt,
	); err != nil {
		t.Fatalf("seed job %s: %v", rec.ID, err)
	}
}

func TestFileTransfersList_FiltersByStateAndQuery(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)
	now := time.Now().UTC()
	seedFileTransferJob(t, app, filetransfer.JobRecord{
		ID: "j-completed", UserID: "admin", TargetID: "t1", TargetName: "Tokyo",
		Backend: filetransfer.BackendRemote, Direction: filetransfer.DirectionDownload,
		RemotePath: "/data/report.pdf", FileName: "report.pdf",
		State:     filetransfer.StateCompleted,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})
	seedFileTransferJob(t, app, filetransfer.JobRecord{
		ID: "j-failed", UserID: "admin", TargetID: "t1", TargetName: "Tokyo",
		Backend: filetransfer.BackendRemote, Direction: filetransfer.DirectionUpload,
		RemotePath: "/logs/error.log", FileName: "error.log",
		State:     filetransfer.StateFailed,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	})
	seedFileTransferJob(t, app, filetransfer.JobRecord{
		ID: "j-other", UserID: "admin", TargetID: "t1", TargetName: "Osaka",
		Backend: filetransfer.BackendTFTPServer, Direction: filetransfer.DirectionDownload,
		RemotePath: "/notes.txt", FileName: "notes.txt",
		State:     filetransfer.StateCompleted,
		CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now.Add(-30 * time.Minute),
	})

	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	// state=failed → only j-failed
	req := httptest.NewRequest(http.MethodGet, "/api/file-transfers?state=failed", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("state filter: %d %s", w.Result().StatusCode, w.Body.String())
	}
	var resp struct {
		Items      []filetransfer.JobSnapshot `json:"items"`
		NextCursor string                     `json:"next_cursor"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 || resp.Items[0].ID != "j-failed" {
		t.Fatalf("state=failed expected only j-failed, got %+v", resp.Items)
	}

	// query=report → only j-completed (file_name match)
	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers?query=report", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	resp.Items = nil
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 || resp.Items[0].ID != "j-completed" {
		t.Fatalf("query=report wrong: %+v", resp.Items)
	}

	// direction=upload + backend=remote → only j-failed
	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers?direction=upload&backend=remote", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	resp.Items = nil
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 || resp.Items[0].ID != "j-failed" {
		t.Fatalf("direction+backend wrong: %+v", resp.Items)
	}
}

func TestFileTransfersList_CursorPagination(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)
	now := time.Now().UTC()
	// Three rows with distinct updated_at in the past so the default
	// [now-30d, now) range filter includes them. Index 0 = oldest, 2 = newest.
	for i, id := range []string{"oldest", "middle", "newest"} {
		ts := now.Add(-time.Duration(3-i) * time.Hour)
		seedFileTransferJob(t, app, filetransfer.JobRecord{
			ID: id, UserID: "admin", TargetID: "t1", TargetName: "T",
			RemotePath: "/" + id + ".bin", FileName: id + ".bin",
			State:     filetransfer.StateCompleted,
			CreatedAt: ts, UpdatedAt: ts,
		})
	}
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	// Page 1 (limit=2): should be newest, middle
	req := httptest.NewRequest(http.MethodGet, "/api/file-transfers?limit=2", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var page1 struct {
		Items      []filetransfer.JobSnapshot `json:"items"`
		NextCursor string                     `json:"next_cursor"`
	}
	_ = json.NewDecoder(w.Body).Decode(&page1)
	if len(page1.Items) != 2 || page1.Items[0].ID != "newest" || page1.Items[1].ID != "middle" {
		t.Fatalf("page1 wrong: %+v", page1.Items)
	}
	if page1.NextCursor == "" {
		t.Fatal("expected next_cursor on page1")
	}

	// Page 2 using cursor: should be oldest, no further cursor
	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers?limit=2&after_cursor="+page1.NextCursor, nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var page2 struct {
		Items      []filetransfer.JobSnapshot `json:"items"`
		NextCursor string                     `json:"next_cursor"`
	}
	_ = json.NewDecoder(w.Body).Decode(&page2)
	if len(page2.Items) != 1 || page2.Items[0].ID != "oldest" {
		t.Fatalf("page2 wrong: %+v", page2.Items)
	}
	if page2.NextCursor != "" {
		t.Fatalf("expected no next_cursor on last page, got %q", page2.NextCursor)
	}
}

func TestFileTransfersList_AdminUserOverride(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)
	_, _ = app.UserStore.CreateUser("u2", "user2", "User123!", "")
	now := time.Now().UTC()
	seedFileTransferJob(t, app, filetransfer.JobRecord{
		ID: "ad-job", UserID: "admin", TargetID: "t1", FileName: "a.bin",
		State: filetransfer.StateCompleted, CreatedAt: now, UpdatedAt: now,
	})
	seedFileTransferJob(t, app, filetransfer.JobRecord{
		ID: "u2-job", UserID: "u2", TargetID: "t1", FileName: "b.bin",
		State: filetransfer.StateCompleted, CreatedAt: now, UpdatedAt: now,
	})

	router := app.NewRouter()
	adminSess, _ := app.SessionStore.Create("admin")
	u2Sess, _ := app.SessionStore.Create("u2")

	// Admin requests user_id=u2 → should see u2's jobs.
	req := httptest.NewRequest(http.MethodGet, "/api/file-transfers?user_id=u2", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var adminResp struct {
		Items []filetransfer.JobSnapshot `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&adminResp)
	if len(adminResp.Items) != 1 || adminResp.Items[0].ID != "u2-job" {
		t.Fatalf("admin override failed: %+v", adminResp.Items)
	}

	// Non-admin requests user_id=admin → override is silently ignored; they only see their own.
	req = httptest.NewRequest(http.MethodGet, "/api/file-transfers?user_id=admin", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: u2Sess.ID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var u2Resp struct {
		Items []filetransfer.JobSnapshot `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&u2Resp)
	for _, it := range u2Resp.Items {
		if it.ID == "ad-job" {
			t.Fatalf("non-admin must not see admin's jobs via user_id override: %+v", u2Resp.Items)
		}
	}
}

func TestFileTransfersList_RejectsInvalidInputs(t *testing.T) {
	app, _, _ := setupAppWithTargetAndSFTPMock(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	cases := []struct {
		name string
		url  string
	}{
		{"bad state", "/api/file-transfers?state=bogus"},
		{"bad direction", "/api/file-transfers?direction=sideways"},
		{"bad backend", "/api/file-transfers?backend=carrierpigeon"},
		{"bad cursor", "/api/file-transfers?after_cursor=%21%21%21not%20base64%21%21%21"},
		{"bad from", "/api/file-transfers?from=not-a-date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Result().StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d (%s)", tc.name, w.Result().StatusCode, w.Body.String())
			}
		})
	}
}

func waitTransferDone(t *testing.T, router http.Handler, cookie *http.Cookie, snap *filetransfer.JobSnapshot) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/file-transfers/"+snap.ID, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		_ = json.NewDecoder(w.Body).Decode(snap)
		if snap.State == string(filetransfer.StateCompleted) {
			return
		}
		if snap.State == string(filetransfer.StateFailed) {
			t.Fatalf("transfer failed: %s", snap.Error)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout state=%s err=%s", snap.State, snap.Error)
}
