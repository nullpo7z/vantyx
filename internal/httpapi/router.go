package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
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
	"github.com/nullpo7z/vantyx/internal/filetransfer"
	"github.com/nullpo7z/vantyx/internal/i18n"
	"github.com/nullpo7z/vantyx/internal/logging"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/recording"
	"github.com/nullpo7z/vantyx/internal/secret"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
)

// httpLogger is the slog logger tagged with the "httpapi.router"
// component. Other files in this package can share it for consistency.
var httpLogger = logging.WithComponent("httpapi.router")

// initialAdminPasswordEnv lets operators supply the bootstrap admin
// password through their secret manager. When unset, NewApp generates
// a random one-time password and writes it to stdout.
//
// #nosec G101 -- this is the name of the env var, not a credential.
const initialAdminPasswordEnv = "VANTYX_INITIAL_ADMIN_PASSWORD"

// App encapsulates HTTP handlers and shared dependencies.
//
// All field types are interfaces so tests can inject in-memory stores
// without touching the real database; production callers go through
// [NewApp] which wires SQLite-backed implementations.
type App struct {
	UserStore               auth.UserStore
	SessionStore            auth.SessionStore
	TargetStore             access.TargetStore
	AccessGroupStore        access.AccessGroupStore
	SSHKeyStore             access.SSHKeyStore
	CredentialIdentityStore access.CredentialIdentityStore

	TerminalSessionManager terminalSessionStarter
	LoginRateLimiter       *loginRateLimiter
	DB                     *sql.DB

	// SFTPClientFactory is optional. When set (typically in tests) the
	// file transfer handlers use it instead of dialling a real SSH
	// server.
	SFTPClientFactory SFTPClientFactoryFunc

	// RDPVNCManager tracks active RDP-to-VNC bridges
	// (xfreerdp → Xvfb → x11vnc).
	RDPVNCManager *rdpvnc.Manager

	// SessionEventBroker broadcasts session lifecycle events for SSE
	// (GET /api/events/sessions).
	SessionEventBroker *SessionEventBroker

	// FileTransferEventBroker streams per-user file transfer updates
	// for SSE (GET /api/events/file-transfers).
	FileTransferEventBroker *FileTransferEventBroker

	// TFTPWriteWindowEvents streams write-window open/close status for
	// SSE, per target (GET /api/tftp/targets/{target_id}/write-window/events).
	TFTPWriteWindowEvents *TFTPWriteWindowEventBroker

	// CommandLogStore persists terminal stdin lines for search.
	CommandLogStore *commandLogStore

	// FileTransferManager tracks background file upload / download jobs.
	FileTransferManager *filetransfer.Manager

	// SharingRegistry holds the in-memory rooms for collaborative
	// terminal sessions. Each running terminal session gets a Room
	// the first time someone interacts with the sharing API.
	SharingRegistry *sharing.Registry

	// SharingStore persists invitations (token hash, expiry, mode).
	// A nil store disables invitation issuance and join.
	SharingStore sharing.Store

	// SharingBridges tracks the live bridge controllers per session
	// so the HTTP layer can hand the write token to a different
	// participant at runtime.
	SharingBridges *bridgeRegistry

	// VNCSessionManager holds detachable VNC sessions for sharing.
	VNCSessionManager *session.Manager

	// videoRecordings tracks active RDP/VNC ffmpeg screen captures.
	videoRecordings *videoRecordingRegistry

	// RecordingExports tracks background GIF/MP4 export jobs.
	RecordingExports *recordingExportRegistry
}

// newAppDBOpen, newAppMigrate, and newAppUserStore are test seams used
// to inject failures without exposing constructor parameters.
var (
	newAppDBOpen    func(dbsqlite.Config) (*sql.DB, error)
	newAppMigrate   func(*sql.DB) error
	newAppUserStore func(*sql.DB) auth.UserStore
)

// sessionIdleWarnAfter reads VANTYX_TERMINAL_SESSION_IDLE_WARN_AFTER
// (default 30 minutes). A value of "0" disables idle warnings entirely.
func sessionIdleWarnAfter() time.Duration {
	v := strings.TrimSpace(os.Getenv("VANTYX_TERMINAL_SESSION_IDLE_WARN_AFTER"))
	if v == "0" {
		return 0
	}
	if v == "" {
		return 30 * time.Minute
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		httpLogger.Warn("invalid VANTYX_TERMINAL_SESSION_IDLE_WARN_AFTER, using default 30m", "value", v, "error", err)
		return 30 * time.Minute
	}
	return d
}

