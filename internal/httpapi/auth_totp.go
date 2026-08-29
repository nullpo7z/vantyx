package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// mfaPendingTTL bounds how long a password-verified login may wait for
// its second factor before the user has to start over.
const mfaPendingTTL = 5 * time.Minute

// mfaPending is a login that passed the password check and now awaits a
// TOTP / recovery code. It lives only in memory: a restart simply makes
// the user log in again.
type mfaPending struct {
	userID   string
	username string
	ip       string
	expires  time.Time
	failures int
}

type mfaPendingStore struct {
	mu    sync.Mutex
	items map[string]*mfaPending
}

func newMFAPendingStore() *mfaPendingStore {
	return &mfaPendingStore{items: make(map[string]*mfaPending)}
}

func (s *mfaPendingStore) issue(userID, username, ip string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, p := range s.items { // opportunistic GC
		if now.After(p.expires) {
			delete(s.items, k)
		}
	}
	s.items[tok] = &mfaPending{userID: userID, username: username, ip: ip, expires: now.Add(mfaPendingTTL)}
	return tok, nil
}

func (s *mfaPendingStore) get(tok string) (*mfaPending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.items[tok]
	if !ok || time.Now().After(p.expires) {
		delete(s.items, tok)
		return nil, false
	}
	return p, true
}

func (s *mfaPendingStore) consume(tok string) {
	s.mu.Lock()
	delete(s.items, tok)
	s.mu.Unlock()
}

// mfaMaxFailures is how many wrong codes a single pending login may see
// before the challenge is discarded (on top of the per-user limiter).
const mfaMaxFailures = 5

type loginTOTPRequest struct {
	MFAToken string `json:"mfa_token"`
	Code     string `json:"code"`
}

// finishLogin issues the server-side session + cookie and writes the
// login response. Shared by password, TOTP and OIDC logins.
func (a *App) finishLogin(w http.ResponseWriter, r *http.Request, u *auth.User, auditName string) bool {
	sess, err := a.SessionStore.Create(u.ID)
	if err != nil {
		audit("login_failed", auditFields{
			"username_hash": auditUsernameHash(u.Username),
			"reason":        "session_create_failed",
			"error":         err.Error(),
		})
		writeJSONErrorKey(w, r, "auth.sessionCreateFailed", http.StatusInternalServerError)
		return false
	}
	setSessionCookie(w, r, sess.ID)
	if u.Role == "" {
		u.Role = auth.RoleUser
	}
	if auditName != "" {
		audit(auditName, auditFields{"user_id": u.ID})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{
		UserID:                u.ID,
		Username:              u.Username,
		Role:                  u.Role,
		Locale:                u.Locale,
		Timezone:              u.Timezone,
		RequirePasswordChange: u.ForcePasswordChange,
	})
	return true
}

// setSessionCookie writes the vantyx_session cookie with the hardened
// attributes used by every login path (24h to match the session TTL).
func setSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	// #nosec G124 -- Secure is set from the request scheme at runtime; the
	// static check can not see the assignment.
	http.SetCookie(w, &http.Cookie{
		Name:     "vantyx_session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   24 * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   cookieSecure(r),
	})
}

// handleLoginTOTP completes a login whose password was already verified
// (POST /api/login/totp {mfa_token, code}). The code may be a TOTP or an
// unused recovery code.
func (a *App) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	if !sameOriginRequest(r) {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	var req loginTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if a.mfaPending == nil || a.TOTPStore == nil {
		writeJSONErrorKey(w, r, "auth.mfaTokenInvalid", http.StatusUnauthorized)
		return
	}
	p, ok := a.mfaPending.get(strings.TrimSpace(req.MFAToken))
	if !ok {
		writeJSONErrorKey(w, r, "auth.mfaTokenInvalid", http.StatusUnauthorized)
		return
	}
	ip := ""
	if a.LoginRateLimiter != nil {
		ip = a.LoginRateLimiter.clientIP(r)
		if !a.LoginRateLimiter.allowIP(ip) || !a.LoginRateLimiter.allowUser(p.username) {
			writeJSONErrorKey(w, r, "auth.tooManyAttempts", http.StatusTooManyRequests)
			return
		}
	}
	usedRecovery, err := a.TOTPStore.Verify(r.Context(), p.userID, req.Code)
	if err != nil {
		p.failures++
		if p.failures >= mfaMaxFailures {
			a.mfaPending.consume(req.MFAToken)
		}
		if a.LoginRateLimiter != nil {
			if ip != "" {
				a.LoginRateLimiter.recordFailureIP(ip)
			}
			a.LoginRateLimiter.recordFailureUser(p.username)
		}
		audit("login_totp_failed", auditFields{
			"user_id":  p.userID,
			"failures": p.failures,
		})
		writeJSONErrorKey(w, r, "auth.totpInvalidCode", http.StatusUnauthorized)
		return
	}
	a.mfaPending.consume(req.MFAToken)
	if a.LoginRateLimiter != nil {
		a.LoginRateLimiter.recordSuccess(ip, p.username)
	}
	u, err := a.UserStore.GetByID(p.userID)
	if err != nil || u == nil {
		writeJSONErrorKey(w, r, "auth.invalidCredentials", http.StatusUnauthorized)
		return
	}
	audit("login_totp_ok", auditFields{
		"user_id":       u.ID,
		"used_recovery": usedRecovery,
	})
	if usedRecovery {
		if st, serr := a.TOTPStore.Status(r.Context(), u.ID); serr == nil && st.RecoveryCodesLeft == 0 {
			audit("login_totp_recovery_exhausted", auditFields{"user_id": u.ID})
		}
	}
	a.finishLogin(w, r, u, "login_ok")
}

