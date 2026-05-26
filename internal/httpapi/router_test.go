package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "httpapi.db")
	if err := os.Setenv("VANTYX_SQLITE_PATH", dbPath); err != nil {
		t.Fatalf("set env: %v", err)
	}
	app := NewApp()
	t.Cleanup(func() {
		_ = closeAuditSink()
		// Give in-flight goroutines a moment to release SQLite handles
		// before TempDir cleanup (avoids flaky -wal/-shm leftovers).
		time.Sleep(50 * time.Millisecond)
		if app != nil && app.DB != nil {
			_, _ = app.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			_ = app.DB.Close()
		}
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
		_ = os.Unsetenv("VANTYX_SQLITE_PATH")
	})
	return app
}

func TestApp_LoginSuccess(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	body := []byte(`{"username":"admin","password":"Admin123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestApp_LoginFailure(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	body := []byte(`{"username":"admin","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.StatusCode)
	}
}

func TestApp_Login_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_ListUsers_Forbidden(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("u1", "user1", "User123!", auth.RoleUser)
	sess, _ := app.SessionStore.Create("u1")
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", w.Result().StatusCode)
	}
}

func TestApp_Me_UnauthorizedWithoutCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.StatusCode)
	}
}

func TestApp_Me_SuccessWithValidSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	// Seeded admin user has ID "admin".
	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{
		Name:  "vantyx_session",
		Value: sess.ID,
		Path:  "/",
	})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestApp_Targets_UnauthorizedWithoutCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.StatusCode)
	}
}

func TestApp_Targets_UnauthorizedInvalidSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "bad-session", Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_Targets_SuccessWithValidSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	req.AddCookie(&http.Cookie{
		Name:  "vantyx_session",
		Value: sess.ID,
		Path:  "/",
	})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	var targets []targetResponse
	if err := json.NewDecoder(res.Body).Decode(&targets); err != nil {
		t.Fatalf("decode targets: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("expected 0 targets by default, got %d", len(targets))
	}
}

func TestApp_Targets_WithTargets(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "Host1", "192.168.1.1", 22, access.ProtocolSSH, access.GroupID("default"), "default", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("default"), access.TargetID("t1"))

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var targets []targetResponse
	if err := json.NewDecoder(res.Body).Decode(&targets); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "t1" {
		t.Fatalf("expected one target t1, got %+v", targets)
	}
}

func TestTargetToResponse_BoolFlags(t *testing.T) {
	tgt := &access.Target{
		ID:          access.TargetID("t1"),
		Name:        "name",
		Host:        "host",
		Port:        22,
		Protocol:    access.ProtocolSSH,
		Path:        "path",
		SFTPEnabled: false,
		FTPEnabled:  true,
		TFTPEnabled: true,
	}
	resp := targetToResponse(tgt, nil)
	if resp.ID != "t1" || resp.Name != "name" || resp.Host != "host" || resp.Port != 22 || resp.Protocol != "ssh" || resp.Path != "path" {
		t.Fatalf("unexpected basic mapping: %+v", resp)
	}
	if resp.SFTPEnabled != false || !resp.FTPEnabled || !resp.TFTPEnabled {
		t.Fatalf("unexpected protocol flags: sftp=%v ftp=%v tftp=%v", resp.SFTPEnabled, resp.FTPEnabled, resp.TFTPEnabled)
	}
}

func TestApp_CreateTarget_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	// create default group and add admin
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	body := []byte(`{"name":"My Server","host":"192.168.1.1","port":22,"protocol":"ssh","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var created targetResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "My Server" || created.Host != "192.168.1.1" || created.Port != 22 || created.Protocol != "ssh" {
		t.Fatalf("unexpected created target: %+v", created)
	}
	if created.Path != "default" {
		t.Fatalf("expected path to default group_id, got %q", created.Path)
	}
	ids, err := app.AccessGroupStore.TargetIDsForUser(ctx, access.UserID("admin"), nil)
	if err != nil {
		t.Fatalf("TargetIDsForUser: %v", err)
	}
	found := false
	for _, id := range ids {
		if id == access.TargetID(created.ID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("new target %s not in admin's target list", created.ID)
	}
}

func TestApp_CreateTarget_WithPath(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("prod/network"), "Prod Network") // path "prod/network" used as group_id in target row
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	body := []byte(`{"name":"Edge1","host":"10.0.0.1","port":22,"protocol":"ssh","path":"prod/network","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var created targetResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Path != "prod/network" {
		t.Fatalf("expected path prod/network, got %q", created.Path)
	}
}

func TestApp_Groups_List(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var groups []groupResponse
	if err := json.NewDecoder(res.Body).Decode(&groups); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(groups) != 1 || groups[0].ID != "default" {
		t.Fatalf("unexpected groups: %+v", groups)
	}
}

func TestApp_Groups_UnauthorizedNoCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_Groups_UnauthorizedInvalidSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "bad-session", Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestAPISpec_UnauthorizedAndAdmin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/spec", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("without cookie expected 401, got %d", w.Result().StatusCode)
	}

	sess, _ := app.SessionStore.Create("admin")
	req2 := httptest.NewRequest(http.MethodGet, "/api/spec", nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	res := w2.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("admin expected 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/x-yaml" {
		t.Fatalf("expected application/x-yaml, got %s", ct)
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) == 0 {
		t.Fatal("expected non-empty spec body")
	}
	_ = res.Body.Close()
}

func TestAPISpec_ForbiddenNonAdmin(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("user1", "user1", "Pass123!", "")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("user1"), access.GroupID("g1"))
	sess, _ := app.SessionStore.Create("user1")

	req := httptest.NewRequest(http.MethodGet, "/api/spec", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin expected 403, got %d", w.Result().StatusCode)
	}
}

func TestDocs_UnauthorizedAndAdmin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("without cookie expected 401, got %d", w.Result().StatusCode)
	}

	sess, _ := app.SessionStore.Create("admin")
	req2 := httptest.NewRequest(http.MethodGet, "/docs", nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	res := w2.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("admin expected 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
	if !bytes.Contains(w2.Body.Bytes(), []byte("swagger-ui")) {
		t.Fatal("expected swagger-ui in docs body")
	}
}

func TestUsers_AdminOnly(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("u1", "user1", "Pass123!", "")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("u1"), access.GroupID("g1"))
	adminSess, _ := app.SessionStore.Create("admin")
	userSess, _ := app.SessionStore.Create("u1")

	// Non-admin: 403 on GET /api/users
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: userSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin GET /api/users expected 403, got %d", w.Result().StatusCode)
	}

	// Admin: 200 and list includes admin + u1
	req2 := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("admin GET /api/users expected 200, got %d body=%s", w2.Result().StatusCode, w2.Body.Bytes())
	}
	var users []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(users) < 2 {
		t.Fatalf("expected at least admin and u1, got %d", len(users))
	}

	// Admin: POST /api/users creates user
	body := []byte(`{"username":"newuser","password":"NewPass1!"}`)
	req3 := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req3.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Result().StatusCode != http.StatusCreated {
		t.Fatalf("admin POST /api/users expected 201, got %d body=%s", w3.Result().StatusCode, w3.Body.Bytes())
	}
	var created struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(w3.Body).Decode(&created); err != nil {
		t.Fatalf("decode created user: %v", err)
	}
	if created.Username != "newuser" {
		t.Fatalf("expected username newuser, got %s", created.Username)
	}
}

