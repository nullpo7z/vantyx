package httpapi

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type commandLogItem struct {
	ID        int64     `json:"id,omitempty"`
	Time      time.Time `json:"time"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	TargetID  string    `json:"target_id"`
	LineText  string    `json:"line_text"`
}

type commandLogsResponse struct {
	Items      []commandLogItem `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func (a *App) handleCommandLogs(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if a.DB == nil {
		writeJSON(w, commandLogsResponse{Items: []commandLogItem{}})
		return
	}

	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	userID := strings.TrimSpace(q.Get("user_id"))
	targetID := strings.TrimSpace(q.Get("target_id"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	pageLimit := limit
	if pageLimit <= 0 || pageLimit > 500 {
		pageLimit = 200
	}

	from, to, err := parseTimeRange(q.Get("from"), q.Get("to"), time.Now().UTC())
	if err != nil {
		writeTimeRangeError(w, err)
		return
	}

	var afterID int64
	if afterStr := strings.TrimSpace(q.Get("after_id")); afterStr != "" {
		afterID, err = strconv.ParseInt(afterStr, 10, 64)
		if err != nil || afterID <= 0 {
			writeJSONError(w, "invalid after_id", http.StatusBadRequest)
			return
		}
	}

	sqlStr, args := buildCommandLogQuery(query, userID, targetID, from, to, afterID, pageLimit+1)

	rows, err := a.DB.Query(sqlStr, args...)
	if err != nil && err != sql.ErrNoRows {
		writeJSONError(w, "failed to query command logs", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]commandLogItem, 0, pageLimit+1)
	for rows.Next() {
		var it commandLogItem
		if err := rows.Scan(&it.ID, &it.Time, &it.SessionID, &it.UserID, &it.TargetID, &it.LineText); err != nil {
			continue
		}
		it.Time = it.Time.UTC()
		items = append(items, it)
	}
	nextCursor := commandNextCursor(items, pageLimit)
	items = trimCommandPage(items, pageLimit)
	writeJSON(w, commandLogsResponse{Items: items, NextCursor: nextCursor})
}

func commandNextCursor(items []commandLogItem, pageLimit int) string {
	if len(items) <= pageLimit || pageLimit <= 0 {
		return ""
	}
	return strconv.FormatInt(items[pageLimit-1].ID, 10)
}

func trimCommandPage(items []commandLogItem, pageLimit int) []commandLogItem {
	if len(items) > pageLimit && pageLimit > 0 {
		return items[:pageLimit]
	}
	return items
}

// buildCommandLogQuery returns a static SQL string and args for command_logs search.
func buildCommandLogQuery(query, userID, targetID string, from, to time.Time, afterID int64, limit int) (string, []interface{}) {
	baseSQL := `SELECT id,time,session_id,user_id,target_id,line_text FROM command_logs`
	var conds []string
	var args []interface{}

	conds = append(conds, "time >= ?", "time < ?")
	args = append(args, from.UTC(), to.UTC())

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
	if afterID > 0 {
		conds = append(conds, "id < ?")
		args = append(args, afterID)
	}

	sqlStr := baseSQL + ` WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY time DESC, id DESC LIMIT ?`
	args = append(args, limit)
	return sqlStr, args
}
