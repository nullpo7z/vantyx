package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// WebAuthn / passkeys as a second factor next to TOTP. Registration and
// login use the standard ceremonies from go-webauthn; the relying party
// ID defaults to the request host (override with VANTYX_WEBAUTHN_RP_ID /
// VANTYX_WEBAUTHN_ORIGINS behind a reverse proxy). Passkeys are never a
// sole factor here: the password (or OIDC) comes first, then the
// authenticator, exactly like TOTP.

const (
	webauthnEnvRPID      = "VANTYX_WEBAUTHN_RP_ID"
	webauthnEnvOrigins   = "VANTYX_WEBAUTHN_ORIGINS"
	webauthnEnvRPName    = "VANTYX_WEBAUTHN_RP_NAME"
	webauthnRegisterTTL  = 5 * time.Minute
	webauthnMaxNameLen   = 100
	webauthnMaxCachedRPs = 16
	webauthnDefaultRPNam = "Vantyx"
)

// errWebAuthnConfig marks a relying-party configuration problem (e.g. the
// UI is reached by IP address), which handlers report as 400 with a hint
// rather than as an internal error.
var errWebAuthnConfig = errors.New("webauthn relying party unavailable")

// writeWebAuthnError maps configuration problems to a helpful 400.
func writeWebAuthnError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errWebAuthnConfig) {
		writeJSONErrorKey(w, r, "auth.webauthnUnavailable", http.StatusBadRequest)
		return
	}
	writeInternalError(w, err)
}

// webauthnUser adapts a Vantyx user to webauthn.User.
type webauthnUser struct {
	user  *auth.User
	creds []auth.WebAuthnCredential
}

func (u *webauthnUser) WebAuthnID() []byte          { return []byte(u.user.ID) }
func (u *webauthnUser) WebAuthnName() string        { return u.user.Username }
func (u *webauthnUser) WebAuthnDisplayName() string { return u.user.Username }
func (u *webauthnUser) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.creds))
	for _, c := range u.creds {
		transports := make([]protocol.AuthenticatorTransport, 0, len(c.Transports))
		for _, t := range c.Transports {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
		out = append(out, webauthn.Credential{
			ID:              c.ID,
			PublicKey:       c.PublicKey,
			AttestationType: c.AttestationType,
			Transport:       transports,
			Flags:           webauthn.CredentialFlags{BackupEligible: c.BackupEligible, BackupState: c.BackedUp},
			Authenticator:   webauthn.Authenticator{AAGUID: c.AAGUID, SignCount: c.SignCount},
		})
	}
	return out
}

// webauthnRegistrations holds in-flight registration ceremonies per user.
type webauthnRegistrations struct {
	mu    sync.Mutex
	items map[string]webauthnPendingReg
}

type webauthnPendingReg struct {
	session webauthn.SessionData
	expires time.Time
}

func (s *webauthnRegistrations) put(userID string, sd webauthn.SessionData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string]webauthnPendingReg{}
	}
	now := time.Now()
	for k, v := range s.items {
		if now.After(v.expires) {
			delete(s.items, k)
		}
	}
	s.items[userID] = webauthnPendingReg{session: sd, expires: now.Add(webauthnRegisterTTL)}
}

func (s *webauthnRegistrations) pop(userID string) (webauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[userID]
	delete(s.items, userID)
	if !ok || time.Now().After(v.expires) {
		return webauthn.SessionData{}, false
	}
	return v.session, true
}

// webauthnFor builds (and caches per host) the relying party for the request.
func (a *App) webauthnFor(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	if h, _, ok := strings.Cut(host, ":"); ok && !strings.HasPrefix(host, "[") {
		host = h
	}
	rpID := strings.TrimSpace(os.Getenv(webauthnEnvRPID))
	if rpID == "" {
		rpID = host
	}
	origins := strings.Fields(strings.ReplaceAll(os.Getenv(webauthnEnvOrigins), ",", " "))
	if len(origins) == 0 {
		origins = []string{effectiveScheme(r) + "://" + r.Host}
	}
	name := strings.TrimSpace(os.Getenv(webauthnEnvRPName))
	if name == "" {
		name = webauthnDefaultRPNam
	}
	key := rpID + "|" + strings.Join(origins, ",")
	a.webauthnMu.Lock()
	defer a.webauthnMu.Unlock()
	if a.webauthnRPs == nil {
		a.webauthnRPs = map[string]*webauthn.WebAuthn{}
	}
	if w, ok := a.webauthnRPs[key]; ok {
		return w, nil
	}
	w, err := webauthn.New(&webauthn.Config{RPDisplayName: name, RPID: rpID, RPOrigins: origins})
	if err != nil {
		// Typically an IP address or otherwise invalid RP ID: WebAuthn needs
		// a DNS name, which operators supply via VANTYX_WEBAUTHN_RP_ID.
		slog.Warn("webauthn unavailable for this host", "rp_id", rpID, "origins", origins, "error", err)
		return nil, fmt.Errorf("%w: %v", errWebAuthnConfig, err)
	}
	// The key derives from the request Host when no RP ID is configured,
	// so cap the cache instead of letting arbitrary Host headers grow it.
	if len(a.webauthnRPs) >= webauthnMaxCachedRPs {
		a.webauthnRPs = map[string]*webauthn.WebAuthn{}
	}
	a.webauthnRPs[key] = w
	return w, nil
}

