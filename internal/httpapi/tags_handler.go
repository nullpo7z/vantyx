package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// handleListTags returns the union of distinct tags registered against
// users, targets, and groups. Requires authentication; used by the tag
// picker in the SPA.
func (a *App) handleListTags(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := a.SessionStore.Get(c.Value); err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
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
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			writeInternalError(w, err)
			return
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	if tags == nil {
		tags = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		Tags []string `json:"tags"`
	}{Tags: tags})
}
