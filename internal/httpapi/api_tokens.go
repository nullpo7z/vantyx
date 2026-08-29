package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// Bearer API tokens for automation. apiTokenMiddleware resolves
// `Authorization: Bearer vtx_...` into the owning user (stored in the
// request context, where currentUserIDWithError picks it up before
// looking at the session cookie) and enforces the read/write scope by
// HTTP method. The CSRF Origin middleware runs *before* this one, so a
// bearer request is still held to the Origin/Referer rules (non-browser
// clients send neither and pass; a browser cannot attach the header
// cross-site without CORS anyway); sameOriginRequest's token bypass only
// matters for handlers that re-check inside.
//
// A leaked token must never be convertible into a broader credential, so
// everything that mints or changes credentials is session-only: token
// management, passwords, TOTP / passkeys, the user's CLI SSH keys, user
// administration, database backups (a snapshot contains every hash and
// live session), webhooks (an exfiltration channel) and the OIDC flow.
// See apiTokenPathAllowed.

type apiTokenCtxKey struct{}

type apiTokenAuth struct {
	userID string
	scope  string
	id     string
}

func apiTokenFromContext(r *http.Request) (*apiTokenAuth, bool) {
	v, ok := r.Context().Value(apiTokenCtxKey{}).(*apiTokenAuth)
	return v, ok && v != nil
}

