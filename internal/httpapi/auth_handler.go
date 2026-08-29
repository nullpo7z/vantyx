package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Locale   string `json:"locale,omitempty"`
	Timezone string `json:"timezone"` // site-wide display timezone (admin setting); "" = browser local

	RequirePasswordChange bool `json:"require_password_change,omitempty"`
	TOTPEnabled           bool `json:"totp_enabled,omitempty"`
}

// meResponse extends the login payload with what the account page shows.
type meResponse struct {
	loginResponse
	Tags     []string  `json:"tags"`
	Groups   []meGroup `json:"groups"`
	Passkeys int       `json:"passkeys"`
}

type meGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type updateLocaleRequest struct {
	Locale string `json:"locale"`
}

type localeResponse struct {
	Locale string `json:"locale"`
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
//
// Per-user lockout (CWE-307) and IP rate limiting are both consulted
// so distributed attacks targeting a single account cannot side-step
// the throttle.
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := ""
	if a.LoginRateLimiter != nil {
		ip = a.LoginRateLimiter.clientIP(r)
		if !a.LoginRateLimiter.allowIP(ip) {
			audit("login_rate_limited", auditFields{
				"ip":    ip,
				"scope": "ip",
			})
			writeJSONErrorKey(w, r, "auth.tooManyAttempts", http.StatusTooManyRequests)
			return
		}
	}

	// Browser CSRF defense for cookie-authenticated APIs (CWE-352).
	// /api/login is exempt from the global csrfOriginMiddleware so we
	// repeat the Origin check here with a stricter "cross-site is a
	// hard failure" stance.
	if !sameOriginRequest(r) {
		audit("login_origin_rejected", auditFields{
			"origin": r.Header.Get("Origin"),
			"host":   r.Host,
		})
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		audit("login_failed", auditFields{
			"username_hash": auditUsernameHash(req.Username),
			"reason":        "invalid_request_body",
		})
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if a.LoginRateLimiter != nil && !a.LoginRateLimiter.allowUser(req.Username) {
		audit("login_rate_limited", auditFields{
			"username_hash": auditUsernameHash(req.Username),
			"scope":         "user",
		})
		writeJSONErrorKey(w, r, "auth.tooManyAttempts", http.StatusTooManyRequests)
		return
	}
	u, err := a.UserStore.Authenticate(req.Username, req.Password)
	if err != nil {
		if a.LoginRateLimiter != nil {
			if ip != "" {
				a.LoginRateLimiter.recordFailureIP(ip)
			}
			a.LoginRateLimiter.recordFailureUser(req.Username)
		}
		audit("login_failed", auditFields{
			"username_hash": auditUsernameHash(req.Username),
			"reason":        "invalid_credentials",
		})
		writeJSONErrorKey(w, r, "auth.invalidCredentials", http.StatusUnauthorized)
		return
	}
	if a.LoginRateLimiter != nil {
		a.LoginRateLimiter.recordSuccess(ip, req.Username)
	}

	// Second factor: when the account has TOTP enabled the password alone
	// does not issue a session. Hand back a short-lived challenge token
	// that POST /api/login/totp completes (see auth_totp.go).
	totpOn := a.TOTPStore != nil && a.TOTPStore.Enabled(r.Context(), u.ID)
	passkeysOn := a.hasPasskeys(r, u.ID)
	if a.mfaPending != nil && (totpOn || passkeysOn) {
		tok, err := a.mfaPending.issue(u.ID, u.Username, ip)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		methods := []string{}
		if totpOn {
			methods = append(methods, "totp")
		}
		if passkeysOn {
			methods = append(methods, "webauthn")
		}
		audit("login_mfa_required", auditFields{"user_id": u.ID, "methods": methods})
		writeJSON(w, map[string]interface{}{
			"mfa_required": true,
			"mfa_token":    tok,
			"methods":      methods,
		})
		return
	}

	audit("login_success", auditFields{
		"user_id":  u.ID,
		"username": u.Username,
	})
	// Session + cookie + response are shared with the TOTP / OIDC paths.
	a.finishLogin(w, r, u, "")
}

// handleLogout invalidates the current session server-side and clears
// the cookie (OWASP ASVS V2.4).
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err == nil && c.Value != "" {
		if delErr := a.SessionStore.Delete(c.Value); delErr != nil {
			// Surface the failure to operators (audit) but still drop
			// the cookie on the client so they don't end up "stuck".
			audit("logout_delete_failed", auditFields{"error": delErr.Error()})
		}
	}
	// #nosec G124 -- same rationale as the login cookie above (Secure
	// is runtime-controlled, all other safe-cookie attributes are set).
	clearCookie := &http.Cookie{
		Name:     "vantyx_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   cookieSecure(r),
	}
	http.SetCookie(w, clearCookie)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns information about the currently authenticated user.
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	u, err := a.UserStore.GetByID(userID)
	if err != nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if u.Role == "" {
		u.Role = auth.RoleUser
	}

	totpEnabled := a.TOTPStore != nil && a.TOTPStore.Enabled(r.Context(), u.ID)
	resp := meResponse{
		loginResponse: loginResponse{
			UserID:      u.ID,
			Username:    u.Username,
			Role:        u.Role,
			Locale:      u.Locale,
			Timezone:    a.displayTimezone(),
			TOTPEnabled: totpEnabled,
		},
		Tags:   []string{},
		Groups: []meGroup{},
	}
	if a.WebAuthn != nil {
		resp.Passkeys, _ = a.WebAuthn.Count(r.Context(), u.ID)
	}
	if tags, err := a.UserStore.TagsForUser(u.ID); err == nil && tags != nil {
		resp.Tags = tags
	}
	if a.AccessGroupStore != nil {
		// Groups the user can reach (memberships, tags, and everything
		// below those groups) -- what the account page shows as
		// "accessible groups".
		if gids, err := a.AccessGroupStore.GroupIDsForUser(r.Context(), access.UserID(u.ID), &access.ListOpts{Limit: 500}); err == nil {
			for _, gid := range gids {
				name := string(gid)
				if g, gerr := a.AccessGroupStore.Get(r.Context(), gid); gerr == nil && g != nil && g.Name != "" {
					name = g.Name
				}
				resp.Groups = append(resp.Groups, meGroup{ID: string(gid), Name: name})
			}
		}
	}
	writeJSON(w, resp)
}

