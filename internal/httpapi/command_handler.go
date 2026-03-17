package httpapi

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type commandLogItem struct {
	Time      time.Time `json:"time"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	TargetID  string    `json:"target_id"`
	LineText  string    `json:"line_text"`
}

func (a *App) handleCommandLogs(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if a.DB == nil {
		writeJSON(w, map[string]interface{}{"items": []commandLogItem{}})
		return
	}

	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	userID := strings.TrimSpace(q.Get("user_id"))
	targetID := strings.TrimSpace(q.Get("target_id"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	// For gosec (G202) and to keep the query string static, enumerate
	// all combinations of optional filters instead of concatenating fragments.
	baseSQL := `SELECT time,session_id,user_id,target_id,line_text FROM command_logs`
	now := time.Now().Add(-30 * 24 * time.Hour).UTC()

	hasQuery := query != ""
	hasUser := userID != ""
	hasTarget := targetID != ""

	var (
		sqlStr string
		args   []interface{}
	)

	switch {
	case !hasQuery && !hasUser && !hasTarget:
		sqlStr = baseSQL + ` WHERE time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{now, limit}
	case hasQuery && !hasUser && !hasTarget:
		sqlStr = baseSQL + ` WHERE line_text LIKE ? AND time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{"%" + query + "%", now, limit}
	case !hasQuery && hasUser && !hasTarget:
		sqlStr = baseSQL + ` WHERE user_id = ? AND time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{userID, now, limit}
	case !hasQuery && !hasUser && hasTarget:
		sqlStr = baseSQL + ` WHERE target_id = ? AND time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{targetID, now, limit}
	case hasQuery && hasUser && !hasTarget:
		sqlStr = baseSQL + ` WHERE line_text LIKE ? AND user_id = ? AND time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{"%" + query + "%", userID, now, limit}
	case hasQuery && !hasUser && hasTarget:
		sqlStr = baseSQL + ` WHERE line_text LIKE ? AND target_id = ? AND time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{"%" + query + "%", targetID, now, limit}
	case !hasQuery && hasUser && hasTarget:
		sqlStr = baseSQL + ` WHERE user_id = ? AND target_id = ? AND time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{userID, targetID, now, limit}
	case hasQuery && hasUser && hasTarget:
		sqlStr = baseSQL + ` WHERE line_text LIKE ? AND user_id = ? AND target_id = ? AND time >= ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{"%" + query + "%", userID, targetID, now, limit}
	}

	rows, err := a.DB.Query(sqlStr, args...)
	if err != nil && err != sql.ErrNoRows {
		writeJSONError(w, "failed to query command logs", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]commandLogItem, 0, limit)
	for rows.Next() {
		var it commandLogItem
		if err := rows.Scan(&it.Time, &it.SessionID, &it.UserID, &it.TargetID, &it.LineText); err != nil {
			continue
		}
		it.Time = it.Time.UTC()
		items = append(items, it)
	}
	writeJSON(w, map[string]interface{}{"items": items})
}