func (a *App) webauthnUserFor(r *http.Request, userID string) (*webauthnUser, error) {
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		return nil, auth.ErrUserNotFound
	}
	creds, err := a.WebAuthn.ListForUser(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	return &webauthnUser{user: u, creds: creds}, nil
}

// hasPasskeys reports whether the user registered at least one passkey.
func (a *App) hasPasskeys(r *http.Request, userID string) bool {
	if a.WebAuthn == nil {
		return false
	}
	n, err := a.WebAuthn.Count(r.Context(), userID)
	return err == nil && n > 0
}

/* -------------------------- account management ----------------------- */

type passkeyResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt string   `json:"last_used_at,omitempty"`
	Transports []string `json:"transports"`
	BackedUp   bool     `json:"backed_up"`
}

func (a *App) passkeyToResponse(c auth.WebAuthnCredential) passkeyResponse {
	loc := a.serverLocation()
	out := passkeyResponse{ID: c.IDString(), Name: c.Name, CreatedAt: c.CreatedAt.In(loc).Format(time.RFC3339), Transports: c.Transports, BackedUp: c.BackedUp}
	if out.Transports == nil {
		out.Transports = []string{}
	}
	if c.LastUsedAt != nil {
		out.LastUsedAt = c.LastUsedAt.In(loc).Format(time.RFC3339)
	}
	return out
}

// handleListPasskeys: GET /api/me/webauthn.
func (a *App) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.sessionUserID(w, r)
	if !ok {
		return
	}
	creds, err := a.WebAuthn.ListForUser(r.Context(), userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]passkeyResponse, 0, len(creds))
	for _, c := range creds {
		out = append(out, a.passkeyToResponse(c))
	}
	writeJSON(w, out)
}

// handlePasskeyRegisterBegin: POST /api/me/webauthn/register/begin ->
// PublicKeyCredentialCreationOptions for navigator.credentials.create().
func (a *App) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.sessionUserID(w, r)
	if !ok {
		return
	}
	rp, err := a.webauthnFor(r)
	if err != nil {
		writeWebAuthnError(w, r, err)
		return
	}
	wu, err := a.webauthnUserFor(r, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(wu.creds))
	for _, c := range wu.WebAuthnCredentials() {
		exclude = append(exclude, c.Descriptor())
	}
	options, session, err := rp.BeginRegistration(wu,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithExclusions(exclude),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	a.webauthnRegs.put(userID, *session)
	writeJSON(w, options)
}

// handlePasskeyRegisterFinish: POST /api/me/webauthn/register/finish
// {name, credential}.
func (a *App) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.sessionUserID(w, r)
	if !ok {
		return
	}
	var body struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil || len(body.Credential) == 0 {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > webauthnMaxNameLen {
		name = name[:webauthnMaxNameLen]
	}
	session, ok := a.webauthnRegs.pop(userID)
	if !ok {
		writeJSONErrorKey(w, r, "auth.webauthnNoPending", http.StatusBadRequest)
		return
	}
	rp, err := a.webauthnFor(r)
	if err != nil {
		writeWebAuthnError(w, r, err)
		return
	}
	wu, err := a.webauthnUserFor(r, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body.Credential))
	if err != nil {
		writeJSONErrorKey(w, r, "auth.webauthnInvalid", http.StatusBadRequest)
		return
	}
	cred, err := rp.CreateCredential(wu, session, parsed)
	if err != nil {
		audit("passkey_register_failed", auditFields{"user_id": userID, "error": err.Error()})
		writeJSONErrorKey(w, r, "auth.webauthnInvalid", http.StatusBadRequest)
		return
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	stored := auth.WebAuthnCredential{
		ID: cred.ID, UserID: userID, Name: name, PublicKey: cred.PublicKey,
		AttestationType: cred.AttestationType, AAGUID: cred.Authenticator.AAGUID,
		SignCount: cred.Authenticator.SignCount, Transports: transports,
		BackupEligible: cred.Flags.BackupEligible, BackedUp: cred.Flags.BackupState,
		CreatedAt: time.Now().UTC(),
	}
	if err := a.WebAuthn.Add(r.Context(), stored); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	audit("passkey_registered", auditFields{"user_id": userID, "credential_id": stored.IDString(), "name": name, "aaguid": base64.RawURLEncoding.EncodeToString(cred.Authenticator.AAGUID)})
	writeJSONStatus(w, http.StatusCreated, a.passkeyToResponse(stored))
}

