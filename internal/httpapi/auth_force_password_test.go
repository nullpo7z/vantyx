package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForcePasswordChangeMiddleware_BlocksWebSocket(t *testing.T) {
	app := newTestApp(t)
	_ = app.UserStore.SetForcePasswordChange("admin", true)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/ws/ssh?target_id=t1", nil)
	sess, _ := app.SessionStore.Create("admin")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for WS while force_password_change, got %d", w.Result().StatusCode)
	}
}

func TestForcePasswordChangeMiddleware_AllowsPasswordChangeAPI(t *testing.T) {
	app := newTestApp(t)
	_ = app.UserStore.SetForcePasswordChange("admin", true)
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	sess, _ := app.SessionStore.Create("admin")
	req.AddCookie(&http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /api/me during rotation, got %d", w.Result().StatusCode)
	}
}
