package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleAuditLogs returns recent in-memory audit entries (admin only).
// GET /api/audit?limit=200&event=login_&user_id=alice&from=&to=
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
		writeTimeRangeError(w, err)
		return
	}

	// Prefer DB-backed persistent logs when available.
	if a != nil && a.DB != nil {
		// hard cap to protect DB; UI defaults to 200
		if limit <= 0 || limit > 1000 {
			limit = 200
		}
		sqlStr, args := buildAuditLogQuery(eventQ, userQ, excludeEvents, from, to, limit)

		rows, err := a.DB.Query(sqlStr, args...)
		if err == nil {
			defer rows.Close()
			items := make([]AuditEntry, 0, limit)
			for rows.Next() {
				var t time.Time
				var ev string
				var fieldsJSON string
				if scanErr := rows.Scan(&t, &ev, &fieldsJSON); scanErr != nil {
					continue
				}
				fields := auditFields{}
				_ = json.Unmarshal([]byte(fieldsJSON), &fields)
				items = append(items, AuditEntry{
					Time:   t.UTC(),
					Event:  ev,
					Fields: fields,
				})
			}
			writeJSON(w, map[string]interface{}{"items": items})
			return
		}
	}

	// Fallback: in-memory recent events.
	items := auditBuffer.listNewestFirst(limit, func(e AuditEntry) bool {
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
	})
	writeJSON(w, map[string]interface{}{"items": items})
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

func buildAuditLogQuery(eventQ, userQ string, excludeEvents []string, from, to time.Time, limit int) (string, []interface{}) {
	base := `SELECT time,event,fields_json FROM audit_logs`
	var conds []string
	var args []interface{}

	conds = append(conds, "time >= ?", "time < ?")
	args = append(args, from.UTC(), to.UTC())

	if eventQ != "" {
		conds = append(conds, "event LIKE ?")
		args = append(args, "%"+eventQ+"%")
	}
	if userQ != "" {
		conds = append(conds, "user_id = ?")
		args = append(args, userQ)
	}
	for _, ex := range excludeEvents {
		conds = append(conds, "event <> ?")
		args = append(args, ex)
	}

	sqlStr := base + ` WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY time DESC LIMIT ?`
	args = append(args, limit)
	return sqlStr, args
}