// handlePasskeyDelete: DELETE /api/me/webauthn/{id}.
func (a *App) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.sessionUserID(w, r)
	if !ok {
		return
	}
	id, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil || len(id) == 0 {
		writeJSONErrorKey(w, r, "auth.webauthnNotFound", http.StatusNotFound)
		return
	}
	if err := a.WebAuthn.Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, auth.ErrWebAuthnNotFound) {
			writeJSONErrorKey(w, r, "auth.webauthnNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("passkey_deleted", auditFields{"user_id": userID, "credential_id": base64.RawURLEncoding.EncodeToString(id)})
	w.WriteHeader(http.StatusNoContent)
}

/* ------------------------------- login -------------------------------- */

// handleLoginWebAuthnBegin: POST /api/login/webauthn/begin {mfa_token} ->
// PublicKeyCredentialRequestOptions for navigator.credentials.get().
func (a *App) handleLoginWebAuthnBegin(w http.ResponseWriter, r *http.Request) {
	if !sameOriginRequest(r) {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		MFAToken string `json:"mfa_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if a.mfaPending == nil || a.WebAuthn == nil {
		writeJSONErrorKey(w, r, "auth.mfaTokenInvalid", http.StatusUnauthorized)
		return
	}
	p, ok := a.mfaPending.get(strings.TrimSpace(req.MFAToken))
	if !ok {
		writeJSONErrorKey(w, r, "auth.mfaTokenInvalid", http.StatusUnauthorized)
		return
	}
	rp, err := a.webauthnFor(r)
	if err != nil {
		writeWebAuthnError(w, r, err)
		return
	}
	wu, err := a.webauthnUserFor(r, p.userID)
	if err != nil || len(wu.creds) == 0 {
		writeJSONErrorKey(w, r, "auth.webauthnNotEnabled", http.StatusBadRequest)
		return
	}
	options, session, err := rp.BeginLogin(wu, webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	a.mfaPending.setWebAuthnSession(req.MFAToken, session)
	writeJSON(w, options)
}

// handleLoginWebAuthnFinish: POST /api/login/webauthn/finish
// {mfa_token, credential} completes the login.
func (a *App) handleLoginWebAuthnFinish(w http.ResponseWriter, r *http.Request) {
	if !sameOriginRequest(r) {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		MFAToken   string          `json:"mfa_token"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || len(req.Credential) == 0 {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if a.mfaPending == nil || a.WebAuthn == nil {
		writeJSONErrorKey(w, r, "auth.mfaTokenInvalid", http.StatusUnauthorized)
		return
	}
	tok := strings.TrimSpace(req.MFAToken)
	p, ok := a.mfaPending.get(tok)
	if !ok || p.webauthn == nil {
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
	rp, err := a.webauthnFor(r)
	if err != nil {
		writeWebAuthnError(w, r, err)
		return
	}
	wu, err := a.webauthnUserFor(r, p.userID)
	if err != nil {
		writeJSONErrorKey(w, r, "auth.invalidCredentials", http.StatusUnauthorized)
		return
	}
	fail := func(reason string) {
		failures := a.mfaPending.fail(tok)
		if a.LoginRateLimiter != nil {
			if ip != "" {
				a.LoginRateLimiter.recordFailureIP(ip)
			}
			a.LoginRateLimiter.recordFailureUser(p.username)
		}
		audit("login_webauthn_failed", auditFields{"user_id": p.userID, "reason": reason, "failures": failures})
		writeJSONErrorKey(w, r, "auth.webauthnInvalid", http.StatusUnauthorized)
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		fail("parse")
		return
	}
	cred, err := rp.ValidateLogin(wu, *p.webauthn, parsed)
	if err != nil {
		fail("verify")
		return
	}
	if cred.Authenticator.CloneWarning {
		audit("passkey_clone_warning", auditFields{"user_id": p.userID, "credential_id": base64.RawURLEncoding.EncodeToString(cred.ID)})
	}
	_ = a.WebAuthn.UpdateSignCount(r.Context(), cred.ID, cred.Authenticator.SignCount, cred.Flags.BackupState)
	a.mfaPending.consume(tok)
	if a.LoginRateLimiter != nil {
		a.LoginRateLimiter.recordSuccess(ip, p.username)
	}
	audit("login_webauthn_ok", auditFields{"user_id": p.userID, "credential_id": base64.RawURLEncoding.EncodeToString(cred.ID)})
	a.finishLogin(w, r, wu.user, "login_ok")
}
