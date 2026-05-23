package httpapi

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// auditFields は監査・操作ログ用のキー/値ペアです。
// 値は log.Printf にそのまま渡せる型（string, int など）を想定します。
type auditFields map[string]interface{}

// formatFields は auditFields を "key=value" の並びに整形します。
// 例: action="terminal session start" user_id=alice target_id=web-1
func formatFields(fields auditFields) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := fields[k]
		switch vv := v.(type) {
		case string:
			// 値にスペースが入る場合に備え、簡易的にダブルクオートで囲む。
			if strings.ContainsAny(vv, " \t") {
				parts = append(parts, k+`="`+vv+`"`)
			} else {
				parts = append(parts, k+"="+vv)
			}
		default:
			parts = append(parts, k+"="+fmt.Sprint(v))
		}
	}
	return strings.Join(parts, " ")
}

// audit は httpapi レイヤの共通監査ログ出力です。
// event は "terminal_session_start" のようなイベント名を想定します。
func audit(event string, fields auditFields) {
	if fields == nil {
		fields = auditFields{}
	}
	// Keep a best-effort structured buffer for UI/debugging.
	if auditBuffer != nil {
		copied := auditFields{}
		for k, v := range fields {
			copied[k] = v
		}
		copied["event"] = event
		auditBuffer.add(AuditEntry{
			Time:   time.Now().UTC(),
			Event:  event,
			Fields: copied,
		})
	}
	// Persist (DB + log file) if configured.
	if sink := getGlobalAuditSink(); sink != nil {
		copied := auditFields{}
		for k, v := range fields {
			copied[k] = v
		}
		copied["event"] = event
		sink.write(AuditEntry{
			Time:   time.Now().UTC(),
			Event:  event,
			Fields: copied,
		})
	}
	fields["event"] = event
	log.Printf("audit %s", formatFields(fields))
}

// auditBuffer stores recent audit events for the audit log UI (in-memory, bounded).
var auditBuffer = newAuditStore(2000)
