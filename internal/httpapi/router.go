package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
	"github.com/nullpo7z/vantyx/internal/session"
)

// App encapsulates HTTP handlers and shared dependencies.
type App struct {
	UserStore        auth.UserStore
	SessionStore     auth.SessionStore
	TargetStore      access.TargetStore
	AccessGroupStore access.AccessGroupStore

	TerminalSessionManager *session.Manager
	DB                     *sql.DB
}

// NewApp constructs an App backed by SQLite.
func NewApp() *App {
	cfg := dbsqlite.Config{
		Path: os.Getenv("VANTYX_SQLITE_PATH"),
	}
	db, err := dbsqlite.Open(cfg)
	if err != nil {
		panic(err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		panic(err)
	}

	userStore := auth.NewSQLiteUserStore(db)
	sessionStore := auth.NewSQLiteSessionStore(db, 24*time.Hour)
	targetStore := access.NewSQLiteTargetStore(db)
	groupStore := access.NewSQLiteAccessGroupStore(db)
	terminalSessions := session.NewManager()

	// Ensure admin user exists.
	if _, err := userStore.CreateUser("admin", "admin", "admin123!"); err != nil && !errors.Is(err, auth.ErrUserExists) {
		panic(err)
	}

	return &App{
		UserStore:              userStore,
		SessionStore:           sessionStore,
		TargetStore:            targetStore,
		AccessGroupStore:       groupStore,
		TerminalSessionManager: terminalSessions,
		DB:                     db,
	}
}

// NewRouter constructs the main HTTP router for the Vantyx API.
func (a *App) NewRouter() http.Handler {
	r := chi.NewRouter()

	r.Use(a.sessionMiddleware)

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Authentication
	r.Post("/api/login", a.handleLogin)
	r.Get("/api/me", a.handleMe)

	// Access groups (requires auth)
	r.Get("/api/groups", a.handleGroups)
	r.Post("/api/groups", a.handleCreateGroup)

	// Targets (requires auth)
	r.Get("/api/targets", a.handleTargets)
	r.Post("/api/targets", a.handleCreateTarget)

	// SSH/WebSocket terminal
	r.Get("/ws/ssh", a.handleSSHWebSocket)

	// SPA: serve web/dist when present (after npm run build)
	if dir := staticDir(); dir != "" {
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			fpath := filepath.Join(dir, filepath.Clean(path))
			if f, err := os.Stat(fpath); err == nil && !f.IsDir() {
				http.ServeFile(w, r, fpath)
				return
			}
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
		})
	}

	return r
}

// staticDir returns "web/dist" if it exists and is a directory, else "".
func staticDir() string {
	dir := "web/dist"
	if d, err := os.Stat(dir); err == nil && d.IsDir() {
		return dir
	}
	return ""
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	u, err := a.UserStore.Authenticate(req.Username, req.Password)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	sess, err := a.SessionStore.Create(u.ID)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "vantyx_session",
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{
		UserID:   u.ID,
		Username: u.Username,
	})
}

func (a *App) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("vantyx_session")
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		_, _ = a.SessionStore.Get(c.Value) // placeholder for attaching to context in later phases
		next.ServeHTTP(w, r)
	})
}

// handleMe returns information about the currently authenticated user.
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	u, err := a.UserStore.GetByID(sess.UserID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{
		UserID:   u.ID,
		Username: u.Username,
	})
}

type targetResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Protocol string `json:"protocol"`
	Path     string `json:"path"`
}

type groupResponse struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Targets []targetResponse `json:"targets"`
}

