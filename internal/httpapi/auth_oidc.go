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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/nullpo7z/vantyx/internal/access"
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
//	VANTYX_OIDC_LINK_EXISTING_USERS (optional; 1/true to attach unlinked IdP identities to the local
//	                               non-admin user with the same username -- off by default)
//	VANTYX_OIDC_DISPLAY_NAME      (optional; button label, default "SSO")
//	VANTYX_OIDC_GROUPS_CLAIM      (optional; claim holding the IdP groups, default "groups")
//	VANTYX_OIDC_GROUP_MAP         (optional; "idpGroup=vantyxGroup,idpGroup2=net/tokyo,..." -- memberships
//	                               synced on every login; groups the IdP stops sending are revoked)
//	VANTYX_OIDC_ADMIN_GROUPS      (optional; "idpGroup,idpGroup2" -- members get role admin, others user)
type oidcConfig struct {
	Issuer        string
	DiscoveryURL  string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	UsernameClaim string
	AutoCreate    bool
	// LinkExisting allows an unlinked IdP identity to attach itself to
	// a local account whose username equals the username claim. Off by
	// default: with it on, whoever controls that claim at the IdP can
	// sign in as that local user, so it is only safe when the IdP is the
	// sole source of truth for usernames. Admin accounts are never linked
	// this way.
	LinkExisting bool
	DisplayName  string
	GroupsClaim  string
	// GroupMap: IdP group -> Vantyx group IDs it grants.
	GroupMap map[string][]string
	// AdminGroups: IdP groups whose members get role admin. When set,
	// OIDC users outside them are kept at role user.
	AdminGroups map[string]bool
}

// syncsGroups reports whether logins should reconcile memberships/role.
func (c *oidcConfig) syncsGroups() bool {
	return c != nil && (len(c.GroupMap) > 0 || len(c.AdminGroups) > 0)
}

// parseOIDCGroupMap parses "a=g1,a=g2,b=net/tokyo" (also ";" separated).
func parseOIDCGroupMap(raw string) map[string][]string {
	out := map[string][]string{}
	for _, pair := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		k, v, ok := strings.Cut(pair, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			continue
		}
		out[k] = append(out[k], v)
	}
	return out
}

func parseOIDCList(raw string) map[string]bool {
	out := map[string]bool{}
	for _, v := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if v = strings.TrimSpace(v); v != "" {
			out[v] = true
		}
	}
	return out
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
	cfg.GroupsClaim = strings.TrimSpace(os.Getenv("VANTYX_OIDC_GROUPS_CLAIM"))
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	cfg.GroupMap = parseOIDCGroupMap(os.Getenv("VANTYX_OIDC_GROUP_MAP"))
	cfg.AdminGroups = parseOIDCList(os.Getenv("VANTYX_OIDC_ADMIN_GROUPS"))
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VANTYX_OIDC_AUTO_CREATE_USERS"))) {
	case "1", "true", "yes":
		cfg.AutoCreate = true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VANTYX_OIDC_LINK_EXISTING_USERS"))) {
	case "1", "true", "yes":
		cfg.LinkExisting = true
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
	// #nosec G710 -- authURL is the IdP's discovered authorization endpoint
	// (from the operator-configured issuer), not request input.
	http.Redirect(w, r, authURL, http.StatusFound)
}

// clearOIDCStateCookie expires the login-state cookie with the same
// attributes it was issued with: browsers refuse to replace a Secure
// cookie with a non-Secure one, so a bare deletion cookie could leave the
// old state behind.
func clearOIDCStateCookie(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- Secure is set from the request scheme at runtime; the
	// static check can not see the assignment (same as setSessionCookie).
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: cookieSecure(r),
	})
}

// oidcFail sends the browser back to the login page with a short error
// code the SPA can localize (never the raw upstream error).
func (a *App) oidcFail(w http.ResponseWriter, r *http.Request, code string) {
	clearOIDCStateCookie(w, r)
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
	if u.Disabled {
		audit("oidc_login_failed", auditFields{"reason": "account_disabled", "user_id": u.ID})
		a.oidcFail(w, r, "disabled")
		return
	}
	if a.oidc.cfg.syncsGroups() {
		if err := a.syncOIDCGroups(ctx, u, rawClaims); err != nil {
			audit("oidc_login_failed", auditFields{"reason": "group_sync", "user_id": u.ID, "error": err.Error()})
			a.oidcFail(w, r, "resolve")
			return
		}
	}
	sess, err := a.SessionStore.Create(u.ID)
	if err != nil {
		audit("oidc_login_failed", auditFields{"reason": "session_create_failed", "error": err.Error()})
		a.oidcFail(w, r, "session")
		return
	}
	setSessionCookie(w, r, sess.ID)
	clearOIDCStateCookie(w, r)
	audit("oidc_login_ok", auditFields{
		"user_id":      u.ID,
		"issuer":       idToken.Issuer,
		"subject":      claims.Subject,
		"user_created": created,
	})
	http.Redirect(w, r, pending.next, http.StatusFound)
}

var errOIDCNotProvisioned = errors.New("oidc identity is not linked to a local user")

// oidcClaimGroups extracts the IdP group names from the configured claim.
// Accepts an array of strings, a single string, or objects carrying
// "name" / "id" (some IdPs emit the latter).
func oidcClaimGroups(raw map[string]interface{}, claim string) []string {
	var out []string
	add := func(v interface{}) {
		switch x := v.(type) {
		case string:
			if x = strings.TrimSpace(x); x != "" {
				out = append(out, x)
			}
		case map[string]interface{}:
			for _, k := range []string{"name", "id", "displayName"} {
				if s, ok := x[k].(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
					return
				}
			}
		}
	}
	switch v := raw[claim].(type) {
	case []interface{}:
		for _, e := range v {
			add(e)
		}
	case string, map[string]interface{}:
		add(v)
	}
	return out
}