type totpStatusResponse struct {
	Enabled           bool   `json:"enabled"`
	Pending           bool   `json:"pending"`
	RecoveryCodesLeft int    `json:"recovery_codes_left"`
	ConfirmedAt       string `json:"confirmed_at,omitempty"`
}

// handleTOTPStatus: GET /api/me/totp.
func (a *App) handleTOTPStatus(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.TOTPStore == nil {
		writeJSON(w, totpStatusResponse{})
		return
	}
	st, err := a.TOTPStore.Status(r.Context(), userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	resp := totpStatusResponse{Enabled: st.Enabled, Pending: st.Pending, RecoveryCodesLeft: st.RecoveryCodesLeft}
	if !st.ConfirmedAt.IsZero() {
		resp.ConfirmedAt = st.ConfirmedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, resp)
}

// handleTOTPSetup: POST /api/me/totp/setup starts enrolment and returns
// the otpauth URL, the raw secret (for manual entry) and a QR code PNG as
// a data URL. Nothing is enforced until the first code is confirmed.
func (a *App) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.TOTPStore == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}
	username := a.usernameFor(r.Context(), userID)
	otpauthURL, secretB32, err := a.TOTPStore.BeginEnrolment(r.Context(), userID, username)
	if err != nil {
		if errors.Is(err, auth.ErrTOTPAlreadyEnabled) {
			writeJSONErrorKey(w, r, "auth.totpAlreadyEnabled", http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}
	qr, err := qrcode.New(otpauthURL, qrcode.Medium)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, qr.Image(256)); err != nil {
		writeInternalError(w, err)
		return
	}
	audit("totp_enrolment_started", auditFields{"user_id": userID})
	writeJSON(w, map[string]interface{}{
		"otpauth_url": otpauthURL,
		"secret":      secretB32,
		"qr_png":      "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
}

// handleTOTPConfirm: POST /api/me/totp/confirm {code} enables the factor
// and returns the one-time recovery codes.
func (a *App) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.TOTPStore == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	codes, err := a.TOTPStore.Confirm(r.Context(), userID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTOTPInvalidCode):
			writeJSONErrorKey(w, r, "auth.totpInvalidCode", http.StatusBadRequest)
		case errors.Is(err, auth.ErrTOTPNoPending):
			writeJSONErrorKey(w, r, "auth.totpNoPending", http.StatusBadRequest)
		case errors.Is(err, auth.ErrTOTPAlreadyEnabled):
			writeJSONErrorKey(w, r, "auth.totpAlreadyEnabled", http.StatusConflict)
		default:
			writeInternalError(w, err)
		}
		return
	}
	audit("totp_enabled", auditFields{"user_id": userID})
	writeJSON(w, map[string]interface{}{"enabled": true, "recovery_codes": codes})
}

// handleTOTPDisable: DELETE /api/me/totp {password} -- re-authenticates
// with the current password before removing the factor.
func (a *App) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.TOTPStore == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := a.UserStore.Authenticate(u.Username, req.Password); err != nil {
		audit("totp_disable_reauth_failed", auditFields{"user_id": userID})
		writeJSONErrorKey(w, r, "auth.currentPasswordWrong", http.StatusForbidden)
		return
	}
	if err := a.TOTPStore.Disable(r.Context(), userID); err != nil {
		if errors.Is(err, auth.ErrTOTPNotEnabled) {
			writeJSONErrorKey(w, r, "auth.totpNotEnabled", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("totp_disabled", auditFields{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminResetTOTP: DELETE /api/users/{user_id}/totp lets an admin
// clear a locked-out user's second factor.
func (a *App) handleAdminResetTOTP(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	target := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if target == "" {
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
		return
	}
	if a.TOTPStore == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}
	if err := a.TOTPStore.Disable(r.Context(), target); err != nil {
		if errors.Is(err, auth.ErrTOTPNotEnabled) {
			writeJSONErrorKey(w, r, "auth.totpNotEnabled", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("totp_reset_by_admin", auditFields{
		"user_id":   a.currentUserID(r),
		"target_id": target,
	})
	w.WriteHeader(http.StatusNoContent)
}