// applyTerminalSessionIdleWarn configures the idle-warning threshold on
// the terminal session manager from env.
func applyTerminalSessionIdleWarn(m *session.Manager) {
	if m == nil {
		return
	}
	if d := sessionIdleWarnAfter(); d > 0 {
		m.SetIdleWarnAfter(d)
	}
}

// applyRDPSessionIdleWarn configures the idle-warning threshold on the
// RDP / VNC session manager from env.
func applyRDPSessionIdleWarn(m *rdpvnc.Manager) {
	if m == nil {
		return
	}
	if d := sessionIdleWarnAfter(); d > 0 {
		m.SetIdleWarnAfter(d)
	}
}

// accessStoreConfigFromEnv builds [access.StoreConfig] from
// VANTYX_ACCESS_QUERY_TIMEOUT and VANTYX_ACCESS_DEFAULT_LIST_LIMIT
// (Twelve-Factor Config). Returns nil when neither is set so the store
// constructors use their built-in defaults.
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

// NewApp constructs an [App] backed by SQLite.
//
// If VANTYX_SQLITE_PATH is unset, [dbsqlite.DefaultPath] is used so
// data persists across container restarts.
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
	// Initialise the persistent audit sink after the schema is ready.
	initAuditSink(db)

	var userStore auth.UserStore = auth.NewSQLiteUserStore(db)
	if newAppUserStore != nil {
		userStore = newAppUserStore(db)
	}
	sessionStore := auth.NewSQLiteSessionStore(db, 24*time.Hour)
	storeCfg := accessStoreConfigFromEnv()
	encKey, encKeyErr := secret.LoadKeyFromEnvStrict("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")
	if encKeyErr != nil {
		panic(encKeyErr)
	}
	targetStore := access.NewSQLiteTargetStore(db, storeCfg, encKey)
	groupStore := access.NewSQLiteAccessGroupStore(db, storeCfg)
	sshKeyStore := access.NewSQLiteSSHKeyStore(db, storeCfg, encKey)
	credIdentityStore := access.NewSQLiteCredentialIdentityStore(db, sshKeyStore, storeCfg, encKey)
	terminalSessions := session.NewManager()
	vncSessions := session.NewManager()
	applyTerminalSessionIdleWarn(terminalSessions)
	rdpSessions := rdpvnc.NewManager()
	applyRDPSessionIdleWarn(rdpSessions)

	// Make sure the admin user exists. The bootstrap password is taken
	// from VANTYX_INITIAL_ADMIN_PASSWORD when set; otherwise we
	// generate a one-time random password, print it to stdout, and
	// require the operator to rotate it on first login. If the row
	// already exists we leave its password untouched (no surprise
	// re-injection from runtime restarts).
	if err := bootstrapAdminUser(userStore); err != nil {
		panic(err)
	}

	transferDir := filepath.Join(filepath.Dir(path), "file-transfers")
	if err := os.MkdirAll(transferDir, 0o700); err != nil {
		panic(err)
	}

	ftStore := filetransfer.NewStore(db)
	ftManager := filetransfer.NewManager(transferDir, ftStore)
	ftBroker := NewFileTransferEventBroker()
	ftManager.SetNotifier(ftBroker.Publish)
	reapCtx, reapCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := ftManager.ReapOrphans(reapCtx, "サーバー再起動により中断"); err != nil {
		httpLogger.Warn("filetransfer reaper failed", "error", err)
	}
	reapCancel()
	exportDir := filepath.Join(filepath.Dir(path), "recording-exports")
	if err := os.MkdirAll(exportDir, 0o700); err != nil {
		panic(err)
	}
	recordingsDir := os.Getenv("VANTYX_RECORDINGS_DIR")
	cleanupOrphanExportTemps(exportDir)
	cleanupLegacyRecordingDirExportTemps(recordingsDir)
	_ = recording.DefaultGovernor()
	return &App{
		UserStore:               userStore,
		SessionStore:            sessionStore,
		TargetStore:             targetStore,
		AccessGroupStore:        groupStore,
		SSHKeyStore:             sshKeyStore,
		CredentialIdentityStore: credIdentityStore,
		TerminalSessionManager:  terminalSessions,
		VNCSessionManager:       vncSessions,
		LoginRateLimiter:        newLoginRateLimiter(),
		DB:                      db,
		RDPVNCManager:           rdpSessions,
		SessionEventBroker:      NewSessionEventBroker(),
		FileTransferEventBroker: ftBroker,
		TFTPWriteWindowEvents:   NewTFTPWriteWindowEventBroker(),
		CommandLogStore:         newCommandLogStore(db),
		FileTransferManager:     ftManager,
		SharingRegistry:         sharing.NewRegistry(),
		SharingStore:            sharing.NewSQLiteStore(db),
		SharingBridges:          newBridgeRegistry(),
		RecordingExports:        newRecordingExportRegistry(exportDir),
	}
}

