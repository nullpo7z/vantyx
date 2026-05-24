package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	pintftp "github.com/pin/tftp/v3"

	"github.com/nullpo7z/vantyx/internal/access"
)

func startTestTFTPServer(t *testing.T) (host string, port uint16, cleanup func()) {
	t.Helper()
	files := map[string][]byte{
		"test.txt": []byte("hello tftp client"),
	}
	var mu sync.Mutex

	readHandler := func(filename string, rf io.ReaderFrom) error {
		mu.Lock()
		data, ok := files[filename]
		mu.Unlock()
		if !ok {
			return fmt.Errorf("file not found: %s", filename)
		}
		_, err := rf.ReadFrom(bytes.NewReader(data))
		return err
	}
	writeHandler := func(filename string, wt io.WriterTo) error {
		pr, pw := io.Pipe()
		go func() {
			_, _ = wt.WriteTo(pw)
			_ = pw.Close()
		}()
		data, err := io.ReadAll(pr)
		if err != nil {
			return err
		}
		mu.Lock()
		files[filename] = data
		mu.Unlock()
		return nil
	}

	srv := pintftp.NewServer(readHandler, writeHandler)
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	udpAddr := l.LocalAddr().(*net.UDPAddr)
	_ = l.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", udpAddr.Port)

	done := make(chan struct{})
	go func() {
		_ = srv.ListenAndServe(addr)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)

	return "127.0.0.1", uint16(udpAddr.Port), func() {
		srv.Shutdown()
		<-done
	}
}

func setupRemoteTFTPTarget(t *testing.T, app *App, host string, port uint16) access.TargetID {
	t.Helper()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	id := access.TargetID("tftp-remote")
	if _, err := app.TargetStore.CreateWithPath(ctx, id, "Remote TFTP", host, port, access.ProtocolTFTP, access.GroupID("g1"), "", "", "", "", "", false, false, false); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), id)
	return id
}

func TestFilesTFTPClient_Download(t *testing.T) {
	host, port, stop := startTestTFTPServer(t)
	defer stop()

	app := newTestApp(t)
	targetID := setupRemoteTFTPTarget(t, app, host, port)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+string(targetID)+"/files/download?path=/test.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	if got := w.Body.String(); got != "hello tftp client" {
		t.Fatalf("body: got %q", got)
	}
}

func TestFilesTFTPClient_Upload(t *testing.T) {
	host, port, stop := startTestTFTPServer(t)
	defer stop()

	app := newTestApp(t)
	targetID := setupRemoteTFTPTarget(t, app, host, port)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	_ = mp.WriteField("path", "/uploaded.txt")
	fw, _ := mp.CreateFormFile("file", "uploaded.txt")
	_, _ = fw.Write([]byte("uploaded content"))
	_ = mp.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/targets/"+string(targetID)+"/files/upload", &buf)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("upload: expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/targets/"+string(targetID)+"/files/download?path=/uploaded.txt", nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("download uploaded: expected 200, got %d", w2.Result().StatusCode)
	}
	if got := w2.Body.String(); got != "uploaded content" {
		t.Fatalf("uploaded body: got %q", got)
	}
}

func TestFilesTFTPClient_DeleteNotSupported(t *testing.T) {
	host, port, stop := startTestTFTPServer(t)
	defer stop()

	app := newTestApp(t)
	targetID := setupRemoteTFTPTarget(t, app, host, port)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodDelete, "/api/targets/"+string(targetID)+"/files?path=/test.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Result().StatusCode)
	}
}
