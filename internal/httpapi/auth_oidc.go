package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// oidcConfig is the OpenID Connect relying-party configuration, read
// from the environment at start-up. Login is enabled when Issuer and
// ClientID are both set.
//
//	VANTYX_OIDC_ISSUER            https://idp.example.com/realms/x (expected `iss`; also the discovery base URL)
//	VANTYX_OIDC_DISCOVERY_URL     (optional; base URL whose /.well-known/openid-configuration to fetch when it
//	                               differs from the issuer, e.g. Cloudflare Access SaaS apps)
//	VANTYX_OIDC_CLIENT_ID
//	VANTYX_OIDC_CLIENT_SECRET     (optional for public clients; PKCE is always used)
//	VANTYX_OIDC_REDIRECT_URL      (optional; default <scheme>://<host>/api/auth/oidc/callback)
//	VANTYX_OIDC_SCOPES            (optional; default "openid profile email")
//	VANTYX_OIDC_USERNAME_CLAIM    (optional; default "preferred_username", falls back to email)
//	VANTYX_OIDC_AUTO_CREATE_USERS (optional; 1/true to create unknown users with role user)
//	VANTYX_OIDC_DISPLAY_NAME      (optional; button label, default "SSO")
type oidcConfig struct {
	Issuer        string
	DiscoveryURL  string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	UsernameClaim string
	AutoCreate    bool
	DisplayName   string
}

func oidcConfigFromEnv() *oidcConfig {
	issuer := strings.TrimSpace(os.Getenv("VANTYX_OIDC_ISSUER"))
	clientID := strings.TrimSpace(os.Getenv("VANTYX_OIDC_CLIENT_ID"))
	if issuer == "" || clientID == "" {
		return nil
	}
	cfg := &oidcConfig{
		Issuer:        issuer,
		DiscoveryURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("VANTYX_OIDC_DISCOVERY_URL")), "/"),
		ClientID:      clientID,
		ClientSecret:  strings.TrimSpace(os.Getenv("VANTYX_OIDC_CLIENT_SECRET")),
		RedirectURL:   strings.TrimSpace(os.Getenv("VANTYX_OIDC_REDIRECT_URL")),
		UsernameClaim: strings.TrimSpace(os.Getenv("VANTYX_OIDC_USERNAME_CLAIM")),
		DisplayName:   strings.TrimSpace(os.Getenv("VANTYX_OIDC_DISPLAY_NAME")),
	}
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "preferred_username"
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "SSO"
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VANTYX_OIDC_AUTO_CREATE_USERS"))) {
	case "1", "true", "yes":
		cfg.AutoCreate = true
	}
	scopes := strings.Fields(strings.ReplaceAll(os.Getenv("VANTYX_OIDC_SCOPES"), ",", " "))
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	hasOpenID := false
	for _, s := range scopes {
		if s == oidc.ScopeOpenID {
			hasOpenID = true
		}
	}
	if !hasOpenID {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	cfg.Scopes = scopes
	return cfg
}

// oidcStateTTL bounds how long a login started at /api/auth/oidc/login may
// take to come back through the callback.
const oidcStateTTL = 10 * time.Minute

const oidcStateCookie = "vantyx_oidc_state"

type oidcPending struct {
	nonce    string
	verifier string
	next     string
	expires  time.Time
}

// oidcService holds the lazily-initialised provider (discovery happens on
// first use so an unreachable IdP does not stop the server from
// starting) and the in-flight login states.
type oidcService struct {
	cfg *oidcConfig

	mu       sync.Mutex
	provider *oidc.Provider
	states   map[string]*oidcPending
}

func newOIDCServiceFromEnv() *oidcService {
	cfg := oidcConfigFromEnv()
	if cfg == nil {
		return nil
	}
	return &oidcService{cfg: cfg, states: make(map[string]*oidcPending)}
}

func (s *oidcService) providerFor(ctx context.Context) (*oidc.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != nil {
		return s.provider, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	discovery := s.cfg.Issuer
	if s.cfg.DiscoveryURL != "" && s.cfg.DiscoveryURL != s.cfg.Issuer {
		// Some providers (Cloudflare Access SaaS apps, some Azure AD
		// set-ups) publish the discovery document under a path that is
		// not the `iss` they put in ID tokens. Fetch from the configured
		// URL but keep verifying tokens against the configured issuer.
		discovery = s.cfg.DiscoveryURL
		ctx = oidc.InsecureIssuerURLContext(ctx, s.cfg.Issuer)
	}
	p, err := oidc.NewProvider(ctx, discovery)
	if err != nil {
		return nil, err
	}
	s.provider = p
	return p, nil
}

func (s *oidcService) oauth2Config(p *oidc.Provider, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.cfg.ClientID,
		ClientSecret: s.cfg.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       s.cfg.Scopes,
	}
}

