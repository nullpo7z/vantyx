package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
)

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

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type sshKeyResponse struct {
	ID        int64  `json:"id"`
	KeyLine   string `json:"key_line"`
	CreatedAt string `json:"created_at"`
}

type addSSHKeyRequest struct {
	AuthorizedKey string `json:"authorized_key"`
}

// handleLogin authenticates a user with username and password. On
// success it issues a new server-side session and sets the
// vantyx_session cookie.
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := ""
	if a.LoginRateLimiter != nil {
		ip = a.LoginRateLimiter.clientIP(r)
		if !a.LoginRateLimiter.allow(ip) {
			audit("login_rate_limited", auditFields{
				"ip": ip,
			})
			writeJSONError(w, "too many failed attempts; try again later", http.StatusTooManyRequests)
			return
		}
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		audit("login_failed", auditFields{
			"username": req.Username,
			"reason":   "invalid_request_body",
		})
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	u, err := a.UserStore.Authenticate(req.Username, req.Password)
	if err != nil {
		if a.LoginRateLimiter != nil && ip != "" {
			a.LoginRateLimiter.recordFailure(ip)
		}
		audit("login_failed", auditFields{
			"username": req.Username,
			"reason":   "invalid_credentials",
		})
		writeJSONError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	sess, err := a.SessionStore.Create(u.ID)
	if err != nil {
		audit("login_failed", auditFields{
			"username": req.Username,
			"reason":   "session_create_failed",
			"error":    err.Error(),
		})
		writeJSONError(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	audit("login_success", auditFields{
		"user_id":  u.ID,
		"username": u.Username,
	})

	cookie := &http.Cookie{
		Name:     "vantyx_session",
		Value:    sess.ID,
		Path:     "/",
		MaxAge:   24 * 3600, // 24h, matches SessionStore TTL (ASVS V2.2).
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	// Skip Secure in E2E / local runs so cookies survive self-signed-cert browser sessions.
	if r.TLS != nil && !isLoopbackHost(r.Host) {
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

// handleLogout invalidates the current session server-side and clears
// the cookie (OWASP ASVS V2.4).
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
	if r.TLS != nil && !isLoopbackHost(r.Host) {
		clearCookie.Secure = true
	}
	http.SetCookie(w, clearCookie)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns information about the currently authenticated user.
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	u, err := a.UserStore.GetByID(userID)
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

// handleChangePassword updates the current user's password.
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

// handleListSSHKeys returns the current user's SSH public keys (used
// for CLI gateway login). Admin only.
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

// handleAddSSHKey adds an SSH public key (authorized_keys format) for
// the current user. Admin only.
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

// handleDeleteSSHKey removes an SSH public key for the current user.
// Admin only.
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

// handleListUserSSHKeys returns SSH public keys for the given user.
// Admin only; used by the user-management UI.
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

// handleAddUserSSHKey adds an SSH public key for the given user. Admin
// only.
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

// handleDeleteUserSSHKey removes an SSH public key for the given user.
// Admin only.
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
