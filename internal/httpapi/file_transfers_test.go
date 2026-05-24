package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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
