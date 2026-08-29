package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetrics_AuthAndContent(t *testing.T) {
	t.Setenv(metricsTokenEnv, "scrape-secret")
	app := newTestApp(t)
	router := app.NewRouter()
	if _, err := app.UserStore.CreateUser("bob", "bob", "Password1!", "user"); err != nil {
		t.Fatal(err)
	}
	adminSess, _ := app.SessionStore.Create("admin")
	bobSess, _ := app.SessionStore.Create("bob")

	get := func(sessID, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if sessID != "" {
			req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := get("", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if w := get("", "wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", w.Code)
	}
	if w := get(bobSess.ID, ""); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin: %d", w.Code)
	}
	// Generate a little traffic so the counters have values.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "wrong"}, ""))

	for _, w := range []*httptest.ResponseRecorder{get(adminSess.ID, ""), get("", "scrape-secret")} {
		if w.Code != http.StatusOK {
			t.Fatalf("scrape: %d %s", w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
			t.Fatalf("content-type = %q", ct)
		}
		body := w.Body.String()
		for _, want := range []string{
			"# TYPE vantyx_http_requests_total counter",
			`vantyx_http_requests_total{method="POST",status="4xx"}`,
			"vantyx_http_request_duration_seconds_count",
			`vantyx_logins_total{result="failure"}`,
			"vantyx_audit_events_total",
			`vantyx_active_sessions{kind="terminal"} 0`,
			`vantyx_users_total{role="admin"} 1`,
			`vantyx_users_total{role="user"} 1`,
			"vantyx_targets_total 0",
			"vantyx_access_requests_pending 0",
			`vantyx_recording_export_jobs{state="queued"} 0`,
			"vantyx_build_info{",
			"process_uptime_seconds",
			"go_goroutines",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %q in:\n%s", want, body)
			}
		}
		// Scrapes themselves are not counted and nothing user-identifying leaks.
		if strings.Contains(body, `path=`) || strings.Contains(body, "admin@") {
			t.Fatalf("unexpected label content:\n%s", body)
		}
	}
}
