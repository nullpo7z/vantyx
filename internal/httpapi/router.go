package httpapi

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	api "github.com/nullpo7z/vantyx/docs/api"
	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/secret"
	"github.com/nullpo7z/vantyx/internal/session"
)

const (
	loginRateLimitWindow = 15 * time.Minute
	loginRateLimitN      = 5
	defaultAdminPassword = "Admin123!"
)

// adminUserID is the user ID that is allowed to access /api/spec and /docs.
const adminUserID = "admin"

// loginRateLimiter limits failed login attempts per IP (ASVS V2.5).
type loginRateLimiter struct {
	mu     sync.Mutex
	byIP   map[string][]time.Time
	window time.Duration
	maxTry int
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		byIP:   make(map[string][]time.Time),
		window: loginRateLimitWindow,
		maxTry: loginRateLimitN,
	}
}

func (l *loginRateLimiter) clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "" {
		return host
	}
	return r.RemoteAddr
}

func (l *loginRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	times := l.byIP[ip]
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[n] = t
			n++
		}
	}
	times = times[:n]
	l.byIP[ip] = times
	return len(times) < l.maxTry
}

func (l *loginRateLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byIP[ip] = append(l.byIP[ip], time.Now())
}

// App encapsulates HTTP handlers and shared dependencies.
type App struct {
	UserStore        auth.UserStore
	SessionStore     auth.SessionStore
	TargetStore      access.TargetStore
	AccessGroupStore access.AccessGroupStore

	TerminalSessionManager terminalSessionStarter
	LoginRateLimiter       *loginRateLimiter
	DB                     *sql.DB

	// SFTPClientFactory is optional. When set (e.g. in tests), file transfer handlers use it instead of connecting to a real SSH server.
	SFTPClientFactory SFTPClientFactoryFunc

	// RDPVNCManager tracks active RDP-to-VNC bridges (xfreerdp→Xvfb→x11vnc).
	RDPVNCManager *rdpvnc.Manager
}

// newAppDBOpen, newAppMigrate, and newAppUserStore are set in tests to inject failures for coverage.
var (
	newAppDBOpen    func(dbsqlite.Config) (*sql.DB, error)
	newAppMigrate   func(*sql.DB) error
	newAppUserStore func(*sql.DB) auth.UserStore
)

// accessStoreConfigFromEnv builds access.StoreConfig from env (Twelve-Factor Config).
// VANTYX_ACCESS_QUERY_TIMEOUT: duration e.g. "5s"; VANTYX_ACCESS_DEFAULT_LIST_LIMIT: integer.
// Returns nil if neither is set so store constructors use defaults.
func accessStoreConfigFromEnv() *access.StoreConfig {
	timeoutStr := os.Getenv("VANTYX_ACCESS_QUERY_TIMEOUT")
	limitStr := os.Getenv("VANTYX_ACCESS_DEFAULT_LIST_LIMIT")
	if timeoutStr == "" && limitStr == "" {
		return nil
	}
	cfg := &access.StoreConfig{}
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil && d > 0 {
			cfg.QueryTimeout = d
		}
	}
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			cfg.DefaultListLimit = n
		}
	}
	return cfg
}

// NewApp constructs an App backed by SQLite.
// If VANTYX_SQLITE_PATH is not set, data/vantyx.db is used so data persists across restarts.
func NewApp() *App {
	path := os.Getenv("VANTYX_SQLITE_PATH")
	if path == "" {
		path = dbsqlite.DefaultPath
	}
	cfg := dbsqlite.Config{Path: path}
	open := dbsqlite.Open
	if newAppDBOpen != nil {
		open = newAppDBOpen
	}
	db, err := open(cfg)
	if err != nil {
		panic(err)
	}
	migrate := dbsqlite.Migrate
	if newAppMigrate != nil {
		migrate = newAppMigrate
	}
	if err := migrate(db); err != nil {
		panic(err)
	}

	var userStore auth.UserStore = auth.NewSQLiteUserStore(db)
	if newAppUserStore != nil {
		userStore = newAppUserStore(db)
	}
	sessionStore := auth.NewSQLiteSessionStore(db, 24*time.Hour)
	storeCfg := accessStoreConfigFromEnv()
	encKey := secret.LoadKeyFromEnv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	targetStore := access.NewSQLiteTargetStore(db, storeCfg, encKey)
	groupStore := access.NewSQLiteAccessGroupStore(db, storeCfg)
	terminalSessions := session.NewManager()

	// Ensure admin user exists (password meets policy: 8+ chars, upper, lower, digit, special).
	if _, err := userStore.CreateUser("admin", "admin", defaultAdminPassword, auth.RoleAdmin); err != nil && !errors.Is(err, auth.ErrUserExists) {
		panic(err)
	}

	return &App{
		UserStore:              userStore,
		SessionStore:           sessionStore,
		TargetStore:            targetStore,
		AccessGroupStore:       groupStore,
		TerminalSessionManager: terminalSessions,
		LoginRateLimiter:       newLoginRateLimiter(),
		DB:                     db,
		RDPVNCManager:          rdpvnc.NewManager(),
	}
}

