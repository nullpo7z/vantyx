package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
)

// currentUserIDWithError returns the authenticated user ID and nil, or ("", nil) when not authenticated,
// or ("", err) when a storage error (e.g. database locked) occurs. Callers that need to return 500 on
// storage errors should use this and call writeInternalError(w, err) when err != nil.
func (a *App) currentUserIDWithError(r *http.Request) (string, error) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		// No session cookie is "unauthenticated", not an error.
		return "", nil //nolint:nilerr
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return "", nil
		}
		return "", err
	}
	return sess.UserID, nil
}

// currentUserID returns the authenticated user ID from the session
// cookie, or an empty string when the caller is unauthenticated.
//
// Handlers should funnel authentication checks through this helper and
// only inspect whether the result is empty. Storage errors are
// swallowed (empty string), so any code that needs to distinguish
// "unauthenticated" from "storage failure" must use
// [App.currentUserIDWithError] and return 500 on err != nil.
func (a *App) currentUserID(r *http.Request) string {
	userID, _ := a.currentUserIDWithError(r)
	return userID
}

// forcePasswordChangeMiddleware blocks all non-bootstrap requests
// while the caller's account is flagged for forced rotation. Without
// this guard the SPA's UI hint alone would leave the API reachable
// (CWE-1188 / ASVS V2.10.4). The whitelist below covers the calls the
// rotation flow itself needs:
//
//   - GET /api/me               – display the "you must change" banner
//   - POST /api/me/password     – the rotation itself
//   - POST /api/logout          – escape hatch
//   - GET /healthz, /api/spec   – infra / docs (no user data)
func (a *App) forcePasswordChangeMiddleware(next http.Handler) http.Handler {
	whitelist := map[string]struct{}{
		"/api/me":          {},
		"/api/me/password": {},
		"/api/logout":      {},
		"/api/login":       {},
		"/healthz":         {},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Static assets and SSE / WS handshakes are gated by their own
		// auth checks; only consult the flag for /api/*.
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := whitelist[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}
		userID, err := a.currentUserIDWithError(r)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if userID == "" {
			next.ServeHTTP(w, r)
			return
		}
		u, err := a.UserStore.GetByID(userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if u == nil || !u.ForcePasswordChange {
			next.ServeHTTP(w, r)
			return
		}
		writeJSONErrorKey(w, r, "auth.passwordChangeRequired", http.StatusForbidden)
	})
}

// requireAdmin writes a JSON error and returns false if the current
// user is not an admin. Admin-only handlers should call this first and
// return early. Storage errors (for example "database is locked") map
// to HTTP 500.
func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	userID, err := a.currentUserIDWithError(r)
	if err != nil {
		writeInternalError(w, err)
		return false
	}
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return false
	}
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return false
	}
	if u.Role != auth.RoleAdmin {
		writeJSONErrorKey(w, r, "common.forbiddenAdminOnly", http.StatusForbidden)
		return false
	}
	return true
}

// requireGroupMemberOrAdmin enforces that the current user is either an
// admin or a member of the given group. The first return value carries
// the resolved user ID for callers that need it (the user-management
// audit handlers do), and is empty when ok is false. Storage errors
// (for example "database is locked") map to HTTP 500.
//
//nolint:unparam // userID is part of the public helper contract.
func (a *App) requireGroupMemberOrAdmin(w http.ResponseWriter, r *http.Request, groupID access.GroupID) (string, bool) {
	userID, err := a.currentUserIDWithError(r)
	if err != nil {
		writeInternalError(w, err)
		return "", false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return "", false
	}
	ctx := r.Context()
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return "", false
	}
	if u.Role == auth.RoleAdmin {
		return userID, true
	}
	gids, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return "", false
	}
	for _, gid := range gids {
		if gid == groupID {
			return userID, true
		}
	}
	writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
	return "", false
}

// requireTargetAccess enforces that the current user can access the
// given target (via group or tag membership). The first return value
// carries the resolved user ID for callers that need it, and is empty
// when ok is false.
//
//nolint:unparam // userID is part of the public helper contract.
func (a *App) requireTargetAccess(w http.ResponseWriter, r *http.Request, targetID access.TargetID) (string, bool) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return "", false
	}
	ctx := r.Context()
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return "", false
	}
	for _, id := range allowedIDs {
		if id == targetID {
			return userID, true
		}
	}
	writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
	return "", false
}

// getSessionAndTargetWithAccess validates the session, loads the target,
// and enforces target access in that order. On success it returns
// (userID, target, true); on failure it writes the appropriate HTTP
// response and returns (_, nil, false).
//
// The target is fetched before authorisation so the response can
// distinguish 404 (no such target) from 403 (target exists but the
// user cannot reach it).
func (a *App) getSessionAndTargetWithAccess(w http.ResponseWriter, r *http.Request, targetID string) (userID string, target *access.Target, ok bool) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		writeJSONErrorKey(w, r, "common.targetIDRequired", http.StatusBadRequest)
		return "", nil, false
	}
	userID = a.currentUserID(r)
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return "", nil, false
	}
	ctx := r.Context()
	target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err != nil {
		audit("target_not_found", auditFields{
			"user_id":   userID,
			"target_id": targetID,
		})
		writeJSONErrorKey(w, r, "common.targetNotFound", http.StatusNotFound)
		return "", nil, false
	}
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return "", nil, false
	}
	allowed := false
	for _, id := range allowedIDs {
		if id == access.TargetID(targetID) {
			allowed = true
			break
		}
	}
	if !allowed {
		audit("target_access_forbidden", auditFields{
			"user_id":   userID,
			"target_id": targetID,
		})
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return "", nil, false
	}
	return userID, target, true
}

// allowedTargetIDSet returns target IDs the user may access via groups/tags.
func (a *App) allowedTargetIDSet(ctx context.Context, userID string) (map[access.TargetID]struct{}, error) {
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		return nil, err
	}
	set := make(map[access.TargetID]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		set[id] = struct{}{}
	}
	return set, nil
}

// userCanAccessTarget reports whether userID may access the given target.
func (a *App) userCanAccessTarget(ctx context.Context, userID string, targetID access.TargetID) (bool, error) {
	set, err := a.allowedTargetIDSet(ctx, userID)
	if err != nil {
		return false, err
	}
	_, ok := set[targetID]
	return ok, nil
}
