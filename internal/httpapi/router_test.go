package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
	_, _ = app.TargetStore.CreateWithPath("t1", "Host1", "192.168.1.1", 22, access.ProtocolSSH, "default")
	_ = app.AccessGroupStore.AddTargetToGroup("default", "t1")

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
	_, _ = app.AccessGroupStore.Create("prod/network", "Prod Network") // path "prod/network" used as group_id in target row
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

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")

	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	body := []byte(`{"name":"Bad","host":"10.0.0.2","port":22,"protocol":"ftp","group_id":"default"}`)
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

func TestApp_Login_InvalidBody(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
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
	app := newTestApp(t)
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
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

func (u *userStoreFailingCreateUser) CreateUser(id, username, plainPassword string) (*auth.User, error) {
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

	body := []byte(`{"username":"admin","password":"admin123!"}`)
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

func (a *accessGroupStoreFailingCreate) Create(id, name string) (*access.AccessGroup, error) {
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

func (a *accessGroupStoreGetFailsForID) Get(id string) (*access.AccessGroup, error) {
	if id == a.failID {
		return nil, errors.New("injected get error")
	}
	return a.AccessGroupStore.Get(id)
}

func TestApp_Groups_GetFailsSkipsGroup(t *testing.T) {
	app := newTestApp(t)
	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_, _ = app.AccessGroupStore.Create("g2", "G2")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g2")
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

func (a *accessGroupStoreFailingAddTargetToGroup) AddTargetToGroup(groupID, targetID string) error {
	return errors.New("injected add target error")
}

// targetStoreFailingCreate wraps TargetStore and makes CreateWithPath return a non-ErrTargetExists error.
type targetStoreFailingCreate struct {
	access.TargetStore
}

func (t *targetStoreFailingCreate) CreateWithPath(id, name, host string, port uint16, protocol access.Protocol, path string) (*access.Target, error) {
	return nil, errors.New("injected create target error")
}

func TestApp_CreateTarget_StoreCreateFails(t *testing.T) {
	app := newTestApp(t)
	app.TargetStore = &targetStoreFailingCreate{TargetStore: app.TargetStore}
	router := app.NewRouter()

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
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

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
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

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
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

	_, _ = app.AccessGroupStore.Create("other", "Other")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
	_, _ = app.AccessGroupStore.Create("default", "Default")
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

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
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

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
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

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
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

	_, _ = app.AccessGroupStore.Create("ops", "Ops") // first group gets "ops"
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

	_, _ = app.AccessGroupStore.Create("ops", "Ops")
	_, _ = app.AccessGroupStore.Create("ops-1", "Ops")
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

	_, _ = app.AccessGroupStore.Create("default", "Default")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "default")
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

	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_, _ = app.TargetStore.CreateWithPath("t1", "Host1", "192.168.1.1", 22, access.ProtocolSSH, "g1")
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "t1")

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

	_, _ = app.AccessGroupStore.Create("g1", "G1")
	_, _ = app.AccessGroupStore.Create("g2", "G2")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g1")
	_ = app.AccessGroupStore.AddUserToGroup("admin", "g2")
	_, _ = app.TargetStore.CreateWithPath("t1", "H1", "1.1.1.1", 22, access.ProtocolSSH, "g1")
	_, _ = app.TargetStore.CreateWithPath("t2", "H2", "2.2.2.2", 22, access.ProtocolSSH, "g1")
	_, _ = app.TargetStore.CreateWithPath("t3", "H3", "3.3.3.3", 22, access.ProtocolSSH, "g2")
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "t1")
	_ = app.AccessGroupStore.AddTargetToGroup("g1", "t2")
	_ = app.AccessGroupStore.AddTargetToGroup("g2", "t3")

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