// sessionMiddleware resolves the session cookie and, for both
// authenticated and anonymous callers, picks a UI locale that the
// downstream handlers can use to localize error responses.
//
// Resolution order:
//  1. Authenticated user's saved preference (users.locale), when set.
//  2. The request's Accept-Language header (lightweight RFC 9110 parse).
//  3. The default locale (English).
//
// The chosen locale is attached to the request context via
// i18n.WithLocale so writeJSONErrorKey / i18n.TR can pick it up
// without touching individual handlers.
func (a *App) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		locale := a.resolveLocale(r)
		ctx := i18n.WithLocale(r.Context(), locale)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveLocale picks the best locale for the current request. Errors
// from the session or user stores fall through to the Accept-Language
// header, mirroring the "best effort" contract of the original
// placeholder middleware.
func (a *App) resolveLocale(r *http.Request) i18n.Locale {
	if c, err := r.Cookie("vantyx_session"); err == nil && c.Value != "" {
		if sess, err := a.SessionStore.Get(c.Value); err == nil && sess != nil {
			if u, err := a.UserStore.GetByID(sess.UserID); err == nil && u != nil && u.Locale != "" {
				if i18n.IsSupported(u.Locale) {
					return i18n.Normalise(u.Locale)
				}
			}
		}
	}
	return i18n.ParseAcceptLanguage(r.Header.Get("Accept-Language"))
}

