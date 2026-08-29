package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoadTimezoneFromEnv(t *testing.T) {
	t.Setenv(TimezoneEnv, "")
	if loc, err := LoadTimezoneFromEnv(); err != nil || loc != time.UTC {
		t.Fatalf("unset: %v, %v; want UTC", loc, err)
	}
	t.Setenv(TimezoneEnv, "  Asia/Tokyo ")
	loc, err := LoadTimezoneFromEnv()
	if err != nil || loc.String() != "Asia/Tokyo" {
		t.Fatalf("Asia/Tokyo: %v, %v", loc, err)
	}
	t.Setenv(TimezoneEnv, "Mars/Olympus")
	if _, err := LoadTimezoneFromEnv(); err == nil {
		t.Fatal("unknown zone accepted")
	}
}

// Every client learns the server-wide zone from /api/me so on-screen
// times match the logs; nothing exposes a way to change it per user.
func TestApp_MeReportsServerTimezone(t *testing.T) {
	t.Setenv(TimezoneEnv, "Asia/Tokyo")
	app := newTestApp(t)
	router := app.NewRouter()
	sess, _ := app.SessionStore.Create("admin")
	cookie := &http.Cookie{Name: "vantyx_session", Value: sess.ID, Path: "/"}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodGet, "/api/me", nil, sess.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("/api/me: %d", w.Code)
	}
	if tz := decodeJSON(t, w)["timezone"]; tz != "Asia/Tokyo" {
		t.Fatalf("timezone = %v, want Asia/Tokyo", tz)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, jsonReq(t, http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "Admin123!"}, ""))
	if tz := decodeJSON(t, w)["timezone"]; tz != "Asia/Tokyo" {
		t.Fatalf("login timezone = %v, want Asia/Tokyo", tz)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/me/timezone"},
		{http.MethodPut, "/api/settings/timezone"},
		{http.MethodGet, "/api/settings/timezone"},
	} {
		req := jsonReq(t, tc.method, tc.path, map[string]string{"timezone": "UTC"}, "")
		req.AddCookie(cookie)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Fatalf("%s %s still served (%d)", tc.method, tc.path, w.Code)
		}
	}
}

func TestApp_InvalidTimezoneFallsBackToUTC(t *testing.T) {
	t.Setenv(TimezoneEnv, "Not/AZone")
	app := newTestApp(t)
	if app.displayTimezone() != "UTC" {
		t.Fatalf("displayTimezone = %q, want UTC", app.displayTimezone())
	}
}