// responseWriter wraps http.ResponseWriter to record status code for logging.
// It implements http.Hijacker by delegating to the underlying ResponseWriter so WebSocket upgrade works.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("responseWriter: underlying ResponseWriter does not implement http.Hijacker")
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrap := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrap, r)
		remote := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.Index(xff, ","); i > 0 {
				remote = strings.TrimSpace(xff[:i])
			} else {
				remote = strings.TrimSpace(xff)
			}
		}
		// #nosec G706 -- audit log; path/remote from request
		log.Printf("http method=%s path=%s status=%d remote=%s duration=%s",
			r.Method, r.URL.Path, wrap.status, remote, time.Since(start).Round(time.Millisecond))
	})
}

// NewRouter constructs the main HTTP router for the Vantyx API.
func (a *App) NewRouter() http.Handler {
	r := chi.NewRouter()

	r.Use(requestLog)
	r.Use(a.sessionMiddleware)

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Authentication
	r.Post("/api/login", a.handleLogin)
	r.Post("/api/logout", a.handleLogout)
	r.Get("/api/me", a.handleMe)
	r.Post("/api/me/password", a.handleChangePassword)
	r.Get("/api/me/ssh-keys", a.handleListSSHKeys)
	r.Post("/api/me/ssh-keys", a.handleAddSSHKey)
	r.Delete("/api/me/ssh-keys/{key_id}", a.handleDeleteSSHKey)

	// Access groups (requires auth)
	r.Get("/api/groups", a.handleGroups)
	r.Post("/api/groups", a.handleCreateGroup)
	r.Get("/api/groups/{group_id}/members", a.handleGroupMembers)
	r.Post("/api/groups/{group_id}/members", a.handleAddGroupMember)
	r.Delete("/api/groups/{group_id}/members/{user_id}", a.handleRemoveGroupMember)
	r.Get("/api/groups/{group_id}/tags", a.handleGroupTags)
	r.Put("/api/groups/{group_id}/tags", a.handleSetGroupTags)

	// Users (admin only)
	r.Get("/api/users", a.handleListUsers)
	r.Post("/api/users", a.handleCreateUser)
	r.Get("/api/users/{user_id}/tags", a.handleUserTags)
	r.Put("/api/users/{user_id}/tags", a.handleSetUserTags)
	r.Get("/api/users/{user_id}/ssh-keys", a.handleListUserSSHKeys)
	r.Post("/api/users/{user_id}/ssh-keys", a.handleAddUserSSHKey)
	r.Delete("/api/users/{user_id}/ssh-keys/{key_id}", a.handleDeleteUserSSHKey)

	// Tags: list all tags registered in the system (user/target/group) for tag picker (requires auth)
	r.Get("/api/tags", a.handleListTags)

	// Targets (requires auth)
	r.Get("/api/targets", a.handleTargets)
	r.Post("/api/targets", a.handleCreateTarget)
	r.Put("/api/targets/{target_id}", a.handleUpdateTarget)
	r.Delete("/api/targets/{target_id}", a.handleDeleteTarget)
	r.Get("/api/targets/{target_id}/tags", a.handleTargetTags)
	r.Put("/api/targets/{target_id}/tags", a.handleSetTargetTags)

	// SSH/WebSocket terminal and session list (Phase 2: resume)
	r.Route("/api/terminal/sessions", func(r chi.Router) {
		r.Get("/", a.handleTerminalSessions)
		r.Delete("/{session_id}", a.handleTerminalSessionDelete)
	})
	r.Get("/ws/ssh", a.handleSSHWebSocket)
	r.Get("/ws/vnc", a.handleVNCWebSocket)
	r.Get("/ws/rdp", a.handleRDPWebSocket)
	r.Get("/ws/rdp/browser", a.handleRDPBrowserWebSocket)
	r.Get("/api/rdp/file", a.handleRDPFile)

	// Recordings (asciinema): list and download (owner only)
	r.Get("/api/recordings", a.handleListRecordings)
	r.Get("/api/recordings/{recording_id}/file", a.handleGetRecordingFile)

	// File transfer (SFTP): list, download, upload, delete (requires auth + target access + stored credentials)
	r.Get("/api/targets/{target_id}/files/download", a.handleDownloadFile)
	r.Post("/api/targets/{target_id}/files/upload", a.handleUploadFile)
	r.Get("/api/targets/{target_id}/files", a.handleListFiles)
	r.Delete("/api/targets/{target_id}/files", a.handleDeleteFile)

	// Admin-only: API spec and reference (Swagger UI)
	r.Get("/api/spec", a.handleAPISpec)
	r.Get("/docs", a.handleDocs)

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

// staticDirForTest overrides staticDir in tests; set to a temp dir with index.html to cover SPA branch.
var staticDirForTest string

// staticDir returns "web/dist" if it exists and is a directory, else "".
func staticDir() string {
	if staticDirForTest != "" {
		return staticDirForTest
	}
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
	UserID                string `json:"user_id"`
	Username              string `json:"username"`
	Role                  string `json:"role"`
	RequirePasswordChange bool   `json:"require_password_change,omitempty"`
}

type errorResponse struct {
	Message string `json:"message"`
}

func writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{Message: message})
}

