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

	conds := []string{"1=1"}
	args := []interface{}{}
	if query != "" {
		conds = append(conds, "line_text LIKE ?")
		args = append(args, "%"+query+"%")
	}
	if userID != "" {
		conds = append(conds, "user_id = ?")
		args = append(args, userID)
	}
	if targetID != "" {
		conds = append(conds, "target_id = ?")
		args = append(args, targetID)
	}
	// For now, limit to recent 30 days to keep the table small in practice.
	conds = append(conds, "time >= ?")
	args = append(args, time.Now().Add(-30*24*time.Hour).UTC())

	args = append(args, limit)
	rows, err := a.DB.Query(
		`SELECT time,session_id,user_id,target_id,line_text
		 FROM command_logs
		 WHERE `+strings.Join(conds, " AND ")+`
		 ORDER BY time DESC
		 LIMIT ?`,
		args...,
	)
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

