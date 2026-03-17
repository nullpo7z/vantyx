package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleCommandLogs_AdminQuery(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()

	sess, _ := app.SessionStore.Create("admin")

	// Seed one command log row.
	if app.DB == nil {
		t.Skip("DB not available")
	}
	_, err := app.DB.ExecContext(context.Background(),
		`INSERT INTO command_logs (session_id,user_id,target_id,time,line_text) VALUES (?,?,?,?,?)`,
		"s1", "admin", "t1", time.Now().UTC(), "ls -la /var/log",
	)
	if err != nil {
		t.Fatalf("insert command_log: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/commands?query=ls&user_id=admin&limit=10", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
}
