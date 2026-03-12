package httpapi

import (
	"net/http"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
)

// currentUserID returns the authenticated user ID from the session cookie, or empty string if unauthenticated.
// 認証状態の判定はこの関数経由に集約し、ハンドラ側では userID が空かどうかのみを見るようにする。
func (a *App) currentUserID(r *http.Request) string {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		return ""
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		return ""
	}
	return sess.UserID
}

// requireAdmin writes JSON error and returns false if the current user is not an admin.
// Admin 権限が必要なエンドポイントは、この関数を最初に呼び出して早期 return するだけにする。
func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	userID := a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	if u.Role != auth.RoleAdmin {
		writeJSONError(w, "forbidden: admin only", http.StatusForbidden)
		return false
	}
	return true
}

// requireGroupMemberOrAdmin enforces that the current user is either admin or a member of the given group.
// グループ単位の権限チェックを行う場所で再利用するためのヘルパー。
func (a *App) requireGroupMemberOrAdmin(w http.ResponseWriter, r *http.Request, groupID access.GroupID) (userID string, ok bool) {
	userID = strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	ctx := r.Context()
	u, err := a.UserStore.GetByID(userID)
	if err != nil || u == nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
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
	writeJSONError(w, "forbidden", http.StatusForbidden)
	return "", false
}

// requireTargetAccess enforces that the current user can access the given target (via groups / tags).
// ターゲット単位の権限チェックを行う場所で再利用するためのヘルパー。
func (a *App) requireTargetAccess(w http.ResponseWriter, r *http.Request, targetID access.TargetID) (userID string, ok bool) {
	userID = strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
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
	writeJSONError(w, "forbidden", http.StatusForbidden)
	return "", false
}

// getSessionAndTargetWithAccess validates session, loads the target, and enforces target access.
// Returns (userID, target, true) on success; on failure writes the response and returns (_, nil, false).
// 存在しない target_id の場合は 404、権限がない場合は 403 を返すため、先にターゲット取得してから認可チェックする。
func (a *App) getSessionAndTargetWithAccess(w http.ResponseWriter, r *http.Request, targetID string) (userID string, target *access.Target, ok bool) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return "", nil, false
	}
	userID = a.currentUserID(r)
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return "", nil, false
	}
	ctx := r.Context()
	target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err != nil {
		audit("target_not_found", auditFields{
			"user_id":   userID,
			"target_id": targetID,
		})
		writeJSONError(w, "target not found", http.StatusNotFound)
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
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return "", nil, false
	}
	return userID, target, true
}