func TestGroupMembers_AdminOnly(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("u1", "user1", "Pass123!", "")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("u1"), access.GroupID("g1"))
	adminSess, _ := app.SessionStore.Create("admin")
	userSess, _ := app.SessionStore.Create("u1")

	// Non-admin: 403 on GET /api/groups/g1/members
	req := httptest.NewRequest(http.MethodGet, "/api/groups/g1/members", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: userSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin GET group members expected 403, got %d", w.Result().StatusCode)
	}

	// Admin: 200 and members include u1
	req2 := httptest.NewRequest(http.MethodGet, "/api/groups/g1/members", nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("admin GET group members expected 200, got %d", w2.Result().StatusCode)
	}
	var members []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members) != 1 || members[0].ID != "u1" {
		t.Fatalf("expected [u1], got %+v", members)
	}

	// Admin: POST add member (admin to g1)
	body := []byte(`{"user_id":"admin"}`)
	req3 := httptest.NewRequest(http.MethodPost, "/api/groups/g1/members", bytes.NewReader(body))
	req3.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	if w3.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("admin POST group member expected 204, got %d", w3.Result().StatusCode)
	}
	// Admin: DELETE remove member u1
	req4 := httptest.NewRequest(http.MethodDelete, "/api/groups/g1/members/u1", nil)
	req4.AddCookie(&http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"})
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, req4)
	if w4.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("admin DELETE group member expected 204, got %d", w4.Result().StatusCode)
	}
	members2, _ := app.AccessGroupStore.UserIDsForGroup(ctx, access.GroupID("g1"), nil)
	if len(members2) != 1 || string(members2[0]) != "admin" {
		t.Fatalf("after remove u1, expected [admin], got %v", members2)
	}
}

func TestListOptsFromRequest(t *testing.T) {
	tests := []struct {
		query     string
		wantNil   bool
		wantLimit int
		wantAfter string
	}{
		{"", true, 0, ""},
		{"limit=5", false, 5, ""},
		{"after_id=g1", false, 100, "g1"},
		{"limit=3&after_id=x", false, 3, "x"},
		{"limit=invalid", false, 0, ""},
		{"limit=0", false, 0, ""},
		{"limit=2000", false, 1000, ""},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/api/groups?"+tt.query, nil)
		opts := listOptsFromRequest(r)
		if tt.wantNil {
			if opts != nil {
				t.Errorf("query %q: expected nil opts", tt.query)
			}
			continue
		}
		if opts == nil {
			t.Errorf("query %q: expected non-nil opts", tt.query)
			continue
		}
		if opts.Limit != tt.wantLimit || opts.AfterID != tt.wantAfter {
			t.Errorf("query %q: got limit=%d after=%q, want limit=%d after=%q",
				tt.query, opts.Limit, opts.AfterID, tt.wantLimit, tt.wantAfter)
		}
	}
}

func TestAccessStoreConfigFromEnv(t *testing.T) {
	defer func() {
		_ = os.Unsetenv("VANTYX_ACCESS_QUERY_TIMEOUT")
		_ = os.Unsetenv("VANTYX_ACCESS_DEFAULT_LIST_LIMIT")
	}()

	cfg := accessStoreConfigFromEnv()
	if cfg != nil {
		t.Fatalf("no env: expected nil, got %+v", cfg)
	}

	os.Setenv("VANTYX_ACCESS_QUERY_TIMEOUT", "7s")
	cfg = accessStoreConfigFromEnv()
	if cfg == nil || cfg.QueryTimeout != 7*time.Second {
		t.Fatalf("with timeout: got %+v", cfg)
	}
	os.Unsetenv("VANTYX_ACCESS_QUERY_TIMEOUT")
	os.Setenv("VANTYX_ACCESS_DEFAULT_LIST_LIMIT", "50")
	cfg = accessStoreConfigFromEnv()
	if cfg == nil || cfg.DefaultListLimit != 50 {
		t.Fatalf("with limit: got %+v", cfg)
	}
	os.Setenv("VANTYX_ACCESS_QUERY_TIMEOUT", "invalid")
	os.Setenv("VANTYX_ACCESS_DEFAULT_LIST_LIMIT", "x")
	cfg = accessStoreConfigFromEnv()
	if cfg == nil {
		t.Fatal("expected non-nil cfg with invalid env (zero values)")
	}
	if cfg.QueryTimeout != 0 || cfg.DefaultListLimit != 0 {
		t.Fatalf("invalid env should yield zero values: %+v", cfg)
	}
}

func TestRequestLog_XForwardedFor(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Forwarded-For", " 192.168.1.1 , 10.0.0.1 ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	// Just ensure no panic; we can't assert log output easily.
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 (no cookie), got %d", w.Result().StatusCode)
	}
}

