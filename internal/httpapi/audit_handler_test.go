package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestParseExcludeEvents(t *testing.T) {
	if got := parseExcludeEvents(""); got != nil {
		t.Fatalf("empty: %v", got)
	}
	got := parseExcludeEvents("http_request, login_failed ,,")
	if len(got) != 2 || got[0] != "http_request" || got[1] != "login_failed" {
		t.Fatalf("got %v", got)
	}
}

func TestAuditEventExcluded(t *testing.T) {
	ex := []string{"http_request", "login_failed"}
	if !auditEventExcluded("http_request", ex) {
		t.Fatal("expected excluded")
	}
	if auditEventExcluded("login_success", ex) {
		t.Fatal("expected not excluded")
	}
}

func TestBuildAuditLogQuery_ExcludeEvents(t *testing.T) {
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	sqlStr, args := buildAuditLogQuery("", "", []string{"http_request", "noise"}, from, now, 0, 50)
	if !strings.Contains(sqlStr, "ORDER BY time DESC, id DESC") {
		t.Fatalf("sql order: %s", sqlStr)
	}
	if !strings.Contains(sqlStr, "event <> ?") {
		t.Fatalf("sql: %s", sqlStr)
	}
	if len(args) < 4 {
		t.Fatalf("args: %v", args)
	}
}

func TestAuditLogs_ExcludeEvent(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	now := time.Now().UTC()
	_, _ = app.DB.ExecContext(context.Background(),
		`INSERT INTO audit_logs(time,event,user_id,method,path,status,remote,duration_ms,fields_json) VALUES (?,?,?,?,?,?,?,?,?)`,
		now, "http_request", "admin", "GET", "/api/x", 200, "r", 1, `{}`,
	)
	_, _ = app.DB.ExecContext(context.Background(),
		`INSERT INTO audit_logs(time,event,user_id,method,path,status,remote,duration_ms,fields_json) VALUES (?,?,?,?,?,?,?,?,?)`,
		now.Add(-time.Second), "login_success", "bob", "", "", 0, "", 0, `{"user_id":"bob"}`,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/audit?limit=10&exclude_event=http_request", nil)
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
	if len(out.Items) != 1 || out.Items[0].Event != "login_success" {
		t.Fatalf("expected login_success only, got %+v", out.Items)
	}
}

func TestAuditLogs_Pagination(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	now := time.Now().UTC()
	// Higher id must correlate with newer time for ORDER BY time DESC, id DESC paging.
	for i := 0; i < 5; i++ {
		_, _ = app.DB.ExecContext(context.Background(),
			`INSERT INTO audit_logs(time,event,user_id,method,path,status,remote,duration_ms,fields_json) VALUES (?,?,?,?,?,?,?,?,?)`,
			now.Add(-time.Duration(4-i)*time.Second), "login_success", "admin", "", "", 0, "", 0, `{}`,
		)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/audit?limit=2&exclude_event=http_request", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("page1: %d", w.Result().StatusCode)
	}
	var page1 struct {
		Items      []AuditEntry `json:"items"`
		NextCursor string       `json:"next_cursor"`
	}
	if err := json.NewDecoder(w.Body).Decode(&page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1: items=%d cursor=%q", len(page1.Items), page1.NextCursor)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/audit?limit=2&exclude_event=http_request&after_id="+page1.NextCursor, nil)
	req2.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	var page2 struct {
		Items      []AuditEntry `json:"items"`
		NextCursor string       `json:"next_cursor"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("page2: got %d items", len(page2.Items))
	}
	if page2.Items[0].ID >= page1.Items[1].ID {
		t.Fatalf("expected older ids on page2: %d >= %d", page2.Items[0].ID, page1.Items[1].ID)
	}
}

func TestAuditLogs_InvalidAfterID(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")

	req := httptest.NewRequest(http.MethodGet, "/api/audit?after_id=abc", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Result().StatusCode)
	}
}
