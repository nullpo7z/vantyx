package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/auth"
)

// handleListTags returns distinct tags visible to the current user.
// Admins receive the global union; other users only see tags attached
// to themselves, accessible targets, or groups they belong to.
func (a *App) handleListTags(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	admin := false
	if a.UserStore != nil {
		if u, err := a.UserStore.GetByID(userID); err == nil && u != nil && u.Role == auth.RoleAdmin {
			admin = true
		}
	}

	tagSet := map[string]struct{}{}
	if admin {
		rows, err := a.DB.QueryContext(ctx, `
			SELECT DISTINCT tag FROM (
				SELECT tag FROM user_tags
				UNION
				SELECT tag FROM target_tags
				UNION
				SELECT tag FROM group_tags
			) ORDER BY tag`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var tag string
			if err := rows.Scan(&tag); err != nil {
				writeInternalError(w, err)
				return
			}
			tagSet[tag] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
	} else {
		userRows, err := a.DB.QueryContext(ctx, `SELECT tag FROM user_tags WHERE user_id = ?`, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for userRows.Next() {
			var tag string
			if err := userRows.Scan(&tag); err != nil {
				userRows.Close()
				writeInternalError(w, err)
				return
			}
			tagSet[tag] = struct{}{}
		}
		userRows.Close()
		if err := userRows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}

		groupRows, err := a.DB.QueryContext(ctx, `
			SELECT gt.tag FROM group_tags gt
			INNER JOIN group_members gm ON gm.group_id = gt.group_id
			WHERE gm.user_id = ?`, userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for groupRows.Next() {
			var tag string
			if err := groupRows.Scan(&tag); err != nil {
				groupRows.Close()
				writeInternalError(w, err)
				return
			}
			tagSet[tag] = struct{}{}
		}
		groupRows.Close()
		if err := groupRows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}

		if allowed, err := a.allowedTargetIDSet(ctx, userID); err == nil && a.TargetStore != nil {
			for tid := range allowed {
				if tags, err := a.TargetStore.TagsForTarget(ctx, tid); err == nil {
					for _, tag := range tags {
						tagSet[tag] = struct{}{}
					}
				}
			}
		}
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		Tags []string `json:"tags"`
	}{Tags: tags})
}