// NewRouter constructs the main HTTP router for the Vantyx API.
func (a *App) NewRouter() http.Handler {
	r := chi.NewRouter()

	r.Use(a.requestLog)
	r.Use(SecurityHeadersMiddleware)
	r.Use(maxBodyBytesMiddleware(2 << 20))
	r.Use(csrfOriginMiddleware)
	r.Use(a.sessionMiddleware)
	r.Use(a.forcePasswordChangeMiddleware)

	// Health check.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Authentication.
	r.Post("/api/login", a.handleLogin)
	r.Post("/api/logout", a.handleLogout)
	r.Get("/api/me", a.handleMe)
	r.Post("/api/me/password", a.handleChangePassword)
	r.Put("/api/me/locale", a.handleUpdateLocale)
	r.Put("/api/me/timezone", a.handleUpdateTimezone)
	r.Get("/api/me/ssh-keys", a.handleListSSHKeys)
	r.Post("/api/me/ssh-keys", a.handleAddSSHKey)
	r.Delete("/api/me/ssh-keys/{key_id}", a.handleDeleteSSHKey)

	// Access groups.
	r.Get("/api/groups", a.handleGroups)
	r.Post("/api/groups", a.handleCreateGroup)
	r.Put("/api/groups/{group_id}", a.handleUpdateGroup)
	r.Delete("/api/groups/{group_id}", a.handleDeleteGroup)
	r.Get("/api/groups/{group_id}/members", a.handleGroupMembers)
	r.Post("/api/groups/{group_id}/members", a.handleAddGroupMember)
	r.Delete("/api/groups/{group_id}/members/{user_id}", a.handleRemoveGroupMember)
	r.Get("/api/groups/{group_id}/tags", a.handleGroupTags)
	r.Put("/api/groups/{group_id}/tags", a.handleSetGroupTags)

	// Users (admin only).
	r.Get("/api/users", a.handleListUsers)
	r.Post("/api/users", a.handleCreateUser)
	r.Delete("/api/users/{user_id}", a.handleDeleteUser)
	r.Get("/api/users/{user_id}/tags", a.handleUserTags)
	r.Put("/api/users/{user_id}/tags", a.handleSetUserTags)
	r.Get("/api/users/{user_id}/ssh-keys", a.handleListUserSSHKeys)
	r.Post("/api/users/{user_id}/ssh-keys", a.handleAddUserSSHKey)
	r.Delete("/api/users/{user_id}/ssh-keys/{key_id}", a.handleDeleteUserSSHKey)

	// Audit logs (admin only; in-memory recent events).
	r.Get("/api/audit", a.handleAuditLogs)
	// App settings (admin only).
	r.Get("/api/settings/audit-forwarder", a.handleGetAuditForwarderSettings)
	r.Put("/api/settings/audit-forwarder", a.handlePutAuditForwarderSettings)
	// Command logs (admin only).
	r.Get("/api/commands", a.handleCommandLogs)

	// Tag picker (requires auth).
	r.Get("/api/tags", a.handleListTags)

	// Targets.
	r.Get("/api/targets", a.handleTargets)
	r.Post("/api/targets", a.handleCreateTarget)
	r.Put("/api/targets/{target_id}", a.handleUpdateTarget)
	r.Delete("/api/targets/{target_id}", a.handleDeleteTarget)
	r.Get("/api/targets/{target_id}/tags", a.handleTargetTags)
	r.Put("/api/targets/{target_id}/tags", a.handleSetTargetTags)
	// SSH keys and identities (admin-only credential library).
	r.Get("/api/ssh-keys", a.handleSSHKeysList)
	r.Post("/api/ssh-keys", a.handleSSHKeysCreate)
	r.Post("/api/ssh-keys/generate", a.handleSSHKeysGenerate)
	r.Put("/api/ssh-keys/{key_id}", a.handleSSHKeysUpdate)
	r.Delete("/api/ssh-keys/{key_id}", a.handleSSHKeysDelete)
	r.Get("/api/credential-identities", a.handleCredentialIdentitiesList)
	r.Post("/api/credential-identities", a.handleCredentialIdentitiesCreate)
	r.Put("/api/credential-identities/{identity_id}", a.handleCredentialIdentitiesUpdate)
	r.Delete("/api/credential-identities/{identity_id}", a.handleCredentialIdentitiesDelete)
	// SSH host-key probing (TOFU helper) and host-key adoption /
	// clearing. Both are admin-only and audited.
	r.Post("/api/targets/probe-host-key", a.handleProbeHostKey)
	r.Put("/api/targets/{target_id}/ssh-host-key", a.handleUpdateTargetHostKey)

	// SSE pushers.
	r.Get("/api/events/sessions", a.handleSessionEvents)
	r.Get("/api/events/file-transfers", a.handleFileTransferEvents)
	r.Get("/api/invitations/incoming", a.handleListIncomingInvitations)

	// SSH terminal and session list.
	r.Route("/api/terminal/sessions", func(r chi.Router) {
		r.Get("/", a.handleTerminalSessions)
		r.Delete("/{session_id}", a.handleTerminalSessionDelete)
		// Collaborative session sharing (Phase A).
		r.Get("/{session_id}/invitation-options", a.handleInvitationOptions)
		r.Post("/{session_id}/invitations", a.handleCreateInvitation)
		r.Get("/{session_id}/invitations", a.handleListInvitations)
		r.Delete("/{session_id}/invitations/{invitation_id}", a.handleRevokeInvitation)
		r.Post("/{session_id}/invitations/{invitation_id}/join-url", a.handleRegenerateInvitationJoinURL)
		r.Post("/{session_id}/join", a.handleJoinSession)
		r.Get("/{session_id}/participants", a.handleListParticipants)
		r.Delete("/{session_id}/participants/{user_id}", a.handleKickParticipant)
		r.Post("/{session_id}/write-requests", a.handleCreateWriteRequest)
		r.Post("/{session_id}/write-requests/{request_id}/grant", a.handleGrantWriteRequest)
		r.Post("/{session_id}/write-requests/{request_id}/deny", a.handleDenyWriteRequest)
		r.Post("/{session_id}/write-token/release", a.handleReleaseWriteToken)
	})
	r.Get("/ws/ssh", a.handleSSHWebSocket)
	r.Get("/ws/vnc", a.handleVNCWebSocket)
	r.Route("/api/vnc/sessions", func(r chi.Router) {
		r.Get("/", a.handleVNCSessions)
		r.Get("/{session_id}/invitation-options", a.handleVNCInvitationOptions)
		r.Post("/{session_id}/invitations", a.handleVNCCreateInvitation)
		r.Get("/{session_id}/invitations", a.handleVNCListInvitations)
		r.Delete("/{session_id}/invitations/{invitation_id}", a.handleVNCRevokeInvitation)
		r.Post("/{session_id}/invitations/{invitation_id}/join-url", a.handleVNCRegenerateInvitationJoinURL)
		r.Post("/{session_id}/join", a.handleVNCJoinSession)
		r.Get("/{session_id}/participants", a.handleVNCListParticipants)
		r.Delete("/{session_id}/participants/{user_id}", a.handleVNCKickParticipant)
	})
	r.Route("/api/rdp/sessions", func(r chi.Router) {
		r.Get("/", a.handleRDPSessions)
		r.Delete("/{session_id}", a.handleRDPSessionDelete)
		r.Get("/{session_id}/invitation-options", a.handleRDPInvitationOptions)
		r.Post("/{session_id}/invitations", a.handleRDPCreateInvitation)
		r.Get("/{session_id}/invitations", a.handleRDPListInvitations)
		r.Delete("/{session_id}/invitations/{invitation_id}", a.handleRDPRevokeInvitation)
		r.Post("/{session_id}/invitations/{invitation_id}/join-url", a.handleRDPRegenerateInvitationJoinURL)
		r.Post("/{session_id}/join", a.handleRDPJoinSession)
		r.Get("/{session_id}/participants", a.handleRDPListParticipants)
		r.Delete("/{session_id}/participants/{user_id}", a.handleRDPKickParticipant)
	})
	r.Get("/ws/rdp", a.handleRDPWebSocket)
	r.Get("/ws/rdp/browser", a.handleRDPBrowserWebSocket)

	// Recordings (asciinema).
	r.Get("/api/recordings", a.handleListRecordings)
	r.Get("/api/recordings/exports", a.handleListRecordingExports)
	r.Get("/api/recordings/exports/{export_id}", a.handleGetRecordingExportStatus)
	r.Get("/api/recordings/exports/{export_id}/file", a.handleGetRecordingExportFile)
	r.Delete("/api/recordings/exports/{export_id}", a.handleDeleteRecordingExport)
	r.Post("/api/recordings/{recording_id}/export", a.handlePostRecordingExport)
	r.Get("/api/recordings/{recording_id}/file", a.handleGetRecordingFile)
	r.Delete("/api/recordings/{recording_id}", a.handleDeleteRecording)

	// Background file transfers.
	r.Get("/api/file-transfers", a.handleFileTransfersList)
	r.Post("/api/file-transfers/download", a.handleFileTransferStartDownload)
	r.Post("/api/file-transfers/upload", a.handleFileTransferUpload)
	r.Get("/api/file-transfers/{transfer_id}", a.handleFileTransferGet)
	r.Delete("/api/file-transfers/{transfer_id}", a.handleFileTransferDelete)
	r.Get("/api/file-transfers/{transfer_id}/content", a.handleFileTransferContent)

	// File transfer (SFTP / FTP).
	r.Get("/api/targets/{target_id}/files/download", a.handleDownloadFile)
	r.Post("/api/targets/{target_id}/files/upload", a.handleUploadFile)
	r.Get("/api/targets/{target_id}/files", a.handleListFiles)
	r.Delete("/api/targets/{target_id}/files", a.handleDeleteFile)

	// TFTP server (Vantyx serving files for network gear).
	r.Get("/api/tftp/targets/{target_id}/files", a.handleTFTPServerListFiles)
	r.Delete("/api/tftp/targets/{target_id}/files", a.handleTFTPServerDeleteFile)
	r.Get("/api/tftp/targets/{target_id}/files/download", a.handleTFTPServerDownloadFile)
	r.Post("/api/tftp/targets/{target_id}/files/upload", a.handleTFTPServerUploadFile)
	r.Get("/api/tftp/targets/{target_id}/write-window", a.handleTFTPGetWriteWindow)
	r.Get("/api/tftp/targets/{target_id}/write-window/events", a.handleTFTPWriteWindowEvents)
	r.Post("/api/tftp/targets/{target_id}/write-window", a.handleTFTPOpenWriteWindow)
	r.Delete("/api/tftp/targets/{target_id}/write-window", a.handleTFTPCloseWriteWindow)

	// Admin-only: API spec and Swagger UI.
	r.Get("/api/spec", a.handleAPISpec)
	r.Get("/docs", a.handleDocs)

	// SPA: serve web/dist when present (after `npm run build`).
	if dir := staticDir(); dir != "" {
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimPrefix(r.URL.Path, "/")
			if raw == "" {
				raw = "index.html"
			}
			// Prevent path traversal. filepath.Clean alone is insufficient because
			// Join(dir, "../x") escapes dir. We normalise to an absolute-clean
			// path first, then enforce that the final path stays within dir.
			//
			// Example attack: GET /../etc/passwd
			clean := filepath.Clean("/" + raw) // always absolute, so ".." collapses safely.
			rel := strings.TrimPrefix(clean, string(filepath.Separator))
			fpath := filepath.Join(dir, rel)
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				http.NotFound(w, r)
				return
			}
			if relp, err := filepath.Rel(dir, fpath); err != nil || relp == ".." || strings.HasPrefix(relp, ".."+string(filepath.Separator)) {
				http.NotFound(w, r)
				return
			}
			if f, err := os.Stat(fpath); err == nil && !f.IsDir() {
				http.ServeFile(w, r, fpath)
				return
			}
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
		})
	}

	return r
}