// handleUpdateLocale persists the current user's UI locale preference.
// Pass {"locale":""} to clear the preference (frontend default applies).
func (a *App) handleUpdateLocale(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	var req updateLocaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	loc, err := auth.NormalizeUILocale(req.Locale)
	if err != nil {
		writeJSONErrorKey(w, r, "auth.unsupportedLocale", http.StatusBadRequest)
		return
	}
	if err := a.UserStore.UpdateLocale(userID, loc); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
			return
		}
		if errors.Is(err, auth.ErrInvalidLocale) {
			writeJSONErrorKey(w, r, "auth.unsupportedLocale", http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("user_locale_update", auditFields{
		"user_id": userID,
		"locale":  loc,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(localeResponse{Locale: loc})
}

// handleChangePassword updates the current user's password.
//
// On success it also clears the force-rotation flag (used for the
// bootstrap admin account) and revokes every other session for the
// caller (ASVS V3.3.1 / CWE-613).
func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	// /api/me/password is a credential-mutating endpoint that must
	// not be brute-forced through a stolen cookie. Throttle per IP
	// just like /api/login (CWE-307).
	ip := ""
	if a.LoginRateLimiter != nil {
		ip = a.LoginRateLimiter.clientIP(r)
		if !a.LoginRateLimiter.allowIP(ip) {
			audit("password_change_rate_limited", auditFields{"ip": ip})
			writeJSONErrorKey(w, r, "auth.tooManyAttempts", http.StatusTooManyRequests)
			return
		}
	}
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	err := a.UserStore.UpdatePassword(userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		if a.LoginRateLimiter != nil && ip != "" && errors.Is(err, auth.ErrWrongPassword) {
			a.LoginRateLimiter.recordFailureIP(ip)
			a.LoginRateLimiter.recordFailureUser(userID)
		}
		switch {
		case errors.Is(err, auth.ErrWrongPassword):
			audit("password_change_wrong_current", auditFields{"user_id": userID})
			writeJSONErrorKey(w, r, "auth.currentPasswordWrong", http.StatusUnauthorized)
		case errors.Is(err, auth.ErrPasswordUnchanged):
			writeJSONErrorKey(w, r, "auth.passwordUnchanged", http.StatusBadRequest)
		case errors.Is(err, auth.ErrUserNotFound):
			writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		case errors.Is(err, auth.ErrEmptyPassword):
			writeJSONErrorKey(w, r, "auth.passwordEmpty", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeJSONErrorKey(w, r, "auth.passwordTooShort", http.StatusBadRequest, "min", auth.MinPasswordLength)
		case errors.Is(err, auth.ErrPasswordTooLong):
			writeJSONErrorKey(w, r, "auth.passwordTooLong", http.StatusBadRequest, "max", auth.MaxPasswordLength)
		case errors.Is(err, auth.ErrPasswordNoUpper):
			writeJSONErrorKey(w, r, "auth.passwordNoUpper", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordNoLower):
			writeJSONErrorKey(w, r, "auth.passwordNoLower", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordNoDigit):
			writeJSONErrorKey(w, r, "auth.passwordNoDigit", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordNoSpecial):
			writeJSONErrorKey(w, r, "auth.passwordNoSpecial", http.StatusBadRequest)
		default:
			writeInternalError(w, err)
		}
		return
	}
	if a.LoginRateLimiter != nil {
		a.LoginRateLimiter.recordSuccess(ip, userID)
	}
	// Clear the "force password change" sentinel and revoke every
	// other session for this user so a stolen cookie loses its grip.
	_ = a.UserStore.SetForcePasswordChange(userID, false)
	keep := ""
	if c, err := r.Cookie("vantyx_session"); err == nil {
		keep = c.Value
	}
	if err := a.SessionStore.DeleteAllForUser(userID, keep); err != nil {
		audit("password_change_session_revoke_failed", auditFields{
			"user_id": userID,
			"error":   err.Error(),
		})
	}
	audit("password_change_success", auditFields{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

// handleListSSHKeys returns the current user's SSH public keys (used
// for CLI gateway login). Any authenticated user may manage their own
// keys (CWE-285): the previous "admin only" restriction forced a
// privilege escalation flow that defeated the audit trail.
func (a *App) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
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

// handleAddSSHKey adds an SSH public key (authorized_keys format) for
// the current user. Available to any authenticated user.
func (a *App) handleAddSSHKey(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	var req addSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if req.AuthorizedKey == "" {
		writeJSONErrorKey(w, r, "auth.sshKeyAuthorizedKeyReq", http.StatusBadRequest)
		return
	}
	id, err := a.UserStore.AddPublicKey(userID, req.AuthorizedKey)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidPublicKey) {
			writeJSONErrorKey(w, r, "auth.invalidSSHKey", http.StatusBadRequest)
			return
		}
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
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
// Available to any authenticated user.
func (a *App) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	keyIDStr := chi.URLParam(r, "key_id")
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil || keyID <= 0 {
		writeJSONErrorKey(w, r, "auth.sshKeyIDInvalid", http.StatusBadRequest)
		return
	}
	err = a.UserStore.DeletePublicKey(userID, keyID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONErrorKey(w, r, "auth.sshKeyNotFound", http.StatusNotFound)
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
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
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
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
		return
	}
	var req addSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if req.AuthorizedKey == "" {
		writeJSONErrorKey(w, r, "auth.sshKeyAuthorizedKeyReq", http.StatusBadRequest)
		return
	}
	id, err := a.UserStore.AddPublicKey(userID, req.AuthorizedKey)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidPublicKey) {
			writeJSONErrorKey(w, r, "auth.invalidSSHKey", http.StatusBadRequest)
			return
		}
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
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
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
		return
	}
	keyIDStr := chi.URLParam(r, "key_id")
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil || keyID <= 0 {
		writeJSONErrorKey(w, r, "auth.sshKeyIDInvalid", http.StatusBadRequest)
		return
	}
	err = a.UserStore.DeletePublicKey(userID, keyID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONErrorKey(w, r, "auth.sshKeyNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
