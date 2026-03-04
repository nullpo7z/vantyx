package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApp_LoginSuccess(t *testing.T) {
	app := NewApp()
	router := app.NewRouter()

	body := []byte(`{"username":"admin","password":"admin123!"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestApp_LoginFailure(t *testing.T) {
	app := NewApp()
	router := app.NewRouter()

	body := []byte(`{"username":"admin","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.StatusCode)
	}
}

func TestApp_Me_UnauthorizedWithoutCookie(t *testing.T) {
	app := NewApp()
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.StatusCode)
	}
}

func TestApp_Me_SuccessWithValidSession(t *testing.T) {
	app := NewApp()
	router := app.NewRouter()

	// Seeded admin user has ID "admin".
	sess, err := app.SessionStore.Create("admin")
	if err != nil {
		t.Fatalf("Create session returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{
		Name:  "vantyx_session",
		Value: sess.ID,
		Path:  "/",
	})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}
