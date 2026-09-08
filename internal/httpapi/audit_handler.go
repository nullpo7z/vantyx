package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type auditLogsResponse struct {
	Items      []AuditEntry `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// handleAuditLogs returns recent in-memory audit entries (admin only).
// GET /api/audit?limit=200&event=login_&user_id=alice&from=&to=&after_id=&exclude_event=
func (a *App) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	eventQ := strings.TrimSpace(q.Get("event"))
	userQ := strings.TrimSpace(q.Get("user_id"))
	excludeEvents := parseExcludeEvents(q.Get("exclude_event"))

	from, to, err := parseTimeRange(q.Get("from"), q.Get("to"), time.Now().UTC())
	if err != nil {
		writeTimeRangeError(w, r, err)
		return
	}

	pageLimit := limit
	if pageLimit <= 0 || pageLimit > 1000 {
		pageLimit = 200
	}

	var afterID int64
	if afterStr := strings.TrimSpace(q.Get("after_id")); afterStr != "" {
		afterID, err = strconv.ParseInt(afterStr, 10, 64)
		if err != nil || afterID <= 0 {
			writeJSONErrorKey(w, r, "common.invalidAfterID", http.StatusBadRequest)
			return
		}
	}

	filter := func(e AuditEntry) bool {
		if e.Time.Before(from) || !e.Time.Before(to) {
			return false
		}
		if auditEventExcluded(e.Event, excludeEvents) {
			return false
		}
		if eventQ != "" && !strings.Contains(e.Event, eventQ) {
			return false
		}
		if userQ != "" {
			if v, ok := e.Fields["user_id"]; ok {
				if s, ok2 := v.(string); ok2 {
					return s == userQ
				}
			}
			return false
		}
		return true
	}

	// Prefer DB-backed persistent logs when available.
	if a != nil && a.DB != nil {
		sqlStr, args := buildAuditLogQuery(eventQ, userQ, excludeEvents, from, to, afterID, pageLimit+1)
		rows, err := a.DB.Query(sqlStr, args...)
		if err == nil {
			defer rows.Close()
			items := make([]AuditEntry, 0, pageLimit+1)
			for rows.Next() {
				var id int64
				var t time.Time
				var ev string
				var fieldsJSON string
				if scanErr := rows.Scan(&id, &t, &ev, &fieldsJSON); scanErr != nil {
					continue
				}
				fields := auditFields{}
				_ = json.Unmarshal([]byte(fieldsJSON), &fields)
				items = append(items, AuditEntry{
					ID:     id,
					Time:   t.In(a.serverLocation()), // same zone as the logs (VANTYX_TIMEZONE)
					Event:  ev,
					Fields: fields,
				})
			}
			nextCursor := auditNextCursor(items, pageLimit)
			items = trimAuditPage(items, pageLimit)
			writeJSON(w, auditLogsResponse{Items: items, NextCursor: nextCursor})
			return
		}
	}

	// Fallback: in-memory recent events.
	items := auditBuffer.listNewestFirst(pageLimit, afterID, filter)
	nextCursor := auditNextCursor(items, pageLimit)
	items = trimAuditPage(items, pageLimit)
	writeJSON(w, auditLogsResponse{Items: items, NextCursor: nextCursor})
}

func parseExcludeEvents(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if ev := strings.TrimSpace(part); ev != "" {
			out = append(out, ev)
		}
	}
	return out
}

func auditEventExcluded(event string, excluded []string) bool {
	for _, ex := range excluded {
		if event == ex {
			return true
		}
	}
	return false
}

func buildAuditLogQuery(eventQ, userQ string, excludeEvents []string, from, to time.Time, afterID int64, limit int) (string, []interface{}) {
	base := `SELECT id,time,event,fields_json FROM audit_logs`
	var conds []string
	var args []interface{}

	conds = append(conds, "time >= ?", "time < ?")
	args = append(args, from.UTC(), to.UTC())

	if eventQ != "" {
		// Escape SQLite LIKE wildcards (% / _) and the escape byte
		// itself so caller-controlled fragments can not turn the query
		// into a full-table scan or smuggle wildcards (M-12).
		conds = append(conds, `event LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escapeLikeOperand(eventQ)+"%")
	}
	if userQ != "" {
		conds = append(conds, "user_id = ?")
		args = append(args, userQ)
	}
	for _, ex := range excludeEvents {
		conds = append(conds, "event <> ?")
		args = append(args, ex)
	}
	if afterID > 0 {
		conds = append(conds, "id < ?")
		args = append(args, afterID)
	}

	sqlStr := base + ` WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY time DESC, id DESC LIMIT ?`
	args = append(args, limit)
	return sqlStr, args
}