// apiTokenMiddleware authenticates bearer tokens on /api/ paths.
func (a *App) apiTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if a.APITokens == nil || !strings.HasPrefix(h, "Bearer "+auth.APITokenPrefix) || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		plain := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		tok, err := a.APITokens.Authenticate(r.Context(), plain)
		if err != nil {
			audit("api_token_auth_failed", auditFields{"path": r.URL.Path, "remote": r.RemoteAddr})
			writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
			return
		}
		if u, err := a.UserStore.GetByID(tok.UserID); err != nil || u == nil || u.Disabled {
			writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
			return
		}
		if tok.Scope == auth.APITokenScopeRead && r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSONErrorKey(w, r, "auth.apiTokenReadOnly", http.StatusForbidden)
			return
		}
		if !apiTokenPathAllowed(r.Method, r.URL.Path) {
			audit("api_token_denied_path", auditFields{"user_id": tok.UserID, "token_id": tok.ID, "method": r.Method, "path": r.URL.Path})
			writeJSONErrorKey(w, r, "auth.apiTokenNotAllowedHere", http.StatusForbidden)
			return
		}
		_ = a.APITokens.Touch(r.Context(), tok.ID)
		ctx := context.WithValue(r.Context(), apiTokenCtxKey{}, &apiTokenAuth{userID: tok.UserID, scope: tok.Scope, id: tok.ID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// apiTokenSessionOnly lists path prefixes a token may never call.
var apiTokenSessionOnly = []string{
	"/api/me/tokens",
	"/api/me/password",
	"/api/me/totp",
	"/api/me/webauthn",
	"/api/me/ssh-keys",
	"/api/login",
	"/api/logout",
	"/api/auth/oidc",
	"/api/settings/backups",
	"/api/settings/webhooks",
}

// apiTokenReadOnlyPrefixes lists prefixes a token may only read
// (GET/HEAD), never modify, regardless of scope: user administration
// (creating admins, resetting passwords / 2FA, adding SSH keys) and the
// stored SSH key material.
var apiTokenReadOnlyPrefixes = []string{
	"/api/users",
	"/api/ssh-keys",
}

func pathHasPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// apiTokenPathAllowed applies the session-only / read-only path rules.
func apiTokenPathAllowed(method, p string) bool {
	for _, prefix := range apiTokenSessionOnly {
		if pathHasPrefix(p, prefix) {
			return false
		}
	}
	if method == http.MethodGet || method == http.MethodHead {
		return true
	}
	for _, prefix := range apiTokenReadOnlyPrefixes {
		if pathHasPrefix(p, prefix) {
			return false
		}
	}
	return true
}

/* ------------------------------ handlers ----------------------------- */

type apiTokenResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Scope      string `json:"scope"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	RevokedAt  string `json:"revoked_at,omitempty"`
	Active     bool   `json:"active"`
}

func (a *App) apiTokenToResponse(t auth.APIToken) apiTokenResponse {
	loc := a.serverLocation()
	out := apiTokenResponse{ID: t.ID, Name: t.Name, Prefix: t.Prefix, Scope: t.Scope, CreatedAt: t.CreatedAt.In(loc).Format(time.RFC3339), Active: t.Active(time.Now())}
	if t.ExpiresAt != nil {
		out.ExpiresAt = t.ExpiresAt.In(loc).Format(time.RFC3339)
	}
	if t.LastUsedAt != nil {
		out.LastUsedAt = t.LastUsedAt.In(loc).Format(time.RFC3339)
	}
	if t.RevokedAt != nil {
		out.RevokedAt = t.RevokedAt.In(loc).Format(time.RFC3339)
	}
	return out
}

// sessionUserID returns the cookie-authenticated user only (never a
// token), for endpoints that must not be reachable with a token.
func (a *App) sessionUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if _, viaToken := apiTokenFromContext(r); viaToken {
		writeJSONErrorKey(w, r, "auth.apiTokenNotAllowedHere", http.StatusForbidden)
		return "", false
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return userID, true
}

// handleListMyTokens: GET /api/me/tokens.
func (a *App) handleListMyTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.sessionUserID(w, r)
	if !ok {
		return
	}
	list, err := a.APITokens.ListForUser(r.Context(), userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]apiTokenResponse, 0, len(list))
	for _, t := range list {
		out = append(out, a.apiTokenToResponse(t))
	}
	writeJSON(w, out)
}

type createAPITokenRequest struct {
	Name          string `json:"name"`
	Scope         string `json:"scope"`
	ExpiresInDays int    `json:"expires_in_days"`
}

// handleCreateMyToken: POST /api/me/tokens (session only) -> token shown once.
func (a *App) handleCreateMyToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.sessionUserID(w, r)
	if !ok {
		return
	}
	var req createAPITokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		writeJSONErrorKey(w, r, "auth.apiTokenNameRequired", http.StatusBadRequest)
		return
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = auth.APITokenScopeRead
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > 3650 {
		writeJSONErrorKey(w, r, "auth.apiTokenExpiryInvalid", http.StatusBadRequest)
		return
	}
	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}
	tok, plain, err := a.APITokens.Create(r.Context(), userID, req.Name, scope, expiresAt)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAPITokenScope):
			writeJSONErrorKey(w, r, "auth.apiTokenScopeInvalid", http.StatusBadRequest)
		case errors.Is(err, auth.ErrAPITokenLimit):
			writeJSONErrorKey(w, r, "auth.apiTokenLimit", http.StatusConflict)
		default:
			writeInternalError(w, err)
		}
		return
	}
	audit("api_token_created", auditFields{"user_id": userID, "token_id": tok.ID, "name": tok.Name, "scope": tok.Scope, "expires_at": func() string {
		if expiresAt != nil {
			return expiresAt.UTC().Format(time.RFC3339)
		}
		return ""
	}()})
	resp := struct {
		apiTokenResponse
		Token string `json:"token"`
	}{a.apiTokenToResponse(*tok), plain}
	writeJSONStatus(w, http.StatusCreated, resp)
}

// handleRevokeMyToken: DELETE /api/me/tokens/{id} (session only).
func (a *App) handleRevokeMyToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.sessionUserID(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := a.APITokens.Revoke(r.Context(), userID, id); err != nil {
		if errors.Is(err, auth.ErrAPITokenNotFound) {
			writeJSONErrorKey(w, r, "auth.apiTokenNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("api_token_revoked", auditFields{"user_id": userID, "token_id": id})
	w.WriteHeader(http.StatusNoContent)
}

// handleListUserTokens: GET /api/users/{user_id}/tokens (admin).
func (a *App) handleListUserTokens(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	target := strings.TrimSpace(chi.URLParam(r, "user_id"))
	list, err := a.APITokens.ListForUser(r.Context(), target)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]apiTokenResponse, 0, len(list))
	for _, t := range list {
		out = append(out, a.apiTokenToResponse(t))
	}
	writeJSON(w, out)
}

// handleAdminRevokeToken: DELETE /api/users/{user_id}/tokens/{id} (admin).
func (a *App) handleAdminRevokeToken(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if _, viaToken := apiTokenFromContext(r); viaToken {
		writeJSONErrorKey(w, r, "auth.apiTokenNotAllowedHere", http.StatusForbidden)
		return
	}
	target := strings.TrimSpace(chi.URLParam(r, "user_id"))
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := a.APITokens.Revoke(r.Context(), target, id); err != nil {
		if errors.Is(err, auth.ErrAPITokenNotFound) {
			writeJSONErrorKey(w, r, "auth.apiTokenNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("api_token_revoked", auditFields{"user_id": a.currentUserID(r), "token_id": id, "owner_id": target, "by_admin": true})
	w.WriteHeader(http.StatusNoContent)
}
