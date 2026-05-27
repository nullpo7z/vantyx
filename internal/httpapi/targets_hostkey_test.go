package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/mock"
)

// adminCookieFor returns a session cookie tied to the bootstrap admin
// user. The returned cookie can be added to httptest.NewRequest
// requests to satisfy requireAdmin / currentUserID helpers.
func adminCookieFor(t *testing.T, app *App) *http.Cookie {
	t.Helper()
	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("SessionStore.Create: %v", err)
	}
	return &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}
}

// nonAdminCookieFor creates a fresh non-admin user with a session.
// It returns the session cookie. Used to exercise the 403 admin
// branch on host-key endpoints.
func nonAdminCookieFor(t *testing.T, app *App, username string) *http.Cookie {
	t.Helper()
	if app == nil || app.UserStore == nil {
		t.Fatal("app missing UserStore")
	}
	if _, err := app.UserStore.CreateUser(username, username, "Passw0rd!", "user"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = app.UserStore.SetForcePasswordChange(username, false)
	sess, err := app.SessionStore.Create(username)
	if err != nil {
		t.Fatalf("SessionStore.Create: %v", err)
	}
	return &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}
}

// seedTarget creates an SSH target that the supplied user has access
// to. Returns its ID. The function commits it through the access
// store directly so it does not depend on the create endpoint we are
// testing.
func seedTarget(t *testing.T, app *App, id string, host string, port uint16) {
	t.Helper()
	ctx := context.Background()
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default"); err != nil {
		// "already exists" is fine for tests that seed multiple targets.
		_ = err
	}
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	if _, err := app.TargetStore.CreateWithPath(ctx, access.TargetID(id), id, host, port, access.ProtocolSSH, access.GroupID("default"), "default", "", "", "", "", true, false, false); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("default"), access.TargetID(id))
}

// --- POST /api/targets/probe-host-key ---

func TestProbeHostKey_Unauthorized_NoCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	body := []byte(`{"host":"127.0.0.1","port":22}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets/probe-host-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestProbeHostKey_Forbidden_NonAdmin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := nonAdminCookieFor(t, app, "u-probe")

	body := []byte(`{"host":"127.0.0.1","port":22}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets/probe-host-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestProbeHostKey_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/targets/probe-host-key", nil)
	app.handleProbeHostKey(w, r)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Result().StatusCode)
	}
}

func TestProbeHostKey_BadJSON(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)

	req := httptest.NewRequest(http.MethodPost, "/api/targets/probe-host-key", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestProbeHostKey_EmptyHost(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)

	req := httptest.NewRequest(http.MethodPost, "/api/targets/probe-host-key", bytes.NewReader([]byte(`{"host":"  ","port":22}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestProbeHostKey_DialFailure_BadGateway(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)

	// 127.0.0.1:1 is unlikely to be listening; the dial will fail
	// quickly with "connection refused".
	body := []byte(`{"host":"127.0.0.1","port":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets/probe-host-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestProbeHostKey_Success_ReturnsFingerprint(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)

	srv, err := mock.NewSSHEchoServer("u", "p")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	body := []byte(fmt.Sprintf(`{"host":%q,"port":%d}`, "127.0.0.1", srv.Port()))
	req := httptest.NewRequest(http.MethodPost, "/api/targets/probe-host-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var resp probeHostKeyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.Fingerprint, "SHA256:") {
		t.Fatalf("expected SHA256: prefix, got %q", resp.Fingerprint)
	}
	if resp.Host != "127.0.0.1" || resp.Port != srv.Port() {
		t.Fatalf("unexpected echo of host/port: %+v", resp)
	}
}

// --- PUT /api/targets/{id}/ssh-host-key ---

func TestUpdateTargetHostKey_Unauthorized_NoCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	seedTarget(t, app, "t1", "10.0.0.1", 22)

	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1/ssh-host-key", bytes.NewReader([]byte(`{"fingerprint":""}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestUpdateTargetHostKey_Forbidden_NonAdmin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := nonAdminCookieFor(t, app, "u-update")
	seedTarget(t, app, "t1", "10.0.0.1", 22)

	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1/ssh-host-key", bytes.NewReader([]byte(`{"fingerprint":""}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestUpdateTargetHostKey_NotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)

	req := httptest.NewRequest(http.MethodPut, "/api/targets/does-not-exist/ssh-host-key", bytes.NewReader([]byte(`{"fingerprint":""}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestUpdateTargetHostKey_BadFormat(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)
	seedTarget(t, app, "t1", "10.0.0.1", 22)

	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1/ssh-host-key", bytes.NewReader([]byte(`{"fingerprint":"not-a-fingerprint"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestUpdateTargetHostKey_BadJSON(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)
	seedTarget(t, app, "t1", "10.0.0.1", 22)

	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1/ssh-host-key", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestUpdateTargetHostKey_AdoptAndClear(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)
	seedTarget(t, app, "t1", "10.0.0.1", 22)

	const fp = "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	body := []byte(fmt.Sprintf(`{"fingerprint":%q}`, fp))
	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1/ssh-host-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("adopt expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var resp targetResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode adopt: %v", err)
	}
	if resp.SSHHostKeyFingerprint != fp {
		t.Fatalf("expected fingerprint %q in response, got %q", fp, resp.SSHHostKeyFingerprint)
	}

	clear := httptest.NewRequest(http.MethodPut, "/api/targets/t1/ssh-host-key", bytes.NewReader([]byte(`{"fingerprint":""}`)))
	clear.Header.Set("Content-Type", "application/json")
	clear.AddCookie(cookie)
	cw := httptest.NewRecorder()
	router.ServeHTTP(cw, clear)
	if cw.Result().StatusCode != http.StatusOK {
		t.Fatalf("clear expected 200, got %d body=%s", cw.Result().StatusCode, cw.Body.String())
	}
	var clearResp targetResponse
	if err := json.NewDecoder(cw.Body).Decode(&clearResp); err != nil {
		t.Fatalf("decode clear: %v", err)
	}
	if clearResp.SSHHostKeyFingerprint != "" {
		t.Fatalf("expected empty fingerprint after clear, got %q", clearResp.SSHHostKeyFingerprint)
	}
}

func TestUpdateTargetHostKey_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/targets/t1/ssh-host-key", nil)
	app.handleUpdateTargetHostKey(w, r)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Result().StatusCode)
	}
}

// --- create-target with embedded fingerprint ---

func TestCreateTarget_AcceptsFingerprint(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	cookie := adminCookieFor(t, app)
	ctx := context.Background()
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default"); err != nil {
		_ = err
	}
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))

	const fp = "SHA256:0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefg"
	body := []byte(fmt.Sprintf(`{"name":"with-fp","host":"10.0.0.2","port":22,"protocol":"ssh","group_id":"default","ssh_host_key_fingerprint":%q}`, fp))
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var resp targetResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SSHHostKeyFingerprint != fp {
		t.Fatalf("expected fingerprint to round-trip; got %q", resp.SSHHostKeyFingerprint)
	}
}
