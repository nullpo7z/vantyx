package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/auth"
)

func seedCommandLog(t *testing.T, app *App, userID, targetID, line string) {
	t.Helper()
	if app.DB == nil {
		t.Skip("DB not available")
	}
	_, err := app.DB.ExecContext(context.Background(),
		`INSERT INTO command_logs (session_id,user_id,target_id,time,line_text) VALUES (?,?,?,?,?)`,
		"s1", userID, targetID, time.Now().UTC(), line,
	)
	if err != nil {
		t.Fatalf("insert command_log: %v", err)
	}
}

func getCommandLogs(t *testing.T, router http.Handler, sessID, query string) []commandLogItem {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/commands"+query, nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sessID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Result().StatusCode, w.Body.String())
	}
	var out struct {
		Items []commandLogItem `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Items
}

func TestHandleCommandLogs_AdminQuery(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	seedCommandLog(t, app, "admin", "t1", "ls -la /var/log")

	items := getCommandLogs(t, router, sess.ID, "?query=ls&user_id=admin&limit=10")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].LineText != "ls -la /var/log" {
		t.Fatalf("unexpected line: %q", items[0].LineText)
	}
}

func TestHandleCommandLogs_FilterCombinations(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	seedCommandLog(t, app, "admin", "t1", "sudo apt update")
	seedCommandLog(t, app, "bob", "t2", "rm -rf /tmp/x")

	tests := []struct {
		query string
		min   int
	}{
		{"", 2},
		{"?query=sudo", 1},
		{"?user_id=bob", 1},
		{"?target_id=t2", 1},
		{"?query=sudo&user_id=admin", 1},
		{"?query=rm&target_id=t2", 1},
		{"?user_id=admin&target_id=t1", 1},
		{"?query=sudo&user_id=admin&target_id=t1", 1},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			items := getCommandLogs(t, router, sess.ID, tc.query)
			if len(items) < tc.min {
				t.Fatalf("expected at least %d items, got %d", tc.min, len(items))
			}
		})
	}
}

func TestHandleCommandLogs_ForbiddenNonAdmin(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	_, _ = app.UserStore.CreateUser("u1", "user1", "User123!", auth.RoleUser)
	sess, _ := app.SessionStore.Create("u1")

	req := httptest.NewRequest(http.MethodGet, "/api/commands?query=x", nil)
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Result().StatusCode)
	}
}

func TestHandleCommandLogs_DefaultLimit(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	seedCommandLog(t, app, "admin", "t1", "ping")

	items := getCommandLogs(t, router, sess.ID, "?limit=0")
	if len(items) < 1 {
		t.Fatalf("expected at least one item")
	}
}