// writeInternalError logs err and sends a generic message (ASVS V8.1: do not expose internal errors to client).
func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeJSONError(w, "internal error", http.StatusInternalServerError)
}

// writeServiceUnavailableError logs err and sends a generic message for 503.
func writeServiceUnavailableError(w http.ResponseWriter, err error) {
	log.Printf("service unavailable: %v", err)
	writeJSONError(w, "service unavailable", http.StatusServiceUnavailable)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := ""
	if a.LoginRateLimiter != nil {
		ip = a.LoginRateLimiter.clientIP(r)
		if !a.LoginRateLimiter.allow(ip) {
			// #nosec G706 -- audit log; ip from client
			log.Printf("login rate limited ip=%s", ip)
			writeJSONError(w, "too many failed attempts; try again later", http.StatusTooManyRequests)
			return
		}
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("login failed username=%s err=invalid request body", req.Username)
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	u, err := a.UserStore.Authenticate(req.Username, req.Password)
	if err != nil {
		if a.LoginRateLimiter != nil && ip != "" {
			a.LoginRateLimiter.recordFailure(ip)
		}
		log.Printf("login failed username=%s err=invalid credentials", req.Username)
		writeJSONError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	sess, err := a.SessionStore.Create(u.ID)
	if err != nil {
		log.Printf("login failed username=%s err=session create %v", req.Username, err)
		writeJSONError(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	log.Printf("login success user_id=%s username=%s", u.ID, u.Username)

	cookie := &http.Cookie{
		Name:     "vantyx_session",
		Value:    sess.ID,
		Path:     "/",
		MaxAge:   24 * 3600, // 24h, matches SessionStore TTL (ASVS V2.2)
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if r.TLS != nil {
		cookie.Secure = true
	}
	http.SetCookie(w, cookie)

	requireChange := u.ID == adminUserID && req.Password == defaultAdminPassword
	if u.Role == "" {
		u.Role = auth.RoleUser
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{
		UserID:                u.ID,
		Username:              u.Username,
		Role:                  u.Role,
		RequirePasswordChange: requireChange,
	})
}

// handleLogout invalidates the current session server-side and clears the cookie (ASVS V2.4).
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err == nil && c.Value != "" {
		a.SessionStore.Delete(c.Value)
	}
	clearCookie := &http.Cookie{
		Name:     "vantyx_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if r.TLS != nil {
		clearCookie.Secure = true
	}
	http.SetCookie(w, clearCookie)
	w.WriteHeader(http.StatusNoContent)
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
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	u, err := a.UserStore.GetByID(sess.UserID)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if u.Role == "" {
		u.Role = auth.RoleUser
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
	})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword updates the current user's password (ASVS default password change).
func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	err := a.UserStore.UpdatePassword(userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrWrongPassword):
			writeJSONError(w, "current password is wrong", http.StatusUnauthorized)
			return
		case errors.Is(err, auth.ErrPasswordUnchanged):
			writeJSONError(w, "new password must differ from current", http.StatusBadRequest)
			return
		case errors.Is(err, auth.ErrUserNotFound):
			writeJSONError(w, "unauthorized", http.StatusUnauthorized)
			return
		default:
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

type sshKeyResponse struct {
	ID        int64  `json:"id"`
	KeyLine   string `json:"key_line"`
	CreatedAt string `json:"created_at"`
}

type addSSHKeyRequest struct {
	AuthorizedKey string `json:"authorized_key"`
}

// handleListSSHKeys returns the current user's SSH public keys (for vantyx SSH login). Admin only.
func (a *App) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := a.currentUserID(r)
	keys, err := a.UserStore.ListPublicKeys(userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]sshKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, sshKeyResponse{
			ID:        k.ID,
			KeyLine:   k.KeyLine,
			CreatedAt: k.CreatedAt.Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// handleAddSSHKey adds an SSH public key for the current user (authorized_keys format). Admin only.
func (a *App) handleAddSSHKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := a.currentUserID(r)
	var req addSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.AuthorizedKey == "" {
		writeJSONError(w, "authorized_key is required", http.StatusBadRequest)
		return
	}
	id, err := a.UserStore.AddPublicKey(userID, req.AuthorizedKey)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidPublicKey) {
			writeJSONError(w, "invalid SSH public key", http.StatusBadRequest)
			return
		}
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sshKeyResponse{ID: id, KeyLine: req.AuthorizedKey, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
}

// handleDeleteSSHKey removes an SSH public key for the current user. Admin only.
func (a *App) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := a.currentUserID(r)
	keyIDStr := chi.URLParam(r, "key_id")
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil || keyID <= 0 {
		writeJSONError(w, "invalid key_id", http.StatusBadRequest)
		return
	}
	err = a.UserStore.DeletePublicKey(userID, keyID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "key not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListUserSSHKeys returns SSH public keys for the given user (admin only). Used from user management.
func (a *App) handleListUserSSHKeys(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeJSONError(w, "user_id required", http.StatusBadRequest)
		return
	}
	keys, err := a.UserStore.ListPublicKeys(userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]sshKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, sshKeyResponse{
			ID:        k.ID,
			KeyLine:   k.KeyLine,
			CreatedAt: k.CreatedAt.Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// handleAddUserSSHKey adds an SSH public key for the given user (admin only).
func (a *App) handleAddUserSSHKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeJSONError(w, "user_id required", http.StatusBadRequest)
		return
	}
	var req addSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.AuthorizedKey == "" {
		writeJSONError(w, "authorized_key is required", http.StatusBadRequest)
		return
	}
	id, err := a.UserStore.AddPublicKey(userID, req.AuthorizedKey)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidPublicKey) {
			writeJSONError(w, "invalid SSH public key", http.StatusBadRequest)
			return
		}
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "user not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sshKeyResponse{ID: id, KeyLine: req.AuthorizedKey, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
}

// handleDeleteUserSSHKey removes an SSH public key for the given user (admin only).
func (a *App) handleDeleteUserSSHKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeJSONError(w, "user_id required", http.StatusBadRequest)
		return
	}
	keyIDStr := chi.URLParam(r, "key_id")
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil || keyID <= 0 {
		writeJSONError(w, "invalid key_id", http.StatusBadRequest)
		return
	}
	err = a.UserStore.DeletePublicKey(userID, keyID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "key not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// currentUserID returns the authenticated user's ID from the session cookie, or "" if not authenticated.
func (a *App) currentUserID(r *http.Request) string {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		return ""
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		return ""
	}
	return sess.UserID
}

// requireAdmin writes 403 JSON and returns false if the current user does not have admin role; otherwise returns true.
func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	if u.Role != auth.RoleAdmin {
		writeJSONError(w, "forbidden: admin only", http.StatusForbidden)
		return false
	}
	return true
}

func (a *App) handleAPISpec(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(api.OpenAPIYAML)
}

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="UTF-8">
  <title>Vantyx API リファレンス</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    .vantyx-notice { padding: 10px 16px; margin: 0; background: #fef3c7; border-bottom: 1px solid #f59e0b; color: #92400e; font-size: 14px; }
    .vantyx-notice strong { font-weight: 600; }
  </style>
</head>
<body>
  <p class="vantyx-notice">
    <strong>Try it out で NetworkError が出る場合:</strong> 自己署名証明書を使っているときは、先にこのサイトの証明書を信頼してください。
    <a href="/" target="_blank" rel="noopener">トップを新しいタブで開き</a>、「詳細」→「安全な接続を続行」などで例外を許可してから、このページを再読み込みして再度 Try it out を実行してください。
  </p>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: window.location.origin + '/api/spec',
        dom_id: '#swagger-ui',
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIBundle.SwaggerUIStandalonePreset
        ],
        requestInterceptor: function(req) { req.credentials = 'same-origin'; return req; }
      });
    };
  </script>
