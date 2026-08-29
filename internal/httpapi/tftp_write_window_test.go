package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/tftp"
)

// GET, POST, and DELETE are all registered on this one path now, so PUT
// (still unregistered) is what exercises chi's routing-level 405 --
// there's no longer a "GET when only POST/DELETE exist" case to test.
func TestTFTPWriteWindow_MethodNotAllowed(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodPut, "/api/tftp/targets/"+targetID+"/write-window", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindow_Get_Unauthenticated(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/write-window", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

// A status GET always returns 200 with open:false, both when no window
// was ever opened and when the embedded server isn't running at all --
// "not running" trivially implies "nothing is open".
func TestTFTPWriteWindow_Get_ClosedByDefault(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/write-window", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var resp tftpWriteWindowStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if resp.Open {
		t.Fatalf("expected open=false, got %+v", resp)
	}
	if resp.TargetID != targetID {
		t.Fatalf("expected target_id %q, got %q", targetID, resp.TargetID)
	}
	if resp.ClientIP != "" || resp.ExpiresAt != "" {
		t.Fatalf("expected no client_ip/expires_at when closed, got %+v", resp)
	}
}

func TestTFTPWriteWindow_Get_NotTFTPTarget(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	seedAdminDemoSSHTarget(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/demo/write-window", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-TFTP-server target, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindow_Open_Unauthenticated(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+targetID+"/write-window", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindow_Open_InvalidJSON(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+targetID+"/write-window", bytes.NewReader([]byte("not json")))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

// An empty body is valid (ttl_seconds is optional): it must NOT be treated
// as invalid JSON, and client_ip is no longer accepted from the caller at
// all (see tftp_write_window.go's package doc for why).
func TestTFTPWriteWindow_Open_EmptyBodyIsValid(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+targetID+"/write-window", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// No embedded TFTP server is running in this test process, so this
	// still stops at 503 rather than invalid-JSON 400 -- the point of this
	// test is that it reaches that branch instead of failing on the body.
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (empty body accepted, reaches server-availability check), got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindow_Open_TargetHostNotIP(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	id := access.TargetID("tftp-hostname")
	if _, err := app.TargetStore.CreateWithPath(ctx, id, "TFTP hostname", "tftp.example.com", 69, access.ProtocolTFTP, "g1", "", "", "", "", "", false, false, true); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), id)

	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+string(id)+"/write-window", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a target whose Host is a hostname (not a literal IP), got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

// The test process never starts the process-wide embedded TFTP server
// (internal/tftp's controller is a global singleton that binds a real UDP
// listener), so a well-formed request reaches, and stops at, the "server
// not running" branch. This still exercises target access resolution and
// request validation ahead of it.
func TestTFTPWriteWindow_Open_ServerNotRunning(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	body := []byte(`{"ttl_seconds":60}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+targetID+"/write-window", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindow_Open_NotTFTPTarget(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	seedAdminDemoSSHTarget(t, app)

	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/demo/write-window", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-TFTP-server target, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindow_Close_ServerNotRunning(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodDelete, "/api/tftp/targets/"+targetID+"/write-window", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

// End-to-end happy path through the real (but ephemeral-port, temp-dir)
// embedded TFTP server singleton: verifies the open response's client_ip
// is derived from the target's stored Host (not caller-supplied), and
// that GET status correctly reflects closed -> open -> closed across the
// whole open/close cycle -- this is what the frontend now polls on page
// load instead of relying on optimistic local-only state.
func TestTFTPWriteWindow_OpenGetClose_StatusReflectsReality(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	cookie := &http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"}

	getStatus := func() tftpWriteWindowStatusResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/write-window", nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Result().StatusCode != http.StatusOK {
			t.Fatalf("GET expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
		}
		var resp tftpWriteWindowStatusResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode GET response: %v body=%s", err, w.Body.String())
		}
		return resp
	}

	oldListen := os.Getenv("VANTYX_TFTP_LISTEN")
	if err := os.Setenv("VANTYX_TFTP_LISTEN", "127.0.0.1:0"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		if oldListen != "" {
			_ = os.Setenv("VANTYX_TFTP_LISTEN", oldListen)
		} else {
			_ = os.Unsetenv("VANTYX_TFTP_LISTEN")
		}
	})
	tftp.NotifyTargetCreated(context.Background(), app.TargetStore, access.ProtocolTFTP)
	t.Cleanup(func() { tftp.NotifyTargetDeleted(access.ProtocolTFTP) })

	if s := getStatus(); s.Open {
		t.Fatalf("expected closed before opening, got %+v", s)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+targetID+"/write-window", bytes.NewReader([]byte(`{"ttl_seconds":60}`)))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var openResp tftpWriteWindowStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &openResp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if !openResp.Open {
		t.Fatalf("expected open=true in the POST response, got %+v", openResp)
	}
	// setupAppWithTFTPServerTarget creates the target with Host "127.0.0.1".
	if openResp.ClientIP != "127.0.0.1" {
		t.Fatalf("expected client_ip derived from target.Host (127.0.0.1), got %q", openResp.ClientIP)
	}
	if openResp.TargetID != targetID {
		t.Fatalf("expected target_id %q, got %q", targetID, openResp.TargetID)
	}
	if openResp.ExpiresAt == "" {
		t.Fatal("expected a non-empty expires_at")
	}

	if s := getStatus(); !s.Open || s.ClientIP != "127.0.0.1" || s.ExpiresAt != openResp.ExpiresAt {
		t.Fatalf("expected GET to reflect the just-opened window, got %+v (open response was %+v)", s, openResp)
	}

	closeReq := httptest.NewRequest(http.MethodDelete, "/api/tftp/targets/"+targetID+"/write-window", nil)
	closeReq.AddCookie(cookie)
	closeW := httptest.NewRecorder()
	router.ServeHTTP(closeW, closeReq)
	if closeW.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", closeW.Result().StatusCode, closeW.Body.String())
	}

	if s := getStatus(); s.Open {
		t.Fatalf("expected closed after DELETE, got %+v", s)
	}
}

func TestTFTPWriteWindowEvents_NoBroker(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	app.TFTPWriteWindowEvents = nil
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/write-window/events", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindowEvents_Unauthenticated(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/write-window/events", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestTFTPWriteWindowEvents_NotTFTPTarget(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	seedAdminDemoSSHTarget(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/demo/write-window/events", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-TFTP-server target, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

// Connecting sends the current status immediately, even with the embedded
// server not running (open:false), so the client never has to wait for a
// change before it knows where things stand.
func TestTFTPWriteWindowEvents_SendsInitialSnapshot(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/write-window/events", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
	body := w.Body.String()
	if !strings.Contains(body, `"open":false`) || !strings.Contains(body, `"target_id":"`+targetID+`"`) {
		t.Fatalf("expected initial closed snapshot in body, got: %s", body)
	}
}

// End-to-end: a client connected to the SSE stream sees the window open
// (via a separate POST) and close (via a separate DELETE) live, without
// needing to reconnect or poll -- this is the "即時反映" behavior.
func TestTFTPWriteWindowEvents_LiveUpdateOnOpenAndClose(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	cookie := &http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"}

	oldListen := os.Getenv("VANTYX_TFTP_LISTEN")
	if err := os.Setenv("VANTYX_TFTP_LISTEN", "127.0.0.1:0"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		if oldListen != "" {
			_ = os.Setenv("VANTYX_TFTP_LISTEN", oldListen)
		} else {
			_ = os.Unsetenv("VANTYX_TFTP_LISTEN")
		}
	})
	tftp.NotifyTargetCreated(context.Background(), app.TargetStore, access.ProtocolTFTP)
	t.Cleanup(func() { tftp.NotifyTargetDeleted(access.ProtocolTFTP) })

	ctx, cancel := context.WithCancel(context.Background())
	sseReq := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/write-window/events", nil).WithContext(ctx)
	sseReq.AddCookie(cookie)
	sseW := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(sseW, sseReq)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond) // let the initial open:false snapshot land

	openReq := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+targetID+"/write-window", bytes.NewReader([]byte(`{"ttl_seconds":60}`)))
	openReq.AddCookie(cookie)
	openW := httptest.NewRecorder()
	router.ServeHTTP(openW, openReq)
	if openW.Result().StatusCode != http.StatusOK {
		t.Fatalf("open: expected 200, got %d body=%s", openW.Result().StatusCode, openW.Body.String())
	}

	time.Sleep(100 * time.Millisecond)

	closeReq := httptest.NewRequest(http.MethodDelete, "/api/tftp/targets/"+targetID+"/write-window", nil)
	closeReq.AddCookie(cookie)
	closeW := httptest.NewRecorder()
	router.ServeHTTP(closeW, closeReq)
	if closeW.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("close: expected 204, got %d", closeW.Result().StatusCode)
	}

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not exit")
	}

	body := sseW.Body.String()
	if !strings.Contains(body, `"open":true`) {
		t.Fatalf("expected an open:true frame after POST, body=%q", body)
	}
	if !strings.Contains(body, `"client_ip":"127.0.0.1"`) {
		t.Fatalf("expected client_ip in the open frame, body=%q", body)
	}
	if strings.Count(body, `"open":false`) < 2 {
		t.Fatalf("expected at least two open:false frames (initial + after DELETE), body=%q", body)
	}
}