func (s *oidcService) putState(state string, p *oidcPending) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.states {
		if now.After(v.expires) {
			delete(s.states, k)
		}
	}
	s.states[state] = p
}

func (s *oidcService) popState(state string) (*oidcPending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.states[state]
	if ok {
		delete(s.states, state)
	}
	if !ok || time.Now().After(p.expires) {
		return nil, false
	}
	return p, true
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// oidcRedirectURL returns the configured callback URL or derives it from
// the request (scheme from TLS / X-Forwarded-Proto, host from Host).
func (a *App) oidcRedirectURL(r *http.Request) string {
	if a.oidc != nil && a.oidc.cfg.RedirectURL != "" {
		return a.oidc.cfg.RedirectURL
	}
	scheme := "https"
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/api/auth/oidc/callback"
}

// safeNextPath only accepts same-origin absolute paths for post-login
// redirects (no scheme, no protocol-relative "//host").
func safeNextPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "/"
	}
	if u, err := url.Parse(raw); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return raw
}

// handleAuthMethods: GET /api/auth/methods (public) tells the login page
// which sign-in options to offer.
func (a *App) handleAuthMethods(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]interface{}{
		"password": true,
		"oidc":     map[string]interface{}{"enabled": false},
	}
	if a.oidc != nil {
		resp["oidc"] = map[string]interface{}{
			"enabled":      true,
			"display_name": a.oidc.cfg.DisplayName,
		}
	}
	writeJSON(w, resp)
}

// handleOIDCLogin: GET /api/auth/oidc/login[?next=/path] starts the
// Authorization Code + PKCE flow.
func (a *App) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		writeJSONErrorKey(w, r, "auth.oidcDisabled", http.StatusNotFound)
		return
	}
	provider, err := a.oidc.providerFor(r.Context())
	if err != nil {
		audit("oidc_login_failed", auditFields{"reason": "discovery", "error": err.Error()})
		a.oidcFail(w, r, "provider_unavailable")
		return
	}
	state, err := randomToken()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	verifier := oauth2.GenerateVerifier()
	a.oidc.putState(state, &oidcPending{
		nonce:    nonce,
		verifier: verifier,
		next:     safeNextPath(r.URL.Query().Get("next")),
		expires:  time.Now().Add(oidcStateTTL),
	})
	// Lax (not Strict): the callback is a top-level navigation *from* the
	// IdP, and a Strict cookie would not be sent on it.
	// #nosec G124 -- Secure follows the request scheme at runtime.
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     "/api/auth/oidc",
		MaxAge:   int(oidcStateTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieSecure(r),
	})
	authURL := a.oidc.oauth2Config(provider, a.oidcRedirectURL(r)).AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	audit("oidc_login_started", auditFields{"issuer": a.oidc.cfg.Issuer})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// oidcFail sends the browser back to the login page with a short error
// code the SPA can localize (never the raw upstream error).
func (a *App) oidcFail(w http.ResponseWriter, r *http.Request, code string) {
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/?oidc_error="+url.QueryEscape(code), http.StatusFound)
}

type oidcClaims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	// Custom username claim (VANTYX_OIDC_USERNAME_CLAIM) is read via the
	// raw map below when it is not one of the standard fields.
}

