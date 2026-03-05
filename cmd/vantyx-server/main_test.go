package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nullpo7z/vantyx/internal/httpapi"
)

func TestHealthz(t *testing.T) {
	app := httpapi.NewApp()
	router := app.NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestRedirectToHTTPS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
	req.Host = "example.com:80"
	w := httptest.NewRecorder()

	redirectToHTTPS(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if loc != "https://example.com/api/login" {
		t.Fatalf("expected Location https://example.com/api/login, got %s", loc)
	}
}