</body>
</html>
`

func (a *App) handleDocs(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swaggerUIHTML))
}

type targetResponse struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Host                 string   `json:"host"`
	Port                 uint16   `json:"port"`
	Protocol             string   `json:"protocol"`
	Path                 string   `json:"path"`
	SSHUsername          string   `json:"ssh_username,omitempty"`
	HasStoredCredentials bool     `json:"has_stored_credentials,omitempty"`
	HasSSHKey            bool     `json:"has_ssh_key,omitempty"`
	NeedsPassword        bool     `json:"needs_password,omitempty"`   // ユーザー名は保存済みだがパスワードが未保存（接続時に入力）
	NeedsPassphrase      bool     `json:"needs_passphrase,omitempty"` // 暗号化された秘密鍵は保存済みだがパスフレーズが未保存（接続時に入力）
	Tags                 []string `json:"tags,omitempty"`
}

type groupResponse struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Targets []targetResponse `json:"targets"`
}

// isEncryptedPEMBlock returns true if the PEM block is encrypted (traditional PEM ENCRYPTED or OpenSSH bcrypt kdf).
func isEncryptedPEMBlock(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "ENCRYPTED") {
		return true
	}
	if strings.Contains(s, "bcrypt") {
		return true
	}
	return false
}

func targetToResponse(t *access.Target, tags []string) targetResponse {
	r := targetResponse{
		ID:          string(t.ID),
		Name:        t.Name,
		Host:        t.Host,
		Port:        t.Port,
		Protocol:    string(t.Protocol),
		Path:        t.Path,
		SSHUsername: t.SSHUsername,
		Tags:        tags,
	}
	if tags == nil {
		r.Tags = []string{}
	}
	if t.SSHUsername != "" {
		r.HasStoredCredentials = true
	}
	if t.SSHPrivateKey != "" {
		r.HasSSHKey = true
	}
	if t.SSHUsername != "" && t.SSHPassword == "" && t.SSHPrivateKey == "" {
		r.NeedsPassword = true
	}
	if t.SSHPrivateKey != "" && t.SSHPrivateKeyPassphrase == "" && isEncryptedPEMBlock(t.SSHPrivateKey) {
		r.NeedsPassphrase = true
	}
	return r
}

// listOptsFromRequest parses limit and after_id from query. Returns nil if neither is set (no pagination).
func listOptsFromRequest(r *http.Request) *access.ListOpts {
	q := r.URL.Query()
	afterID := strings.TrimSpace(q.Get("after_id"))
	limitStr := strings.TrimSpace(q.Get("limit"))
	if afterID == "" && limitStr == "" {
		return nil
	}
	opts := &access.ListOpts{AfterID: afterID}
	if limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n <= 0 {
			return opts
		}
		if n > 1000 {
			n = 1000
		}
		opts.Limit = n
	} else {
		opts.Limit = 100
	}
	return opts
}

// handleGroups returns access groups the current user belongs to, including their targets.
func (a *App) handleGroups(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	opts := listOptsFromRequest(r)
	paginate := opts != nil
	pageLimit := 0
	if paginate {
		pageLimit = opts.Limit
		opts = &access.ListOpts{Limit: pageLimit + 1, AfterID: opts.AfterID}
	}
	groupIDs, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(sess.UserID), opts)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var nextCursor string
	if paginate && pageLimit > 0 && len(groupIDs) > pageLimit {
		groupIDs = groupIDs[:pageLimit]
		nextCursor = string(groupIDs[pageLimit-1])
	}
	// Targets this user is allowed to see (membership or tag-based)
	allowedTargetIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowedSet := make(map[access.TargetID]bool)
	for _, id := range allowedTargetIDs {
		allowedSet[id] = true
	}

	out := make([]groupResponse, 0, len(groupIDs))
	for _, gid := range groupIDs {
		g, err := a.AccessGroupStore.Get(ctx, gid)
		if err != nil {
			continue
		}
		tids, err := a.AccessGroupStore.TargetIDsForGroup(ctx, gid, nil)
		if err != nil {
			continue
		}
		// Only include targets the user is allowed to see (avoids leaking targets when access is tag-based)
		var filtered []access.TargetID
		for _, tid := range tids {
			if allowedSet[tid] {
				filtered = append(filtered, tid)
			}
		}
		targets, err := a.TargetStore.ListByIDs(ctx, filtered, nil)
		if err != nil {
			continue
		}
		tout := make([]targetResponse, 0, len(targets))
		for _, t := range targets {
			tags, _ := a.TargetStore.TagsForTarget(ctx, t.ID)
			tout = append(tout, targetToResponse(t, tags))
		}
		out = append(out, groupResponse{ID: string(g.ID), Name: g.Name, Targets: tout})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if paginate {
		_ = json.NewEncoder(w).Encode(struct {
			Items      []groupResponse `json:"items"`
			NextCursor string          `json:"next_cursor,omitempty"`
		}{Items: out, NextCursor: nextCursor})
	} else {
		_ = json.NewEncoder(w).Encode(out)
	}
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
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Path = normalizeTargetPath(req.Path)
	if req.Name == "" {
		writeJSONError(w, "name is required", http.StatusBadRequest)
		return
	}

	base := slugID(req.Name)
	if req.Path != "" {
		base = req.Path + "/" + base
	}
	ctx := r.Context()
	id := base
	for i := 0; ; i++ {
		if i > 0 {
			id = base + "-" + strconv.Itoa(i)
		}
		_, err := a.AccessGroupStore.Create(ctx, access.GroupID(id), req.Name)
		if err == nil {
			break
		}
		if !errors.Is(err, access.ErrGroupExists) {
			writeInternalError(w, err)
			return
		}
	}
	if err := a.AccessGroupStore.AddUserToGroup(ctx, access.UserID(sess.UserID), access.GroupID(id)); err != nil {
		writeInternalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(groupResponse{ID: id, Name: req.Name, Targets: []targetResponse{}})
}

type userResponse struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Role     string   `json:"role"`
	Tags     []string `json:"tags,omitempty"`
}

// handleListUsers returns all users (admin only). Does not expose password hashes.
func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	users, err := a.UserStore.ListUsers(limit, offset)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, u := range users {
		role := u.Role
		if role == "" {
			role = auth.RoleUser
		}
		tags, _ := a.UserStore.TagsForUser(u.ID)
		if tags == nil {
			tags = []string{}
		}
		out = append(out, userResponse{ID: u.ID, Username: u.Username, Role: role, Tags: tags})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

type createUserRequest struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// handleCreateUser creates a new user (admin only).
func (a *App) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.ID = strings.TrimSpace(req.ID)
	req.Role = strings.TrimSpace(req.Role)
	if req.Username == "" {
		writeJSONError(w, "username is required", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		writeJSONError(w, "password is required", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		req.ID = slugID(req.Username)
	}
	if req.Role != auth.RoleAdmin && req.Role != auth.RoleUser {
		req.Role = auth.RoleUser
	}
	u, err := a.UserStore.CreateUser(req.ID, req.Username, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeJSONError(w, "user already exists (id or username)", http.StatusConflict)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	role := u.Role
	if role == "" {
		role = auth.RoleUser
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(userResponse{ID: u.ID, Username: u.Username, Role: role})
}

// handleUserTags returns tags for the user (admin only).
func (a *App) handleUserTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := chi.URLParam(r, "user_id")
	userID = strings.TrimSpace(userID)
	if userID == "" {
		writeJSONError(w, "user_id required", http.StatusBadRequest)
		return
	}
	tags, err := a.UserStore.TagsForUser(userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "user not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse{Tags: tags})
}

// handleSetUserTags sets tags for the user (admin only).
func (a *App) handleSetUserTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := chi.URLParam(r, "user_id")
	userID = strings.TrimSpace(userID)
	if userID == "" {
		writeJSONError(w, "user_id required", http.StatusBadRequest)
		return
	}
	var req setTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	if err := a.UserStore.SetUserTags(userID, req.Tags); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "user not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse(req))
}

// handleGroupMembers returns users belonging to the group (admin only).
func (a *App) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		writeJSONError(w, "group_id required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	opts := listOptsFromRequest(r)
	userIDs, err := a.AccessGroupStore.UserIDsForGroup(ctx, access.GroupID(groupID), opts)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]userResponse, 0, len(userIDs))
	for _, uid := range userIDs {
		u, err := a.UserStore.GetByID(string(uid))
		if err != nil {
			continue
		}
		role := u.Role
		if role == "" {
			role = auth.RoleUser
		}
		out = append(out, userResponse{ID: u.ID, Username: u.Username, Role: role})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

type addGroupMemberRequest struct {
	UserID string `json:"user_id"`
}

// handleAddGroupMember adds a user to an access group (admin only).
func (a *App) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := chi.URLParam(r, "group_id")
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		writeJSONError(w, "group_id required", http.StatusBadRequest)
		return
	}
	var req addGroupMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeJSONError(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if _, err := a.UserStore.GetByID(req.UserID); err != nil {
		writeJSONError(w, "user not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if _, err := a.AccessGroupStore.Get(ctx, access.GroupID(groupID)); err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONError(w, "group not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := a.AccessGroupStore.AddUserToGroup(ctx, access.UserID(req.UserID), access.GroupID(groupID)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveGroupMember removes a user from an access group (admin only).
func (a *App) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := chi.URLParam(r, "group_id")
	userID := chi.URLParam(r, "user_id")
	groupID = strings.TrimSpace(groupID)
	userID = strings.TrimSpace(userID)
	if groupID == "" || userID == "" {
		writeJSONError(w, "group_id and user_id required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if err := a.AccessGroupStore.RemoveUserFromGroup(ctx, access.UserID(userID), access.GroupID(groupID)); err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONError(w, "group not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tagsResponse struct {
	Tags []string `json:"tags"`
}

// handleGroupTags returns tags for the group. Caller must belong to the group or be admin.
func (a *App) handleGroupTags(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "group_id")
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		writeJSONError(w, "group_id required", http.StatusBadRequest)
		return
	}
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if u.Role != auth.RoleAdmin {
		gids, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(userID), nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		allowed := false
		for _, gid := range gids {
			if string(gid) == groupID {
				allowed = true
				break
			}
		}
		if !allowed {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	tags, err := a.AccessGroupStore.TagsForGroup(ctx, access.GroupID(groupID))
	if err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONError(w, "group not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse{Tags: tags})
}

type setTagsRequest struct {
	Tags []string `json:"tags"`
}

// handleSetGroupTags sets tags for the group. Caller must belong to the group or be admin.
func (a *App) handleSetGroupTags(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "group_id")
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		writeJSONError(w, "group_id required", http.StatusBadRequest)
		return
	}
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if u.Role != auth.RoleAdmin {
		gids, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(userID), nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		allowed := false
		for _, gid := range gids {
			if string(gid) == groupID {
				allowed = true
				break
			}
		}
		if !allowed {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	var req setTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	if err := a.AccessGroupStore.SetGroupTags(ctx, access.GroupID(groupID), req.Tags); err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONError(w, "group not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse(req))
}

// handleListTags returns all distinct tags from user_tags, target_tags, and group_tags (requires auth).
func (a *App) handleListTags(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := a.SessionStore.Get(c.Value); err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := a.DB.QueryContext(ctx, `
		SELECT DISTINCT tag FROM (
			SELECT tag FROM user_tags
			UNION
			SELECT tag FROM target_tags
			UNION
			SELECT tag FROM group_tags
		) ORDER BY tag`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			writeInternalError(w, err)
			return
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	if tags == nil {
		tags = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		Tags []string `json:"tags"`
	}{Tags: tags})
}

// handleTargets returns the list of targets the current user can access.
func (a *App) handleTargets(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	opts := listOptsFromRequest(r)
	paginate := opts != nil
	pageLimit := 0
	if paginate {
		pageLimit = opts.Limit
		opts = &access.ListOpts{Limit: pageLimit + 1, AfterID: opts.AfterID}
	}
	ids, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), opts)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var nextCursor string
	if paginate && pageLimit > 0 && len(ids) > pageLimit {
		ids = ids[:pageLimit]
		nextCursor = string(ids[pageLimit-1])
	}
	targets, err := a.TargetStore.ListByIDs(ctx, ids, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]targetResponse, 0, len(targets))
	for _, t := range targets {
		tags, _ := a.TargetStore.TagsForTarget(ctx, t.ID)
		out = append(out, targetToResponse(t, tags))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if paginate {
		_ = json.NewEncoder(w).Encode(struct {
			Items      []targetResponse `json:"items"`
			NextCursor string           `json:"next_cursor,omitempty"`
		}{Items: out, NextCursor: nextCursor})
	} else {
		_ = json.NewEncoder(w).Encode(out)
	}
}

type createTargetRequest struct {
	Name                    string `json:"name"`
	Host                    string `json:"host"`
	Port                    uint16 `json:"port"`
	Protocol                string `json:"protocol"`
	Path                    string `json:"path"`
	GroupID                 string `json:"group_id"`
	SSHUsername             string `json:"ssh_username"`
	SSHPassword             string `json:"ssh_password"`
	SSHPrivateKey           string `json:"ssh_private_key"`
	SSHPrivateKeyPassphrase string `json:"ssh_private_key_passphrase"`
}

// handleCreateTarget creates a new target and adds it to the specified access group.
func (a *App) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = sess // reserved for future per-user permission

	var req createTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.Path = normalizeTargetPath(req.Path)
	req.GroupID = strings.TrimSpace(req.GroupID)
	if req.Name == "" || req.Host == "" {
		writeJSONError(w, "name and host are required", http.StatusBadRequest)
		return
	}
	if req.GroupID == "" {
		writeJSONError(w, "group_id is required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, err := a.AccessGroupStore.Get(ctx, access.GroupID(req.GroupID)); err != nil {
		writeJSONError(w, "group not found", http.StatusNotFound)
		return
	}
	allowedGroups, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowed := false
	for _, gid := range allowedGroups {
		if gid == access.GroupID(req.GroupID) {
			allowed = true
			break
		}
	}
	if !allowed {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	protocol := access.ProtocolSSH
	if req.Protocol == "telnet" {
		protocol = access.ProtocolTelnet
	} else if req.Protocol == "vnc" {
		protocol = access.ProtocolVNC
	} else if req.Protocol == "tftp" {
		protocol = access.ProtocolTFTP
	} else if req.Protocol == "rdp" {
		protocol = access.ProtocolRDP
	} else if req.Protocol != "" && req.Protocol != "ssh" {
		writeJSONError(w, "protocol must be ssh, telnet, vnc, tftp, or rdp", http.StatusBadRequest)
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
		_, err := a.TargetStore.CreateWithPath(ctx, access.TargetID(id), req.Name, req.Host, req.Port, protocol, access.GroupID(req.GroupID), path, strings.TrimSpace(req.SSHUsername), req.SSHPassword, req.SSHPrivateKey, req.SSHPrivateKeyPassphrase)
		if err == nil {
			break
		}
		if errors.Is(err, access.ErrEncryptionKeyRequired) {
			writeServiceUnavailableError(w, err)
			return
		}
		if !errors.Is(err, access.ErrTargetExists) {
			writeInternalError(w, err)
			return
		}
	}
	if err := a.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID(req.GroupID), access.TargetID(id)); err != nil {
		writeJSONError(w, "failed to assign target to group", http.StatusInternalServerError)
		return
	}

	t, _ := a.TargetStore.Get(ctx, access.TargetID(id))
	tags, _ := a.TargetStore.TagsForTarget(ctx, access.TargetID(id))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(targetToResponse(t, tags))
}

type updateTargetRequest struct {
	Name                    string  `json:"name"`
	Host                    string  `json:"host"`
	Port                    uint16  `json:"port"`
	Protocol                string  `json:"protocol"`
	Path                    string  `json:"path"`
	SSHUsername             string  `json:"ssh_username"`
	SSHPassword             *string `json:"ssh_password,omitempty"`               // nil = 変更しない、空文字 = クリア
	SSHPrivateKey           *string `json:"ssh_private_key,omitempty"`            // nil = 変更しない、空文字 = クリア
	SSHPrivateKeyPassphrase *string `json:"ssh_private_key_passphrase,omitempty"` // nil = 変更しない、空文字 = クリア
}

// handleUpdateTarget updates an existing target. Caller must have access to the target.
func (a *App) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	targetID := chi.URLParam(r, "target_id")
	if targetID == "" {
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowed := false
	for _, id := range allowedIDs {
		if id == access.TargetID(targetID) {
			allowed = true
			break
		}
	}
	if !allowed {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}
	var req updateTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.Path = normalizeTargetPath(req.Path)
	if req.Name == "" || req.Host == "" {
		writeJSONError(w, "name and host are required", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	protocol := access.ProtocolSSH
	if req.Protocol == "telnet" {
		protocol = access.ProtocolTelnet
	} else if req.Protocol == "vnc" {
		protocol = access.ProtocolVNC
	} else if req.Protocol == "tftp" {
		protocol = access.ProtocolTFTP
	} else if req.Protocol == "rdp" {
		protocol = access.ProtocolRDP
	} else if req.Protocol != "" && req.Protocol != "ssh" {
		writeJSONError(w, "protocol must be ssh, telnet, vnc, tftp, or rdp", http.StatusBadRequest)
		return
	}
	var sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string
	cur, errCur := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if errCur == nil {
		sshPassword = cur.SSHPassword
		sshPrivateKey = cur.SSHPrivateKey
		sshPrivateKeyPassphrase = cur.SSHPrivateKeyPassphrase
	}
	if req.SSHPassword != nil {
		sshPassword = *req.SSHPassword
	}
	if req.SSHPrivateKey != nil {
		sshPrivateKey = *req.SSHPrivateKey
	}
	if req.SSHPrivateKeyPassphrase != nil {
		sshPrivateKeyPassphrase = *req.SSHPrivateKeyPassphrase
	}
	t, err := a.TargetStore.Update(ctx, access.TargetID(targetID), req.Name, req.Host, req.Port, protocol, req.Path, strings.TrimSpace(req.SSHUsername), sshPassword, sshPrivateKey, sshPrivateKeyPassphrase)
	if err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONError(w, "target not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, access.ErrEncryptionKeyRequired) {
			writeServiceUnavailableError(w, err)
			return
		}
		writeInternalError(w, err)
		return
	}
	tags, _ := a.TargetStore.TagsForTarget(ctx, t.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(targetToResponse(t, tags))
}

// handleDeleteTarget deletes a target. Caller must have access to the target.
func (a *App) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	targetID := chi.URLParam(r, "target_id")
	if targetID == "" {
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowed := false
	for _, id := range allowedIDs {
		if id == access.TargetID(targetID) {
			allowed = true
			break
		}
	}
	if !allowed {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := a.TargetStore.Delete(ctx, access.TargetID(targetID)); err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONError(w, "target not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTargetTags returns tags for the target. Caller must have access to the target.
func (a *App) handleTargetTags(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "target_id")
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowed := false
	for _, id := range allowedIDs {
		if string(id) == targetID {
			allowed = true
			break
		}
	}
	if !allowed {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}
	tags, err := a.TargetStore.TagsForTarget(ctx, access.TargetID(targetID))
	if err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONError(w, "target not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse{Tags: tags})
}

// handleSetTargetTags sets tags for the target. Caller must have access to the target.
func (a *App) handleSetTargetTags(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "target_id")
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowed := false
	for _, id := range allowedIDs {
		if string(id) == targetID {
			allowed = true
			break
		}
	}
	if !allowed {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}
	var req setTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	if err := a.TargetStore.SetTargetTags(ctx, access.TargetID(targetID), req.Tags); err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONError(w, "target not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse(req))
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