// handleOIDCCallback: GET /api/auth/oidc/callback?code&state completes
// the flow, maps the identity to a local user and issues the session.
func (a *App) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		writeJSONErrorKey(w, r, "auth.oidcDisabled", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		audit("oidc_login_failed", auditFields{"reason": "idp_error", "error": e})
		a.oidcFail(w, r, "denied")
		return
	}
	state := q.Get("state")
	cookie, err := r.Cookie(oidcStateCookie)
	if err != nil || cookie.Value == "" || state == "" || cookie.Value != state {
		audit("oidc_login_failed", auditFields{"reason": "state_mismatch"})
		a.oidcFail(w, r, "state")
		return
	}
	pending, ok := a.oidc.popState(state)
	if !ok {
		audit("oidc_login_failed", auditFields{"reason": "state_expired"})
		a.oidcFail(w, r, "state")
		return
	}
	provider, err := a.oidc.providerFor(r.Context())
	if err != nil {
		audit("oidc_login_failed", auditFields{"reason": "discovery", "error": err.Error()})
		a.oidcFail(w, r, "provider_unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, err := a.oidc.oauth2Config(provider, a.oidcRedirectURL(r)).Exchange(ctx, q.Get("code"), oauth2.VerifierOption(pending.verifier))
	if err != nil {
		audit("oidc_login_failed", auditFields{"reason": "exchange", "error": err.Error()})
		a.oidcFail(w, r, "exchange")
		return
	}
	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		audit("oidc_login_failed", auditFields{"reason": "no_id_token"})
		a.oidcFail(w, r, "exchange")
		return
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: a.oidc.cfg.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		audit("oidc_login_failed", auditFields{"reason": "id_token_invalid", "error": err.Error()})
		a.oidcFail(w, r, "token")
		return
	}
	if idToken.Nonce != pending.nonce {
		audit("oidc_login_failed", auditFields{"reason": "nonce_mismatch"})
		a.oidcFail(w, r, "token")
		return
	}
	var claims oidcClaims
	rawClaims := map[string]interface{}{}
	if err := idToken.Claims(&claims); err != nil {
		audit("oidc_login_failed", auditFields{"reason": "claims", "error": err.Error()})
		a.oidcFail(w, r, "token")
		return
	}
	_ = idToken.Claims(&rawClaims)
	if claims.Subject == "" {
		claims.Subject = idToken.Subject
	}

	u, created, err := a.resolveOIDCUser(ctx, idToken.Issuer, claims, rawClaims)
	if err != nil {
		reason := "not_provisioned"
		if !errors.Is(err, errOIDCNotProvisioned) {
			reason = "resolve"
		}
		audit("oidc_login_failed", auditFields{
			"reason":      reason,
			"issuer":      idToken.Issuer,
			"subject":     claims.Subject,
			"error":       err.Error(),
			"auto_create": a.oidc.cfg.AutoCreate,
		})
		a.oidcFail(w, r, reason)
		return
	}
	sess, err := a.SessionStore.Create(u.ID)
	if err != nil {
		audit("oidc_login_failed", auditFields{"reason": "session_create_failed", "error": err.Error()})
		a.oidcFail(w, r, "session")
		return
	}
	setSessionCookie(w, r, sess.ID)
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1, HttpOnly: true})
	audit("oidc_login_ok", auditFields{
		"user_id":      u.ID,
		"issuer":       idToken.Issuer,
		"subject":      claims.Subject,
		"user_created": created,
	})
	http.Redirect(w, r, pending.next, http.StatusFound)
}

var errOIDCNotProvisioned = errors.New("oidc identity is not linked to a local user")

// resolveOIDCUser maps a verified identity to a local user:
//  1. an existing link (issuer, subject) wins;
//  2. otherwise a local user whose username equals the configured
//     username claim (or email) is linked on first login;
//  3. otherwise, when auto-create is enabled, a new role=user account is
//     created (with an unusable random password) and linked.
func (a *App) resolveOIDCUser(ctx context.Context, issuer string, claims oidcClaims, raw map[string]interface{}) (*auth.User, bool, error) {
	if a.OIDCLinks != nil {
		if uid, err := a.OIDCLinks.Lookup(ctx, issuer, claims.Subject); err == nil {
			if u, gerr := a.UserStore.GetByID(uid); gerr == nil && u != nil {
				return u, false, nil
			}
		} else if !errors.Is(err, auth.ErrOIDCLinkNotFound) {
			return nil, false, err
		}
	}
	username := ""
	if v, ok := raw[a.oidc.cfg.UsernameClaim].(string); ok {
		username = strings.TrimSpace(v)
	}
	if username == "" {
		username = strings.TrimSpace(claims.PreferredUsername)
	}
	if username == "" {
		username = strings.TrimSpace(claims.Email)
	}
	if username == "" {
		return nil, false, errOIDCNotProvisioned
	}
	// Match an existing local account by username (case-insensitive).
	users, err := a.UserStore.ListUsers(1000, 0)
	if err != nil {
		return nil, false, err
	}
	for _, u := range users {
		if u != nil && strings.EqualFold(u.Username, username) {
			if a.OIDCLinks != nil {
				if err := a.OIDCLinks.Link(ctx, issuer, claims.Subject, u.ID); err != nil {
					return nil, false, err
				}
			}
			return u, false, nil
		}
	}
	if !a.oidc.cfg.AutoCreate {
		return nil, false, errOIDCNotProvisioned
	}
	pw, err := randomUnusablePassword()
	if err != nil {
		return nil, false, err
	}
	id := slugID(username)
	u, err := a.UserStore.CreateUser(id, username, pw, auth.RoleUser)
	if err != nil {
		return nil, false, fmt.Errorf("auto-create user %q: %w", username, err)
	}
	if a.OIDCLinks != nil {
		if err := a.OIDCLinks.Link(ctx, issuer, claims.Subject, u.ID); err != nil {
			return nil, false, err
		}
	}
	audit("oidc_user_created", auditFields{"user_id": u.ID, "issuer": issuer})
	return u, true, nil
}

// randomUnusablePassword satisfies the local password policy but is never
// disclosed, so an OIDC-provisioned account cannot be used with password
// login unless an admin sets a password.
func randomUnusablePassword() (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	return "Aa1!" + tok, nil
}
