package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "httpapi.db")
	if err := os.Setenv("VANTYX_SQLITE_PATH", dbPath); err != nil {
		t.Fatalf("set env: %v", err)
	}
	return NewApp()
}

func TestApp_LoginSuccess(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	body := []byte(`{"username":"admin","password":"admin123!"}`)
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

func TestApp_CreateTarget_Success(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	// create default group and add admin
	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")

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
	ids := app.AccessGroupStore.TargetIDsForUser("admin")
	found := false
	for _, id := range ids {
		if id == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("new target %s not in admin's target list", created.ID)
	}
}

func TestApp_CreateTarget_WithPath(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")

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
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")

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