// bootstrapAdminUser ensures the bundled admin account exists. The
// resolution order is:
//
//  1. The user is already present in the database -> leave it alone.
//  2. VANTYX_INITIAL_ADMIN_PASSWORD is set -> use it (validated by the
//     password policy).
//  3. Otherwise generate a random one-time password, print it to
//     stdout, and store it. The login handler forces a password
//     change before any other API call succeeds.
//
// The original hard-coded "Admin123!" path is gone, so an inadvertent
// row deletion can no longer recreate the well-known credentials.
func bootstrapAdminUser(userStore auth.UserStore) error {
	if u, _ := userStore.GetByID("admin"); u != nil {
		return nil
	}
	pw := strings.TrimSpace(os.Getenv(initialAdminPasswordEnv))
	random := false
	if pw == "" {
		gen, err := generateInitialAdminPassword()
		if err != nil {
			return fmt.Errorf("generate initial admin password: %w", err)
		}
		pw = gen
		random = true
	}
	if _, err := userStore.CreateUser("admin", "admin", pw, auth.RoleAdmin); err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			return nil
		}
		return err
	}
	// Tag the freshly-created admin row so the API layer can refuse
	// non-rotation calls until the operator changes the password.
	if err := userStore.SetForcePasswordChange("admin", true); err != nil {
		// Non-fatal: log instead of panicking so the server still
		// starts; the password is unknown to attackers either way.
		httpLogger.Warn("could not set force_password_change for admin", "error", err)
	}
	if random {
		// stdout (not the structured logger) so operators see it
		// regardless of how slog is configured. Audit also captures
		// the event for traceability.
		fmt.Fprintf(os.Stdout, "\n==============================================================\n"+
			"Vantyx initial admin password (write it down – shown once):\n"+
			"  username: admin\n"+
			"  password: %s\n"+
			"You must rotate it from the SPA on first login.\n"+
			"Set VANTYX_INITIAL_ADMIN_PASSWORD to choose your own.\n"+
			"==============================================================\n\n", pw)
		audit("initial_admin_password_generated", auditFields{
			"user_id": "admin",
		})
	} else {
		audit("initial_admin_password_from_env", auditFields{
			"user_id": "admin",
		})
	}
	return nil
}

// generateInitialAdminPassword returns a random password that satisfies
// auth.ValidatePassword (upper, lower, digit, special, >= 16 chars).
func generateInitialAdminPassword() (string, error) {
	// 18 random bytes -> 24-char URL-safe Base64. We append fixed
	// characters from each required class so the result always meets
	// the password policy.
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(buf)
	return body + "A1!a", nil
}

// slugID returns a lowercased, hyphenated identifier built from s. It
// keeps the canonical ID alphabet [a-z0-9-] used by access stores.
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

// normalizeTargetPath collapses double slashes, strips empty path
// components, and rejects "." / "..".
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
