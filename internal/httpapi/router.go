package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/session"
)

// App encapsulates HTTP handlers and shared dependencies.
type App struct {
	UserStore        *auth.InMemoryUserStore
	SessionStore     *auth.InMemorySessionStore
	TargetStore      *access.InMemoryTargetStore
	AccessGroupStore *access.InMemoryAccessGroupStore

	TerminalSessionManager *session.Manager
}

// NewApp constructs an App with default in-memory dependencies.
func NewApp() *App {
	store := auth.NewInMemoryUserStore()
	sessions := auth.NewInMemorySessionStore(24 * time.Hour)
	targets := access.NewInMemoryTargetStore()
	groups := access.NewInMemoryAccessGroupStore()
	terminalSessions := session.NewManager()

	admin, _ := store.CreateUser("admin", "admin", "admin123!")
	_, _ = sessions.Create(admin.ID)

	_, _ = groups.Create("default", "Default")
	_ = groups.AddUserToGroup(admin.ID, "default")
	_, _ = targets.Create("demo", "Demo host", "127.0.0.1", 22, access.ProtocolSSH)
	_ = groups.AddTargetToGroup("default", "demo")

	return &App{
		UserStore:              store,
		SessionStore:           sessions,
		TargetStore:            targets,
		AccessGroupStore:       groups,
		TerminalSessionManager: terminalSessions,
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

	// Targets (requires auth)
	r.Get("/api/targets", a.handleTargets)

	// SSH/WebSocket terminal (Phase 2 skeleton)
	r.Get("/ws/ssh", a.handleSSHWebSocket)

	return r
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
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