// handleGroups returns access groups the current user belongs to, including their targets.
func (a *App) handleGroups(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	groupIDs := a.AccessGroupStore.GroupIDsForUser(sess.UserID)
	out := make([]groupResponse, 0, len(groupIDs))
	for _, gid := range groupIDs {
		g, err := a.AccessGroupStore.Get(gid)
		if err != nil {
			continue
		}
		tids := a.AccessGroupStore.TargetIDsForGroup(gid)
		targets := a.TargetStore.ListByIDs(tids)
		tout := make([]targetResponse, 0, len(targets))
		for _, t := range targets {
			tout = append(tout, targetResponse{
				ID:       t.ID,
				Name:     t.Name,
				Host:     t.Host,
				Port:     t.Port,
				Protocol: string(t.Protocol),
				Path:     t.Path,
			})
		}
		out = append(out, groupResponse{ID: g.ID, Name: g.Name, Targets: tout})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

type createGroupRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// handleCreateGroup creates a new access group and adds the current user to it.
func (a *App) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Path = normalizeTargetPath(req.Path)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	base := slugID(req.Name)
	if req.Path != "" {
		base = req.Path + "/" + base
	}
	id := base
	for i := 0; ; i++ {
		if i > 0 {
			id = base + "-" + strconv.Itoa(i)
		}
		_, err := a.AccessGroupStore.Create(id, req.Name)
		if err == nil {
			break
		}
		if !errors.Is(err, access.ErrGroupExists) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	_ = a.AccessGroupStore.AddUserToGroup(sess.UserID, id)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(groupResponse{ID: id, Name: req.Name, Targets: []targetResponse{}})
}

// handleTargets returns the list of targets the current user can access.
func (a *App) handleTargets(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ids := a.AccessGroupStore.TargetIDsForUser(sess.UserID)
	targets := a.TargetStore.ListByIDs(ids)

	out := make([]targetResponse, 0, len(targets))
	for _, t := range targets {
		out = append(out, targetResponse{
			ID:       t.ID,
			Name:     t.Name,
			Host:     t.Host,
			Port:     t.Port,
			Protocol: string(t.Protocol),
			Path:     t.Path,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

type createTargetRequest struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     uint16 `json:"port"`
	Protocol string `json:"protocol"`
	Path     string `json:"path"`
	GroupID  string `json:"group_id"`
}

// handleCreateTarget creates a new target and adds it to the specified access group.
func (a *App) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = sess // reserved for future per-user permission

	var req createTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.Path = normalizeTargetPath(req.Path)
	req.GroupID = strings.TrimSpace(req.GroupID)
	if req.Name == "" || req.Host == "" {
		http.Error(w, "name and host are required", http.StatusBadRequest)
		return
	}
	if req.GroupID == "" {
		http.Error(w, "group_id is required", http.StatusBadRequest)
		return
	}
	if _, err := a.AccessGroupStore.Get(req.GroupID); err != nil {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}
	allowedGroups := a.AccessGroupStore.GroupIDsForUser(sess.UserID)
	allowed := false
	for _, gid := range allowedGroups {
		if gid == req.GroupID {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	protocol := access.ProtocolSSH
	if req.Protocol == "telnet" {
		protocol = access.ProtocolTelnet
	} else if req.Protocol != "" && req.Protocol != "ssh" {
		http.Error(w, "protocol must be ssh or telnet", http.StatusBadRequest)
		return
	}

	baseID := slugID(req.Name)
	id := baseID
	for i := 0; ; i++ {
		if i > 0 {
			id = baseID + "-" + strconv.Itoa(i)
		}
		path := req.Path
		if path == "" {
			path = req.GroupID
		}
		_, err := a.TargetStore.CreateWithPath(id, req.Name, req.Host, req.Port, protocol, path)
		if err == nil {
			break
		}
		if !errors.Is(err, access.ErrTargetExists) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := a.AccessGroupStore.AddTargetToGroup(req.GroupID, id); err != nil {
		http.Error(w, "failed to assign target to group", http.StatusInternalServerError)
		return
	}

	t, _ := a.TargetStore.Get(id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(targetResponse{
		ID:       t.ID,
		Name:     t.Name,
		Host:     t.Host,
		Port:     t.Port,
		Protocol: string(t.Protocol),
		Path:     t.Path,
	})
}

func slugID(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b = append(b, c+32)
		} else if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b = append(b, c)
		} else if c == ' ' || c == '-' || c == '_' {
			if len(b) > 0 && b[len(b)-1] != '-' {
				b = append(b, '-')
			}
		}
	}
	if len(b) == 0 {
		return "target"
	}
	if b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	return string(b)
}

func normalizeTargetPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "/")
}