// syncOIDCGroups reconciles the user's Vantyx memberships and role with
// the IdP's groups claim:
//   - every mapped Vantyx group present in the claim is granted;
//   - groups OIDC granted on an earlier login but not carried now are
//     revoked (memberships an admin added by hand are never touched);
//   - with VANTYX_OIDC_ADMIN_GROUPS set, the role follows the claim
//     (never demoting the last remaining admin).
func (a *App) syncOIDCGroups(ctx context.Context, u *auth.User, raw map[string]interface{}) error {
	cfg := a.oidc.cfg
	idpGroups := oidcClaimGroups(raw, cfg.GroupsClaim)
	desired := map[string]bool{}
	var unknown []string
	for _, g := range idpGroups {
		for _, target := range cfg.GroupMap[g] {
			if _, err := a.AccessGroupStore.Get(ctx, access.GroupID(target)); err != nil {
				unknown = append(unknown, target)
				continue
			}
			desired[target] = true
		}
	}
	previous, err := a.OIDCLinks.ManagedGroups(ctx, u.ID)
	if err != nil {
		return err
	}
	prevSet := map[string]bool{}
	for _, g := range previous {
		prevSet[g] = true
	}
	var added, removed []string
	for g := range desired {
		if !prevSet[g] {
			if err := a.AccessGroupStore.AddUserToGroup(ctx, access.UserID(u.ID), access.GroupID(g)); err != nil {
				return fmt.Errorf("add %s to %s: %w", u.ID, g, err)
			}
			added = append(added, g)
		}
	}
	for _, g := range previous {
		if !desired[g] {
			if err := a.AccessGroupStore.RemoveUserFromGroup(ctx, access.UserID(u.ID), access.GroupID(g)); err != nil && !errors.Is(err, access.ErrGroupNotFound) {
				return fmt.Errorf("remove %s from %s: %w", u.ID, g, err)
			}
			removed = append(removed, g)
		}
	}
	managed := make([]string, 0, len(desired))
	for g := range desired {
		managed = append(managed, g)
	}
	sort.Strings(managed)
	sort.Strings(added)
	sort.Strings(removed)
	if err := a.OIDCLinks.SetManagedGroups(ctx, u.ID, managed); err != nil {
		return err
	}

	roleNote := ""
	if len(cfg.AdminGroups) > 0 {
		wantAdmin := false
		for _, g := range idpGroups {
			if cfg.AdminGroups[g] {
				wantAdmin = true
				break
			}
		}
		current := u.Role
		if current == "" {
			current = auth.RoleUser
		}
		want := auth.RoleUser
		if wantAdmin {
			want = auth.RoleAdmin
		}
		if want != current {
			if want == auth.RoleUser {
				if n, err := a.countAdmins(); err == nil && n <= 1 {
					roleNote = "kept_last_admin"
					want = current
				}
			}
			if want != current {
				if err := a.UserStore.UpdateRole(u.ID, want); err != nil {
					return fmt.Errorf("update role: %w", err)
				}
				u.Role = want
				roleNote = current + "->" + want
			}
		}
	}
	if len(added) > 0 || len(removed) > 0 || roleNote != "" || len(unknown) > 0 {
		audit("oidc_groups_synced", auditFields{
			"user_id":        u.ID,
			"idp_groups":     idpGroups,
			"added":          added,
			"removed":        removed,
			"role":           roleNote,
			"unknown_groups": unknown,
		})
	}
	return nil
}

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
	fromEmail := a.oidc.cfg.UsernameClaim == "email"
	if v, ok := raw[a.oidc.cfg.UsernameClaim].(string); ok {
		username = strings.TrimSpace(v)
	}
	if username == "" {
		username = strings.TrimSpace(claims.PreferredUsername)
		fromEmail = false
	}
	if username == "" {
		username = strings.TrimSpace(claims.Email)
		fromEmail = true
	}
	if username == "" {
		return nil, false, errOIDCNotProvisioned
	}
	// An identity that is not linked yet may attach itself to an existing
	// local account only when the operator opted in, the account is not
	// an admin, and (for email-derived usernames) the IdP did not flag the
	// address as unverified. Otherwise the match is refused rather than
	// silently creating a second account with a clashing name.
	users, err := a.UserStore.ListUsers(1000, 0)
	if err != nil {
		return nil, false, err
	}
	for _, u := range users {
		if u == nil || !strings.EqualFold(u.Username, username) {
			continue
		}
		refuse := ""
		switch {
		case !a.oidc.cfg.LinkExisting:
			refuse = "link_disabled"
		case u.Role == auth.RoleAdmin:
			refuse = "admin_account"
		case fromEmail && emailUnverified(raw):
			refuse = "email_unverified"
		}
		if refuse != "" {
			audit("oidc_link_refused", auditFields{"user_id": u.ID, "issuer": issuer, "subject": claims.Subject, "reason": refuse})
			return nil, false, errOIDCNotProvisioned
		}
		if a.OIDCLinks != nil {
			if err := a.OIDCLinks.Link(ctx, issuer, claims.Subject, u.ID); err != nil {
				return nil, false, err
			}
		}
		audit("oidc_user_linked", auditFields{"user_id": u.ID, "issuer": issuer, "subject": claims.Subject})
		return u, false, nil
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

// emailUnverified reports whether the ID token explicitly says the email
// address is not verified (absent claim = no statement = allowed).
func emailUnverified(raw map[string]interface{}) bool {
	switch v := raw["email_verified"].(type) {
	case bool:
		return !v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "false")
	}
	return false
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
