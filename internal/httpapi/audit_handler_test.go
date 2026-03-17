package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuditLogs_Admin_DBQueryFilters(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	// Seed DB audit logs.
	now := time.Now().UTC()
	_, _ = app.DB.ExecContext(context.Background(),
		`INSERT INTO audit_logs(time,event,user_id,method,path,status,remote,duration_ms,fields_json) VALUES (?,?,?,?,?,?,?,?,?)`,
		now, "http_request", "admin", "GET", "/api/x", 200, "r", 1, `{"user_id":"admin","event":"http_request"}`,
	)
	_, _ = app.DB.ExecContext(context.Background(),
		`INSERT INTO audit_logs(time,event,user_id,method,path,status,remote,duration_ms,fields_json) VALUES (?,?,?,?,?,?,?,?,?)`,
		now.Add(-time.Second), "login_success", "bob", "", "", 0, "", 0, `{"user_id":"bob","event":"login_success"}`,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/audit?limit=10&event=http_&user_id=admin", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	var out struct {
		Items []AuditEntry `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].Event != "http_request" {
		t.Fatalf("unexpected event: %q", out.Items[0].Event)
	}
}