func TestRequestLog_XForwardedFor_FirstClient(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Forwarded-For", "client, proxy1, proxy2")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

// TestResponseWriter_Hijack_NotImplemented covers the path where the underlying ResponseWriter does not implement http.Hijacker (e.g. httptest.ResponseRecorder).
func TestResponseWriter_Hijack_NotImplemented(t *testing.T) {
	w := &responseWriter{ResponseWriter: httptest.NewRecorder()}
	conn, buf, err := w.Hijack()
	if conn != nil || buf != nil {
		t.Fatalf("expected nil conn and buf")
	}
	if err == nil || !strings.Contains(err.Error(), "does not implement") {
		t.Fatalf("expected error about Hijacker, got %v", err)
	}
}

func TestApp_Groups_PaginationResponse(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g2"))
	sess, _ := app.SessionStore.Create("admin")

	req := httptest.NewRequest(http.MethodGet, "/api/groups?limit=1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var out struct {
		Items      []groupResponse `json:"items"`
		NextCursor string          `json:"next_cursor"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.NextCursor == "" {
		t.Fatal("expected next_cursor when more items exist")
	}
}

func TestApp_Targets_PaginationResponse(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "H1", "10.0.0.1", 22, access.ProtocolSSH, access.GroupID("default"), "default", "", "", "", "", true, false, false)
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t2"), "H2", "10.0.0.2", 22, access.ProtocolSSH, access.GroupID("default"), "default", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("default"), access.TargetID("t1"))
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("default"), access.TargetID("t2"))
	sess, _ := app.SessionStore.Create("admin")

	req := httptest.NewRequest(http.MethodGet, "/api/targets?limit=1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var out struct {
		Items      []targetResponse `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.NextCursor == "" {
		t.Fatal("expected next_cursor when more targets exist")
	}
}

func TestApp_Groups_EmptyWhenNoGroups(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var groups []groupResponse
	if err := json.NewDecoder(res.Body).Decode(&groups); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected no groups, got %+v", groups)
	}
}

func TestApp_Groups_Create(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	body := []byte(`{"name":"Network","path":"prod"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var g groupResponse
	if err := json.NewDecoder(res.Body).Decode(&g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.ID != "prod/network" || g.Name != "Network" {
		t.Fatalf("unexpected group: %+v", g)
	}
}

// TestApp_CreateGroup_NonAdminForbidden checks that a non-admin user
// gets HTTP 403 when trying to create an access group (server-
// management operations are admin-only).
func TestApp_CreateGroup_NonAdminForbidden(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	if _, err := app.UserStore.CreateUser("regular", "regular", "Regular123!", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sess, err := app.SessionStore.Create("regular")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	body := []byte(`{"name":"MyGroup","path":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin create group, got %d (%s)", w.Result().StatusCode, w.Body.String())
	}
}

// TestApp_CreateTarget_NonAdminForbidden checks that a non-admin user
// gets HTTP 403 when trying to create a target.
func TestApp_CreateTarget_NonAdminForbidden(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	if _, err := app.UserStore.CreateUser("regular", "regular", "Regular123!", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1"); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("regular"), access.GroupID("g1")); err != nil {
		t.Fatalf("add user to group: %v", err)
	}
	sess, err := app.SessionStore.Create("regular")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	body := []byte(`{"name":"T1","host":"10.0.0.1","port":22,"protocol":"ssh","group_id":"g1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin create target, got %d (%s)", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_CreateGroup_EmptyName_BadRequest(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	body := []byte(`{"name":"   ","path":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", res.StatusCode)
	}
}

func TestApp_CreateTarget_InvalidProtocol_BadRequest(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	body := []byte(`{"name":"Bad","host":"10.0.0.2","port":22,"protocol":"http","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid protocol, got %d", res.StatusCode)
	}
}

func TestApp_Me_InvalidSessionCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "invalid-session-id", Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_Logout_InvalidatesSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	// Login to get a session
	loginBody := []byte(`{"username":"admin","password":"Admin123!"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	if loginW.Result().StatusCode != http.StatusOK {
		t.Fatalf("login expected 200, got %d", loginW.Result().StatusCode)
	}
	cookies := loginW.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "vantyx_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no vantyx_session cookie in login response")
	}

	// Logout
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutW := httptest.NewRecorder()
	router.ServeHTTP(logoutW, logoutReq)
	if logoutW.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("logout expected 204, got %d", logoutW.Result().StatusCode)
	}

	// Session must be invalid: /api/me with same cookie returns 401
	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(sessionCookie)
	meW := httptest.NewRecorder()
	router.ServeHTTP(meW, meReq)
	if meW.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout expected 401 from /api/me, got %d", meW.Result().StatusCode)
	}
}

func TestApp_ChangePassword(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	loginBody := []byte(`{"username":"admin","password":"Admin123!"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)
	if loginW.Result().StatusCode != http.StatusOK {
		t.Fatalf("login expected 200, got %d", loginW.Result().StatusCode)
	}
	var sessionCookie *http.Cookie
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "vantyx_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie")
	}

	changeBody := []byte(`{"current_password":"Admin123!","new_password":"NewAdmin1!"}`)
	changeReq := httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewReader(changeBody))
	changeReq.Header.Set("Content-Type", "application/json")
	changeReq.AddCookie(sessionCookie)
	changeW := httptest.NewRecorder()
	router.ServeHTTP(changeW, changeReq)
	if changeW.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("change password expected 204, got %d body=%s", changeW.Result().StatusCode, changeW.Body.String())
	}

	login2Body := []byte(`{"username":"admin","password":"NewAdmin1!"}`)
	login2Req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(login2Body))
	login2Req.Header.Set("Content-Type", "application/json")
	login2W := httptest.NewRecorder()
	router.ServeHTTP(login2W, login2Req)
	if login2W.Result().StatusCode != http.StatusOK {
		t.Fatalf("login with new password expected 200, got %d", login2W.Result().StatusCode)
	}
}

// TestApp_LoginRateLimit_Returns429 exercises clientIP (X-Forwarded-For), allow, and 429 when over limit.
func TestApp_LoginRateLimit_Returns429(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	// 5 failed logins from same IP (X-Forwarded-For with comma: clientIP uses first part)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`{"username":"admin","password":"wrong"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", " 10.0.0.1 , 192.168.1.1 ")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Result().StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Result().StatusCode)
		}
	}
	// 6th attempt from same IP -> 429
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`{"username":"admin","password":"wrong"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", " 10.0.0.1 , 192.168.1.1 ")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", w.Result().StatusCode)
	}
}

// TestApp_Login_ClientIP_FromRemoteAddr covers clientIP when X-Forwarded-For is absent.
func TestApp_Login_ClientIP_FromRemoteAddr(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`{"username":"admin","password":"wrong"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.2.1:45678"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

// TestApp_Login_ClientIP_NoPort covers clientIP when RemoteAddr has no colon (host empty, return RemoteAddr).
func TestApp_Login_ClientIP_NoPort(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`{"username":"admin","password":"wrong"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "localhost" // no port -> SplitHostPort returns host ""
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_ChangePassword_WrongCurrentPassword(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"current_password":"WrongPass1!","new_password":"NewAdmin1!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 (wrong current password), got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_ChangePassword_UnchangedPassword(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"current_password":"Admin123!","new_password":"Admin123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (password unchanged), got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_ChangePassword_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_ChangePassword_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	body := []byte(`{"current_password":"Admin123!","new_password":"NewAdmin1!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", w.Result().StatusCode)
	}
}

// TestApp_UpdateLocale_RoundTrip verifies that PUT /api/me/locale persists the
// user's locale and that subsequent login / GET /api/me reflect it.
func TestApp_UpdateLocale_RoundTrip(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	req := httptest.NewRequest(http.MethodPut, "/api/me/locale", bytes.NewReader([]byte(`{"locale":"ja"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/me/locale expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var resp struct {
		Locale string `json:"locale"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Locale != "ja" {
		t.Fatalf("expected locale=ja in response, got %q", resp.Locale)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(cookie)
	meW := httptest.NewRecorder()
	router.ServeHTTP(meW, meReq)
	if meW.Result().StatusCode != http.StatusOK {
		t.Fatalf("/api/me expected 200, got %d", meW.Result().StatusCode)
	}
	var me struct {
		Locale string `json:"locale"`
	}
	_ = json.Unmarshal(meW.Body.Bytes(), &me)
	if me.Locale != "ja" {
		t.Fatalf("expected /api/me locale=ja, got %q", me.Locale)
	}

	clearReq := httptest.NewRequest(http.MethodPut, "/api/me/locale", bytes.NewReader([]byte(`{"locale":""}`)))
	clearReq.Header.Set("Content-Type", "application/json")
	clearReq.AddCookie(cookie)
	clearW := httptest.NewRecorder()
	router.ServeHTTP(clearW, clearReq)
	if clearW.Result().StatusCode != http.StatusOK {
		t.Fatalf("clearing locale expected 200, got %d", clearW.Result().StatusCode)
	}
}

// TestApp_UpdateLocale_Validation rejects unsupported locale codes and
// unauthenticated callers.
func TestApp_UpdateLocale_Validation(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	req := httptest.NewRequest(http.MethodPut, "/api/me/locale", bytes.NewReader([]byte(`{"locale":"fr"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported locale expected 400, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}

	bad := httptest.NewRequest(http.MethodPut, "/api/me/locale", bytes.NewReader([]byte("not json")))
	bad.Header.Set("Content-Type", "application/json")
	bad.AddCookie(cookie)
	badW := httptest.NewRecorder()
	router.ServeHTTP(badW, bad)
	if badW.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid body expected 400, got %d", badW.Result().StatusCode)
	}

	unauthed := httptest.NewRequest(http.MethodPut, "/api/me/locale", bytes.NewReader([]byte(`{"locale":"ja"}`)))
	unauthed.Header.Set("Content-Type", "application/json")
	uw := httptest.NewRecorder()
	router.ServeHTTP(uw, unauthed)
	if uw.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated expected 401, got %d", uw.Result().StatusCode)
	}
}

const testSSHAuthorizedKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl user@host"

func TestApp_SSHKeys_List_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/me/ssh-keys", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_SSHKeys_List_Forbidden_NonAdmin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	// Create non-admin user via API (as admin)
	adminSess, _ := app.SessionStore.Create("admin")
	adminCookie := &http.Cookie{Name: "vantyx_session", Value: adminSess.ID, Path: "/"}
	createBody := []byte(`{"username":"u2","password":"Passw0rd!","role":"user"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(adminCookie)
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	if createW.Result().StatusCode != http.StatusOK && createW.Result().StatusCode != http.StatusCreated {
		t.Fatalf("create user expected 200/201, got %d body=%s", createW.Result().StatusCode, createW.Body.String())
	}
	// Request ssh-keys as non-admin
	u2Sess, _ := app.SessionStore.Create("u2")
	u2Cookie := &http.Cookie{Name: "vantyx_session", Value: u2Sess.ID, Path: "/"}
	req := httptest.NewRequest(http.MethodGet, "/api/me/ssh-keys", nil)
	req.AddCookie(u2Cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_SSHKeys_ListAddDelete(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	// List empty
	req := httptest.NewRequest(http.MethodGet, "/api/me/ssh-keys", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("list expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var list []struct {
		ID        int64  `json:"id"`
		KeyLine   string `json:"key_line"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}

	// Add key
	addBody := []byte(`{"authorized_key":"` + testSSHAuthorizedKey + `"}`)
	addReq := httptest.NewRequest(http.MethodPost, "/api/me/ssh-keys", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addReq.AddCookie(cookie)
	addW := httptest.NewRecorder()
	router.ServeHTTP(addW, addReq)
	if addW.Result().StatusCode != http.StatusCreated {
		t.Fatalf("add expected 201, got %d body=%s", addW.Result().StatusCode, addW.Body.String())
	}
	var added struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(addW.Body).Decode(&added); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if added.ID <= 0 {
		t.Fatalf("expected positive id, got %d", added.ID)
	}

	// List returns one
	req2 := httptest.NewRequest(http.MethodGet, "/api/me/ssh-keys", nil)
	req2.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("list after add expected 200, got %d", w2.Result().StatusCode)
	}
	if err := json.NewDecoder(w2.Body).Decode(&list); err != nil {
		t.Fatalf("decode list 2: %v", err)
	}
	if len(list) != 1 || list[0].KeyLine != testSSHAuthorizedKey {
		t.Fatalf("expected one key, got %+v", list)
	}

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete, "/api/me/ssh-keys/"+fmt.Sprintf("%d", added.ID), nil)
	delReq.AddCookie(cookie)
	delW := httptest.NewRecorder()
	router.ServeHTTP(delW, delReq)
	if delW.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("delete expected 204, got %d body=%s", delW.Result().StatusCode, delW.Body.String())
	}
}

func TestApp_SSHKeys_Add_InvalidKey(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"authorized_key":"not-a-valid-key"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid key, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_SSHKeys_Add_EmptyKey(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"authorized_key":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty key, got %d", w.Result().StatusCode)
	}
}

func TestApp_SSHKeys_Delete_InvalidKeyID(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/me/ssh-keys/abc", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid key_id, got %d", w.Result().StatusCode)
	}
}

func TestApp_SSHKeys_Delete_NotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/me/ssh-keys/99999", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing key, got %d", w.Result().StatusCode)
	}
}

// userStoreFailingAddPublicKey makes AddPublicKey return a generic error to cover writeInternalError path.
type userStoreFailingAddPublicKey struct {
	auth.UserStore
}

func (u *userStoreFailingAddPublicKey) AddPublicKey(userID, keyLine string) (int64, error) {
	return 0, errors.New("injected add public key error")
}

func TestApp_SSHKeys_Add_InternalError(t *testing.T) {
	app := newTestApp(t)
	app.UserStore = &userStoreFailingAddPublicKey{UserStore: app.UserStore}
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"authorized_key":"` + testSSHAuthorizedKey + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/me/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 on AddPublicKey error, got %d", w.Result().StatusCode)
	}
}

// setupAppWithTargetAndSFTPMock creates an app with one group, one target (SSH with creds), admin has access, and SFTPClientFactory returns a mock.
func setupAppWithTargetAndSFTPMock(t *testing.T) (*App, *MockSFTPClient, string) {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	os.Setenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	defer os.Unsetenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, err := app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "T1", "host", 22, access.ProtocolSSH, access.GroupID("g1"), "", "root", "pass", "", "", true, false, false)
	if err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	mock := NewMockSFTPClient()
	app.SFTPClientFactory = func(context.Context, *access.Target) (FileTransferClient, error) { return mock, nil }
	return app, mock, "t1"
}

func TestApp_Files_ListWithMock(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.AddDir("/", []MockFileInfo{
		{NameField: "a.txt", SizeField: 10, IsDirField: false, ModTimeField: time.Now()},
		{NameField: "dir", SizeField: 0, IsDirField: true, ModTimeField: time.Now()},
	})
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var list []struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		IsDir   bool   `json:"is_dir"`
		ModTime string `json:"mod_time"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %+v", list)
	}
}

func TestApp_Files_List_Unauthorized(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files?path=/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_DownloadWithMock(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.AddFile("/data.txt", []byte("hello world"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files/download?path=/data.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	if body := w.Body.String(); body != "hello world" {
		t.Fatalf("expected body hello world, got %q", body)
	}
}

func TestApp_Files_Download_DirectoryRejected(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.AddDir("/sub", nil)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files/download?path=/sub", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (cannot download directory), got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_UploadWithMock(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	_ = mp.WriteField("path", "/uploaded.txt")
	fw, _ := mp.CreateFormFile("file", "uploaded.txt")
	_, _ = fw.Write([]byte("upload content"))
	_ = mp.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/targets/"+targetID+"/files/upload", &buf)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_Files_DeleteWithMock(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.AddFile("/to-delete.txt", []byte("x"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/"+targetID+"/files?path=/to-delete.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_Delete_RootRejected(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/"+targetID+"/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (cannot delete root), got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_GetTargetAndSFTPClient_NoTargetID(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	// Path with empty target_id segment: /api/targets//files
	req := httptest.NewRequest(http.MethodGet, "/api/targets//files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (target_id required), got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_FactoryReturnsError(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	app.SFTPClientFactory = func(context.Context, *access.Target) (FileTransferClient, error) {
		return nil, errors.New("injected connect error")
	}
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_GetTarget_TargetNotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/nonexistent/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_GetTarget_Forbidden(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	os.Setenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	defer os.Unsetenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t2"), "T2", "host", 22, access.ProtocolSSH, access.GroupID("g2"), "", "root", "pass", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g2"), access.TargetID("t2"))
	app.SFTPClientFactory = func(context.Context, *access.Target) (FileTransferClient, error) { return NewMockSFTPClient(), nil }
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/t2/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 (admin not in g2), got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_GetTarget_NonSSH(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "T1", "host", 23, access.ProtocolTelnet, access.GroupID("g1"), "", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/t1/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (only SSH), got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_Files_GetTarget_NoStoredCredentials(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "T1", "host", 22, access.ProtocolSSH, access.GroupID("g1"), "", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/t1/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (stored credentials required), got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_List_ReadDirError(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.ReadDirErr = errors.New("readdir failed")
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_Download_OpenError(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.OpenErr = errors.New("open failed")
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files/download?path=/x.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_Upload_NoPath(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	fw, _ := mp.CreateFormFile("file", "x.txt")
	_, _ = fw.Write([]byte("x"))
	_ = mp.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/targets/"+targetID+"/files/upload", &buf)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (path required), got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_Upload_CreateError(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.CreateErr = errors.New("create failed")
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	_ = mp.WriteField("path", "/up.txt")
	fw, _ := mp.CreateFormFile("file", "up.txt")
	_, _ = fw.Write([]byte("data"))
	_ = mp.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/targets/"+targetID+"/files/upload", &buf)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_Delete_RemoveError(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	mock.AddFile("/f.txt", []byte("x"))
	mock.RemoveErr = errors.New("remove failed")
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/"+targetID+"/files?path=/f.txt", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Result().StatusCode)
	}
}

func TestApp_Files_List_MethodNotAllowed(t *testing.T) {
	app, _, targetID := setupAppWithTargetAndSFTPMock(t)
	if app == nil {
		return
	}
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPost, "/api/targets/"+targetID+"/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateGroup_UnauthorizedNoCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader([]byte(`{"name":"x","path":""}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateGroup_UnauthorizedInvalidSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader([]byte(`{"name":"x","path":""}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "bad", Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateGroup_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_Groups_GetSkipsMissing(t *testing.T) {
	ctx := context.Background()
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	// Create "ghost" group then delete it; with FK CASCADE user_groups row is removed too.
	// So we create g1 and ghost, add admin to both, then delete ghost so Get("ghost") is never called.
	// To cover "Get err != nil continue": we need GroupIDsForUser to return an id where Get fails.
	// Without raw insert we can't. So just ensure groups list returns 200 with one group.
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var list []groupResponse
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 || list[0].ID != "g1" {
		t.Fatalf("unexpected groups: %+v", list)
	}
}

// userStoreFailingCreateUser wraps UserStore and makes CreateUser return a non-ErrUserExists error.
type userStoreFailingCreateUser struct {
	auth.UserStore
}

func (u *userStoreFailingCreateUser) CreateUser(id, username, plainPassword, role string) (*auth.User, error) {
	return nil, errors.New("injected create user error")
}

// sessionStoreFailingCreate wraps SessionStore and makes Create return an error.
type sessionStoreFailingCreate struct {
	auth.SessionStore
}

func (s *sessionStoreFailingCreate) Create(userID string) (*auth.Session, error) {
	return nil, errors.New("injected session create error")
}

func TestApp_Login_SessionCreateFails(t *testing.T) {
	app := newTestApp(t)
	app.SessionStore = &sessionStoreFailingCreate{SessionStore: app.SessionStore}
	router := app.NewRouter()

	body := []byte(`{"username":"admin","password":"Admin123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

// userStoreFailingGetByID wraps UserStore and makes GetByID return an error.
type userStoreFailingGetByID struct {
	auth.UserStore
}

func (u *userStoreFailingGetByID) GetByID(id string) (*auth.User, error) {
	return nil, errors.New("injected getbyid error")
}

func TestApp_Me_GetByIDFails(t *testing.T) {
	app := newTestApp(t)
	app.UserStore = &userStoreFailingGetByID{UserStore: app.UserStore}
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

// accessGroupStoreFailingCreate wraps and makes Create return non-ErrGroupExists error.
type accessGroupStoreFailingCreate struct {
	access.AccessGroupStore
}

func (a *accessGroupStoreFailingCreate) Create(ctx context.Context, id access.GroupID, name string) (*access.AccessGroup, error) {
	return nil, errors.New("injected create error")
}

func TestApp_CreateGroup_StoreCreateFails(t *testing.T) {
	app := newTestApp(t)
	app.AccessGroupStore = &accessGroupStoreFailingCreate{AccessGroupStore: app.AccessGroupStore}
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"name":"NewGroup","path":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

// accessGroupStoreGetFailsForID wraps and makes Get return error for a specific ID.
type accessGroupStoreGetFailsForID struct {
	access.AccessGroupStore
	failID string
}

func (a *accessGroupStoreGetFailsForID) Get(ctx context.Context, id access.GroupID) (*access.AccessGroup, error) {
	if string(id) == a.failID {
		return nil, errors.New("injected get error")
	}
	return a.AccessGroupStore.Get(ctx, id)
}

func TestApp_Groups_GetFailsSkipsGroup(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g2"))
	app.AccessGroupStore = &accessGroupStoreGetFailsForID{AccessGroupStore: app.AccessGroupStore, failID: "g2"}

	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var list []groupResponse
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// g2 Get fails so we skip it; only g1 in list
	if len(list) != 1 || list[0].ID != "g1" {
		t.Fatalf("expected one group g1, got %+v", list)
	}
}

// accessGroupStoreFailingAddTargetToGroup wraps and makes AddTargetToGroup return error.
type accessGroupStoreFailingAddTargetToGroup struct {
	access.AccessGroupStore
}

func (a *accessGroupStoreFailingAddTargetToGroup) AddTargetToGroup(ctx context.Context, groupID access.GroupID, targetID access.TargetID) error {
	return errors.New("injected add target error")
}

// targetStoreFailingCreate wraps TargetStore and makes CreateWithPath return a non-ErrTargetExists error.
type targetStoreFailingCreate struct {
	access.TargetStore
}

func (t *targetStoreFailingCreate) CreateWithPath(ctx context.Context, id access.TargetID, name, host string, port uint16, protocol access.Protocol, groupID access.GroupID, path string, sshUsername, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string, sftpEnabled, ftpEnabled, tftpEnabled bool) (*access.Target, error) {
	return nil, errors.New("injected create target error")
}

func TestApp_CreateTarget_StoreCreateFails(t *testing.T) {
	app := newTestApp(t)
	app.TargetStore = &targetStoreFailingCreate{TargetStore: app.TargetStore}
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"Srv","host":"h","port":22,"protocol":"ssh","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_AddTargetToGroupFails(t *testing.T) {
	app := newTestApp(t)
	app.AccessGroupStore = &accessGroupStoreFailingAddTargetToGroup{AccessGroupStore: app.AccessGroupStore}
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"Srv","host":"h","port":22,"protocol":"ssh","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

func TestApp_NewApp_PanicOnInvalidDB(t *testing.T) {
	oldOpen := newAppDBOpen
	defer func() { newAppDBOpen = oldOpen }()
	newAppDBOpen = func(dbsqlite.Config) (*sql.DB, error) {
		return nil, errors.New("injected open error")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when DB open fails")
		}
	}()
	NewApp()
}

func TestApp_NewApp_PanicOnMigrateFailure(t *testing.T) {
	oldOpen := newAppDBOpen
	oldMigrate := newAppMigrate
	defer func() { newAppDBOpen = oldOpen; newAppMigrate = oldMigrate }()

	cfg := dbsqlite.Config{Path: filepath.Join(t.TempDir(), "m.db")}
	db, err := dbsqlite.Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	newAppDBOpen = func(dbsqlite.Config) (*sql.DB, error) { return db, nil }
	newAppMigrate = func(*sql.DB) error {
		db.Close()
		return errors.New("injected migrate error")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when migrate fails")
		}
	}()
	NewApp()
}

func TestApp_NewApp_PanicOnCreateUserFailure(t *testing.T) {
	oldUserStore := newAppUserStore
	defer func() { newAppUserStore = oldUserStore }()

	dbPath := filepath.Join(t.TempDir(), "cu.db")
	os.Setenv("VANTYX_SQLITE_PATH", dbPath)
	defer os.Unsetenv("VANTYX_SQLITE_PATH")

	newAppUserStore = func(db *sql.DB) auth.UserStore {
		return &userStoreFailingCreateUser{UserStore: auth.NewSQLiteUserStore(db)}
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when CreateUser fails")
		}
	}()
	NewApp()
}

func TestApp_CreateTarget_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	app.handleCreateTarget(w, r)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_UnauthorizedNoCookie(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte(`{"name":"x","host":"h","group_id":"g"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_UnauthorizedInvalidSession(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte(`{"name":"x","host":"h","group_id":"g"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: "bad", Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_EmptyName(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"","host":"h","port":22,"protocol":"ssh","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

// TestApp_CreateTarget_ServiceUnavailableWhenEncryptionKeyMissing triggers writeServiceUnavailableError (503) when SSH password is set but VANTYX_SSH_PASSWORD_ENCRYPTION_KEY is not set.
func TestApp_CreateTarget_ServiceUnavailableWhenEncryptionKeyMissing(t *testing.T) {
	oldVal := os.Getenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	os.Unsetenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	defer func() {
		if oldVal != "" {
			_ = os.Setenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY", oldVal)
		}
	}()

	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"Srv","host":"10.0.0.1","port":22,"protocol":"ssh","group_id":"default","ssh_username":"root","ssh_password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.StatusCode)
	}
	var out struct{ Message string }
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.Message != "service unavailable" {
		t.Fatalf("expected message 'service unavailable', got %q", out.Message)
	}
}

func TestApp_CreateTarget_EmptyGroupID(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"name":"srv","host":"h","port":22,"protocol":"ssh","group_id":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_GroupNotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"name":"srv","host":"h","port":22,"protocol":"ssh","group_id":"nonexistent"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_Forbidden(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("other"), "Other")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"srv","host":"h","port":22,"protocol":"ssh","group_id":"other"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateTarget_TelnetProtocol(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"TelnetHost","host":"10.0.0.3","port":23,"protocol":"telnet","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var created targetResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Protocol != "telnet" {
		t.Fatalf("expected protocol telnet, got %q", created.Protocol)
	}
}

func TestApp_CreateTarget_PortZeroDefaultsTo22(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"NoPort","host":"10.0.0.4","protocol":"ssh","group_id":"default"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var created targetResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Port != 22 {
		t.Fatalf("expected port 22, got %d", created.Port)
	}
}

func TestSlugID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Hello", "hello"},
		{"My Server", "my-server"},
		{"a-b_c", "a-b-c"},
		{"UPPER", "upper"},
		{"", "target"},
		{"123", "123"},
		{"  leading", "leading"},
		{"trailing-", "trailing"},
	}
	for _, tt := range tests {
		got := slugID(tt.in)
		if got != tt.want {
			t.Errorf("slugID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeTargetPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", ""},
		{"a", "a"},
		{"a/b", "a/b"},
		{"a/./b", "a/b"},
		{"a/../b", "a/b"}, // implementation drops ".." segments, does not resolve parent
		{"  a  ", "a"},
		{"/a/b/", "a/b"},
	}
	for _, tt := range tests {
		got := normalizeTargetPath(tt.in)
		if got != tt.want {
			t.Errorf("normalizeTargetPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestApp_Healthz(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_SPAFallback(t *testing.T) {
	dir := t.TempDir()
	// #nosec G306 -- 0644 intentional for test static file (SPA fallback)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa</html>"), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	old := staticDirForTest
	staticDirForTest = dir
	defer func() { staticDirForTest = old }()

	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/any/path", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", w2.Result().StatusCode)
	}
}

func TestStaticDir_DefaultPathWhenSet(t *testing.T) {
	// Cover staticDir() when staticDirForTest is "" and "web/dist" exists and is dir
	d := t.TempDir()
	distDir := filepath.Join(d, "web", "dist")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// #nosec G306 -- 0644 intentional for test static file
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("ok"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	origWd, _ := os.Getwd()
	if err := os.Chdir(d); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	old := staticDirForTest
	staticDirForTest = ""
	defer func() { staticDirForTest = old }()

	dir := staticDir()
	if dir != "web/dist" {
		t.Fatalf("staticDir() = %q, want web/dist", dir)
	}
}

func TestApp_CreateGroup_DuplicateIDRetries(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("ops"), "Ops") // first group gets "ops"
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"ops","path":""}`) // same slug "ops" -> Create fails, retry "ops-1"
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var g groupResponse
	if err := json.NewDecoder(res.Body).Decode(&g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.ID != "ops-1" && g.ID != "ops" {
		t.Fatalf("expected ops-1 or ops, got %q", g.ID)
	}
}

func TestApp_CreateGroup_RetryUntilSuccess(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("ops"), "Ops")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("ops-1"), "Ops")
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"ops","path":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var g groupResponse
	if err := json.NewDecoder(res.Body).Decode(&g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.ID != "ops-2" {
		t.Fatalf("expected ops-2 after ops and ops-1 exist, got %q", g.ID)
	}
}

func TestApp_CreateTarget_DuplicateIDRetries(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("default"), "Default")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("default"))
	sess, _ := app.SessionStore.Create("admin")

	body := []byte(`{"name":"Router","host":"10.0.0.1","port":22,"protocol":"ssh","group_id":"default"}`)
	req1 := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Result().StatusCode != http.StatusCreated {
		t.Fatalf("first create: %d", w1.Result().StatusCode)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/targets", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	res := w2.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("second create: %d", res.StatusCode)
	}
	var created targetResponse
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID != "router-1" {
		t.Fatalf("expected router-1, got %q", created.ID)
	}
}

func TestApp_Groups_WithTargets(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "Host1", "192.168.1.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var list []groupResponse
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 || len(list[0].Targets) != 1 || list[0].Targets[0].ID != "t1" {
		t.Fatalf("unexpected groups: %+v", list)
	}
}

func TestApp_Groups_TwoGroupsWithTargets(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g2"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "H1", "1.1.1.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t2"), "H2", "2.2.2.2", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t3"), "H3", "3.3.3.3", 22, access.ProtocolSSH, access.GroupID("g2"), "g2", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t2"))
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g2"), access.TargetID("t3"))

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var list []groupResponse
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(list))
	}
}

func TestApp_UpdateTarget_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "Old", "10.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"name":"Updated","host":"10.0.0.2","port":2222,"protocol":"ssh","path":"","ssh_username":"","ssh_password":"","sftp_enabled":false,"ftp_enabled":true,"tftp_enabled":true}`)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var out targetResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Name != "Updated" || out.Host != "10.0.0.2" || out.Port != 2222 {
		t.Fatalf("unexpected response: %+v", out)
	}
	if out.SFTPEnabled != false || !out.FTPEnabled || !out.TFTPEnabled {
		t.Fatalf("unexpected protocol flags after update: sftp=%v ftp=%v tftp=%v", out.SFTPEnabled, out.FTPEnabled, out.TFTPEnabled)
	}
}

func TestApp_UpdateTarget_Forbidden(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t2"), "H2", "2.2.2.2", 22, access.ProtocolSSH, access.GroupID("g2"), "g2", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g2"), access.TargetID("t2"))

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"name":"X","host":"1.1.1.1","port":22,"protocol":"ssh","path":"","ssh_username":"","ssh_password":""}`)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/t2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Result().StatusCode)
	}
}

func TestApp_UpdateTarget_ServiceUnavailableWhenEncryptionKeyMissing(t *testing.T) {
	oldVal := os.Getenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	os.Unsetenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	defer func() {
		if oldVal != "" {
			_ = os.Setenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY", oldVal)
		}
	}()

	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "H1", "10.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))

	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"name":"H1","host":"10.0.0.1","port":22,"protocol":"ssh","path":"g1","ssh_username":"root","ssh_password":"secret"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", res.StatusCode)
	}
	var out struct{ Message string }
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.Message != "service unavailable" {
		t.Fatalf("expected message 'service unavailable', got %q", out.Message)
	}
}

func TestApp_DeleteTarget_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "H1", "10.0.0.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/t1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Result().StatusCode)
	}
	if _, err := app.TargetStore.Get(ctx, access.TargetID("t1")); err != access.ErrTargetNotFound {
		t.Fatalf("target should be deleted, got %v", err)
	}
}

func TestApp_DeleteTarget_Forbidden(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t2"), "H2", "2.2.2.2", 22, access.ProtocolSSH, access.GroupID("g2"), "g2", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g2"), access.TargetID("t2"))

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/t2", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Result().StatusCode)
	}
}

func TestApp_UserTags_Admin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/users/admin/tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_SetUserTags_Admin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	body := []byte(`{"tags":["prod","dev"]}`)
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPut, "/api/users/admin/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_GroupTags_Admin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/groups/g1/tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetGroupTags_Admin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	body := []byte(`{"tags":["a","b"]}`)
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPut, "/api/groups/g1/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_ListTags_Authenticated(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_ListTags_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_GroupTags_GroupIDRequired(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/groups//tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_GroupTags_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.AccessGroupStore.Create(context.Background(), access.GroupID("g1"), "G1")
	req := httptest.NewRequest(http.MethodGet, "/api/groups/g1/tags", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_GroupTags_Forbidden(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g2"), "G2")
	_, _ = app.UserStore.CreateUser("u1", "user1", "User123!", auth.RoleUser)
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("u1"), access.GroupID("g1"))
	sess, _ := app.SessionStore.Create("u1")
	req := httptest.NewRequest(http.MethodGet, "/api/groups/g2/tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetGroupTags_GroupIDRequired(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPut, "/api/groups//tags", bytes.NewReader([]byte(`{"tags":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetGroupTags_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.AccessGroupStore.Create(context.Background(), access.GroupID("g1"), "G1")
	req := httptest.NewRequest(http.MethodPut, "/api/groups/g1/tags", bytes.NewReader([]byte(`{"tags":["x"]}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetGroupTags_NotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPut, "/api/groups/nonexistent/tags", bytes.NewReader([]byte(`{"tags":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetGroupTags_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.AccessGroupStore.Create(context.Background(), access.GroupID("g1"), "G1")
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPut, "/api/groups/g1/tags", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_ListFiles_RelativePath(t *testing.T) {
	app, mock, targetID := setupAppWithTargetAndSFTPMock(t)
	mock.AddDir("/", []MockFileInfo{{NameField: "sub", SizeField: 0, IsDirField: true, ModTimeField: time.Now()}})
	mock.AddDir("/sub", nil)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/"+targetID+"/files?path=sub", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for path=sub (relative), got %d %s", w.Result().StatusCode, w.Body.String())
	}
}

func TestApp_AddGroupMember_UserNotFound(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"user_id":"nonexistent"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups/g1/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 (user not found), got %d", w.Result().StatusCode)
	}
}

func TestApp_AddGroupMember_GroupNotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"user_id":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups/nonexistent/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 (group not found), got %d", w.Result().StatusCode)
	}
}

func TestApp_AddGroupMember_EmptyUserID(t *testing.T) {
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	body := []byte(`{"user_id":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups/g1/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_RemoveGroupMember_EmptyIDs(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodDelete, "/api/groups//members/u1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (group_id required), got %d", w.Result().StatusCode)
	}
}

func TestApp_GetRecordingFile_RecordingIDRequired(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings//file", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_GetRecordingFile_Unauthorized(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/some-id/file", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
}

func TestApp_TargetTags_Admin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "H1", "1.1.1.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/targets/t1/tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetTargetTags_Admin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.TargetStore.CreateWithPath(ctx, access.TargetID("t1"), "H1", "1.1.1.1", 22, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "", true, false, false)
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	body := []byte(`{"tags":["web"]}`)
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodPut, "/api/targets/t1/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_ListRecordings_Authenticated(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_GetRecordingFile_NotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/nonexistent-id/file", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestApp_GetRecordingFile_RecordingsDirUnset(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, err := app.DB.ExecContext(ctx, `INSERT INTO recordings (id, user_id, target_id, session_id, channel_type, started_at, file_path) VALUES ('rec1', 'admin', 't1', 's1', 'ssh', datetime('now'), '/tmp/rec1.cast')`)
	if err != nil {
		t.Skipf("recordings table or schema: %v", err)
	}
	orig := os.Getenv("VANTYX_RECORDINGS_DIR")
	os.Unsetenv("VANTYX_RECORDINGS_DIR")
	defer func() { _ = os.Setenv("VANTYX_RECORDINGS_DIR", orig) }()

	sess, _ := app.SessionStore.Create("admin")
	req := httptest.NewRequest(http.MethodGet, "/api/recordings/rec1/file", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when RECORDINGS_DIR unset, got %d", w.Result().StatusCode)
	}
}

// --- isEncryptedPEMBlock ---

func TestIsEncryptedPEMBlock(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"unencrypted RSA", "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----", false},
		{"encrypted traditional", "-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC\n...", true},
		{"openssh bcrypt", "-----BEGIN OPENSSH PRIVATE KEY-----\nbcrypt...", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEncryptedPEMBlock(tt.in); got != tt.want {
				t.Errorf("isEncryptedPEMBlock(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// --- SSH Key Handlers ---

func adminSession(t *testing.T, app *App) string {
	t.Helper()
	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sess.ID
}

func TestApp_ListUserSSHKeys_Empty(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodGet, "/api/users/admin/ssh-keys", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_AddUserSSHKey_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"authorized_key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/admin/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(w.Body)
		t.Fatalf("expected 201, got %d: %s", w.Result().StatusCode, string(b))
	}
}

func TestApp_AddUserSSHKey_EmptyKey(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"authorized_key":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/admin/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_AddUserSSHKey_InvalidKey(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"authorized_key":"not-a-real-key"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/admin/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_AddUserSSHKey_UserNotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"authorized_key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/nonexistent/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestApp_AddUserSSHKey_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodPost, "/api/users/admin/ssh-keys", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_DeleteUserSSHKey_InvalidKeyID(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodDelete, "/api/users/admin/ssh-keys/abc", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_DeleteUserSSHKey_NotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodDelete, "/api/users/admin/ssh-keys/9999", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestApp_DeleteUserSSHKey_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"authorized_key":"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/admin/ssh-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Result().StatusCode)
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	req2 := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/users/admin/ssh-keys/%d", resp.ID), nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w2.Result().StatusCode)
	}
}

// --- CreateUser additional coverage ---

func TestApp_CreateUser_MissingUsername(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"username":"","password":"Pass123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateUser_MissingPassword(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"username":"newuser","password":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateUser_Duplicate(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"username":"newuser","password":"Pass123!","role":"user"}`)
	req1 := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Result().StatusCode != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", w1.Result().StatusCode)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create: expected 409, got %d", w2.Result().StatusCode)
	}
}

func TestApp_CreateUser_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_CreateUser_AdminRole(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	body := []byte(`{"username":"admin2","password":"Admin123!","role":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Result().StatusCode)
	}
	var resp struct {
		Role string `json:"role"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Role != "admin" {
		t.Fatalf("expected role admin, got %s", resp.Role)
	}
}

// --- User Tags ---

func TestApp_UserTags_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/users/admin/tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_UserTags_NonexistentUserReturnsOK(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/users/nonexistent/tags", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (tags returns empty for unknown user), got %d", w.Result().StatusCode)
	}
}

func TestApp_SetUserTags_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	body := []byte(`{"tags":["tag1","tag2"]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/users/admin/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetUserTags_NotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	body := []byte(`{"tags":["t"]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/users/nonexistent/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetUserTags_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodPut, "/api/users/admin/tags", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_SetUserTags_NilTags(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPut, "/api/users/admin/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

// --- Group Members ---

func TestApp_GroupMembers_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))

	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodGet, "/api/groups/g1/members", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_AddGroupMember_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.UserStore.CreateUser("u1", "user1", "User123!", auth.RoleUser)

	sessID := adminSession(t, app)
	body := []byte(`{"user_id":"u1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/groups/g1/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(w.Body)
		t.Fatalf("expected 204, got %d: %s", w.Result().StatusCode, string(b))
	}
}

func TestApp_AddGroupMember_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodPost, "/api/groups/g1/members", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}

func TestApp_RemoveGroupMember_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_, _ = app.UserStore.CreateUser("u1", "user1", "User123!", auth.RoleUser)
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("u1"), access.GroupID("g1"))

	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodDelete, "/api/groups/g1/members/u1", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Result().StatusCode)
	}
}

func TestApp_RemoveGroupMember_GroupNotFound(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)
	req := httptest.NewRequest(http.MethodDelete, "/api/groups/nonexistent/members/admin", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}

// --- TFTP Client Adapter ---

func TestTFTPClientAdapter_ReadDir(t *testing.T) {
	a := &tftpClientAdapter{}
	files, err := a.ReadDir("/")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if files != nil {
		t.Fatalf("expected nil, got %v", files)
	}
}

func TestTFTPClientAdapter_RemoveAll(t *testing.T) {
	a := &tftpClientAdapter{}
	err := a.RemoveAll("/foo")
	if !errors.Is(err, errTFTPNoDelete) {
		t.Fatalf("expected errTFTPNoDelete, got %v", err)
	}
}

func TestTFTPFileInfo(t *testing.T) {
	fi := &tftpFileInfo{name: "test.txt", size: 42}
	if fi.Name() != "test.txt" {
		t.Fatalf("expected test.txt, got %s", fi.Name())
	}
	if fi.Size() != 42 {
		t.Fatalf("expected 42, got %d", fi.Size())
	}
	if fi.Mode() != 0 {
		t.Fatalf("expected 0, got %v", fi.Mode())
	}
	if !fi.ModTime().IsZero() {
		t.Fatalf("expected zero time")
	}
	if fi.IsDir() {
		t.Fatal("expected not dir")
	}
	if fi.Sys() != nil {
		t.Fatal("expected nil")
	}
}

func TestTFTPWriteCloser_Write(t *testing.T) {
	wc := &tftpWriteCloser{}
	n, err := wc.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

// --- TFTP server files handlers ---

func setupAppWithTFTPServerTarget(t *testing.T, root string) (*App, string) {
	t.Helper()
	app := newTestApp(t)
	ctx := context.Background()
	_, _ = app.AccessGroupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = app.AccessGroupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	// Target that exposes the embedded TFTP server (protocol=tftp, tftp_enabled=true).
	id := access.TargetID("tftp1")
	if _, err := app.TargetStore.CreateWithPath(ctx, id, "TFTP1", "127.0.0.1", 69, access.ProtocolTFTP, access.GroupID("g1"), "", "", "", "", "", false, false, true); err != nil {
		t.Fatalf("CreateWithPath: %v", err)
	}
	_ = app.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID("g1"), id)
	return app, string(id)
}

func withTFTPRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old := os.Getenv("VANTYX_TFTP_ROOT")
	if err := os.Setenv("VANTYX_TFTP_ROOT", root); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		if old != "" {
			_ = os.Setenv("VANTYX_TFTP_ROOT", old)
		} else {
			_ = os.Unsetenv("VANTYX_TFTP_ROOT")
		}
	})
	return root
}

func TestTFTPServer_ListFiles_EmptyDir(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/files?path=/", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.StatusCode, w.Body.String())
	}
	body, _ := io.ReadAll(res.Body)
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("expected empty JSON array, got %s", string(body))
	}
}

func TestTFTPServer_UploadDownloadDelete(t *testing.T) {
	root := withTFTPRoot(t)
	app, targetID := setupAppWithTFTPServerTarget(t, root)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	// Upload
	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	const relPath = "/dir/file.txt"
	if err := mp.WriteField("path", relPath); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	fw, err := mp.CreateFormFile("file", "file.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	const content = "hello tftp"
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("Write file content: %v", err)
	}
	_ = mp.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/tftp/targets/"+targetID+"/files/upload", &buf)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("upload expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}

	// Download
	req = httptest.NewRequest(http.MethodGet, "/api/tftp/targets/"+targetID+"/files/download?path="+relPath, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("download expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	downloaded, _ := io.ReadAll(w.Body)
	if string(downloaded) != content {
		t.Fatalf("downloaded content mismatch: got %q, want %q", string(downloaded), content)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/tftp/targets/"+targetID+"/files?path="+relPath, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("delete expected 204, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	fullPath := filepath.Join(root, targetID, "dir", "file.txt")
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err=%v", err)
	}
}

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.1:8443", true},
		{"localhost", true},
		{"localhost:443", true},
		{"[::1]", true},
		{"[::1]:8443", true},
		{"10.0.0.1", false},
		{"vantyx.example.com", false},
	}
	for _, tc := range cases {
		if got := isLoopbackHost(tc.host); got != tc.want {
			t.Fatalf("isLoopbackHost(%q)=%v want %v", tc.host, got, tc.want)
		}
	}
}

// --- convertCastToVideo ---

func TestConvertCastToVideo_NoAggInPath(t *testing.T) {
	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", ""); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", oldPath)
	}()
	if _, _, _, err := convertCastToVideo("/tmp/nonexistent.cast", "gif"); err == nil {
		t.Fatal("expected error when agg is not found in PATH")
	}
}

// --- ListUsers with pagination ---

func TestApp_ListUsers_WithPagination(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/users?limit=10&offset=0", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestApp_ListUsers_InvalidPagination(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sessID := adminSession(t, app)

	req := httptest.NewRequest(http.MethodGet, "/api/users?limit=abc&offset=xyz", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (ignores bad params), got %d", w.Result().StatusCode)
	}
}

// decodeErrorMessage returns the `message` field of an error response
// body so locale-sensitive tests can read it concisely.
func decodeErrorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var er struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &er); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return er.Message
}

// TestApp_ErrorMessage_AcceptLanguageJapanese exercises the locale
// middleware via the Accept-Language header on an anonymous request.
func TestApp_ErrorMessage_AcceptLanguageJapanese(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Accept-Language", "ja-JP, en;q=0.5")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Result().StatusCode)
	}
	msg := decodeErrorMessage(t, w.Body.Bytes())
	// The Japanese phrase intentionally contains characters absent from
	// the English message, so a simple containment check is enough.
	if !strings.Contains(msg, "認証") {
		t.Fatalf("expected Japanese 'unauthorized' message, got %q", msg)
	}
}

// TestApp_ErrorMessage_AcceptLanguageEnglish ensures Accept-Language=en
// continues to receive the English wording (default fallback).
func TestApp_ErrorMessage_AcceptLanguageEnglish(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Accept-Language", "en-US")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if msg := decodeErrorMessage(t, w.Body.Bytes()); msg != "unauthorized" {
		t.Fatalf("expected English message 'unauthorized', got %q", msg)
	}
}

// TestApp_ErrorMessage_UserLocaleOverridesHeader pins the behavior
// where an authenticated user's saved locale wins over the request's
// Accept-Language header.
func TestApp_ErrorMessage_UserLocaleOverridesHeader(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	// Default admin is created by NewApp(); persist a Japanese locale
	// for them and reuse the cookie below.
	if err := app.UserStore.UpdateLocale("admin", "ja"); err != nil {
		t.Fatalf("UpdateLocale: %v", err)
	}
	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("SessionStore.Create: %v", err)
	}

	// Hit an admin-only path while pretending to be a regular user so
	// the response is `forbidden: admin only`. Setting Accept-Language
	// to English should be ignored in favor of the saved Japanese
	// preference.
	if _, err := app.UserStore.CreateUser("u1", "alice", "Alice1!x", "user"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	uSess, _ := app.SessionStore.Create("u1")
	if err := app.UserStore.UpdateLocale("u1", "ja"); err != nil {
		t.Fatalf("UpdateLocale u1: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Accept-Language", "en-US")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: uSess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Result().StatusCode)
	}
	if msg := decodeErrorMessage(t, w.Body.Bytes()); !strings.Contains(msg, "管理者") {
		t.Fatalf("expected Japanese 'admin only' message, got %q", msg)
	}

	// And the admin session continues to receive Japanese too, even
	// without an Accept-Language header.
	req2 := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader([]byte("not json")))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w2.Result().StatusCode)
	}
	if msg := decodeErrorMessage(t, w2.Body.Bytes()); !strings.Contains(msg, "リクエスト") {
		t.Fatalf("expected Japanese 'invalid request body', got %q", msg)
	}
}

// TestApp_ErrorMessage_LocalizedHandlers exercises a handful of
// recently migrated handlers (groups / recordings / settings) to make
// sure their error responses honor the locale resolved by the
// session middleware.
func TestApp_ErrorMessage_LocalizedHandlers(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	if err := app.UserStore.UpdateLocale("admin", "ja"); err != nil {
		t.Fatalf("UpdateLocale: %v", err)
	}
	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("SessionStore.Create: %v", err)
	}

	cases := []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode int
		wantSub  string
	}{
		{
			name:     "groups: name is required (ja)",
			method:   http.MethodPost,
			path:     "/api/groups",
			body:     `{"name":"  "}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "必須",
		},
		{
			name:     "recordings: bad format",
			method:   http.MethodGet,
			path:     "/api/recordings/abc/file?format=mp4",
			body:     "",
			wantCode: http.StatusBadRequest,
			wantSub:  "cast",
		},
		{
			name:     "settings: invalid proto",
			method:   http.MethodPut,
			path:     "/api/settings/audit-forwarder",
			body:     `{"config":{"proto":"sctp"}}`,
			wantCode: http.StatusBadRequest,
			wantSub:  "プロトコル",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = bytes.NewReader([]byte(tc.body))
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Result().StatusCode != tc.wantCode {
				t.Fatalf("status: got %d, want %d (body=%s)", w.Result().StatusCode, tc.wantCode, w.Body.String())
			}
			if msg := decodeErrorMessage(t, w.Body.Bytes()); !strings.Contains(msg, tc.wantSub) {
				t.Fatalf("expected localized message containing %q, got %q", tc.wantSub, msg)
			}
		})
	}
}
