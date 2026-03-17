package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleAuditLogs returns recent in-memory audit entries (admin only).
// GET /api/audit?limit=200&event=login_&user_id=alice
func (a *App) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	eventQ := strings.TrimSpace(q.Get("event"))
	userQ := strings.TrimSpace(q.Get("user_id"))

	// Prefer DB-backed persistent logs when available.
	if a != nil && a.DB != nil {
		// hard cap to protect DB; UI defaults to 200
		if limit <= 0 || limit > 1000 {
			limit = 200
		}
		conds := []string{"1=1"}
		args := []interface{}{}
		if eventQ != "" {
			conds = append(conds, "event LIKE ?")
			args = append(args, "%"+eventQ+"%")
		}
		if userQ != "" {
			conds = append(conds, "user_id = ?")
			args = append(args, userQ)
		}
		args = append(args, limit)
		rows, err := a.DB.Query(
			`SELECT time,event,fields_json FROM audit_logs WHERE `+strings.Join(conds, " AND ")+` ORDER BY time DESC LIMIT ?`,
			args...,
		)
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

