package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	orig := os.Getenv("VANTYX_ALLOWED_HOSTS")
	os.Setenv("VANTYX_ALLOWED_HOSTS", "example.com")
	defer func() { _ = os.Setenv("VANTYX_ALLOWED_HOSTS", orig) }()

	handler := redirectToHTTPSHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
	req.Host = "example.com:80"
	w := httptest.NewRecorder()

	handler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if loc != "https://example.com:8443/api/login" {
		t.Fatalf("expected Location https://example.com:8443/api/login, got %s", loc)
	}
}

func TestRedirectToHTTPS_ExternalHost(t *testing.T) {
	orig := os.Getenv("VANTYX_EXTERNAL_HOST")
	os.Setenv("VANTYX_EXTERNAL_HOST", "app.example.com")
	defer func() { _ = os.Setenv("VANTYX_EXTERNAL_HOST", orig) }()
	origAllowed := os.Getenv("VANTYX_ALLOWED_HOSTS")
	os.Unsetenv("VANTYX_ALLOWED_HOSTS")
	defer func() { _ = os.Setenv("VANTYX_ALLOWED_HOSTS", origAllowed) }()

	handler := redirectToHTTPSHandler()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.Host = "any.host:80"
	w := httptest.NewRecorder()

	handler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if loc != "https://app.example.com/foo" {
		t.Fatalf("expected Location https://app.example.com/foo, got %s", loc)
	}
}

func TestRedirectToHTTPS_RejectHost(t *testing.T) {
	origAllowed := os.Getenv("VANTYX_ALLOWED_HOSTS")
	os.Setenv("VANTYX_ALLOWED_HOSTS", "allowed.com")
	defer func() { _ = os.Setenv("VANTYX_ALLOWED_HOSTS", origAllowed) }()
	origExt := os.Getenv("VANTYX_EXTERNAL_HOST")
	os.Unsetenv("VANTYX_EXTERNAL_HOST")
	defer func() { _ = os.Setenv("VANTYX_EXTERNAL_HOST", origExt) }()

	handler := redirectToHTTPSHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.com:80"
	w := httptest.NewRecorder()

	handler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestRedirectToHTTPS_EmptyRequestHost(t *testing.T) {
	origAllowed := os.Getenv("VANTYX_ALLOWED_HOSTS")
	os.Setenv("VANTYX_ALLOWED_HOSTS", "localhost")
	defer func() { _ = os.Setenv("VANTYX_ALLOWED_HOSTS", origAllowed) }()
	origExt := os.Getenv("VANTYX_EXTERNAL_HOST")
	os.Unsetenv("VANTYX_EXTERNAL_HOST")
	defer func() { _ = os.Setenv("VANTYX_EXTERNAL_HOST", origExt) }()
	origAddr := os.Getenv("VANTYX_HTTPS_ADDR")
	os.Setenv("VANTYX_HTTPS_ADDR", ":9443")
	defer func() { _ = os.Setenv("VANTYX_HTTPS_ADDR", origAddr) }()

	handler := redirectToHTTPSHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = ""
	w := httptest.NewRecorder()

	handler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if loc != "https://localhost:9443/" {
		t.Fatalf("expected Location https://localhost:9443/, got %s", loc)
	}
}

func TestCorsMiddleware_NoOrigins(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware(next)
	orig := os.Getenv("VANTYX_CORS_ALLOWED_ORIGINS")
	os.Unsetenv("VANTYX_CORS_ALLOWED_ORIGINS")
	defer func() { _ = os.Setenv("VANTYX_CORS_ALLOWED_ORIGINS", orig) }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	if w.Result().Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("should not set CORS when no origins configured")
	}
}

func TestCorsMiddleware_WithOrigin(t *testing.T) {
	orig := os.Getenv("VANTYX_CORS_ALLOWED_ORIGINS")
	os.Setenv("VANTYX_CORS_ALLOWED_ORIGINS", "https://app.example.com")
	defer func() { _ = os.Setenv("VANTYX_CORS_ALLOWED_ORIGINS", orig) }()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("expected CORS origin, got %q", res.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestCorsMiddleware_Options(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware(next)
	orig := os.Getenv("VANTYX_CORS_ALLOWED_ORIGINS")
	os.Setenv("VANTYX_CORS_ALLOWED_ORIGINS", "https://app.example.com")
	defer func() { _ = os.Setenv("VANTYX_CORS_ALLOWED_ORIGINS", orig) }()

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Result().StatusCode)
	}
}

func TestLoadOrGenerateCert_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	// Generate to create valid cert+key files
	_, err := loadOrGenerateCert(certFile, keyFile)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Load existing (should not regenerate)
	cert, err := loadOrGenerateCert(certFile, keyFile)
	if err != nil {
		t.Fatalf("load existing: %v", err)
	}
	if cert == nil {
		t.Fatal("expected cert")
	}
}

func TestLoadOrGenerateCert_InvalidExisting(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certFile, []byte("not pem"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("not pem"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrGenerateCert(certFile, keyFile)
	if err == nil {
		t.Fatal("expected error loading invalid cert")
	}
}

func TestLoadOrGenerateCert_Generate(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")

	cert, err := loadOrGenerateCert(certFile, keyFile)
	if err != nil {
		t.Fatalf("loadOrGenerateCert: %v", err)
	}
	if cert == nil {
		t.Fatal("expected cert")
	}
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Error("cert file not created")
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Error("key file not created")
	}
}

func TestLoadOrGenerateCert_OneExists(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := loadOrGenerateCert(certFile, keyFile)
	if err != os.ErrNotExist {
		t.Fatalf("expected ErrNotExist when only cert exists, got %v", err)
	}
}

func TestCollectCertSANs(t *testing.T) {
	orig := os.Getenv("VANTYX_TLS_SANS")
	defer func() { _ = os.Setenv("VANTYX_TLS_SANS", orig) }()

	os.Unsetenv("VANTYX_TLS_SANS")
	dns, ips := collectCertSANs()
	if len(dns) == 0 || len(ips) == 0 {
		t.Fatalf("expected default localhost SANs, got dns=%v ips=%v", dns, ips)
	}
	found := false
	for _, d := range dns {
		if d == "localhost" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected localhost in DNS SANs")
	}
}

func TestCollectCertSANs_WithEnv(t *testing.T) {
	orig := os.Getenv("VANTYX_TLS_SANS")
	os.Setenv("VANTYX_TLS_SANS", "192.168.1.1, myhost.local ,  ")
	defer func() { _ = os.Setenv("VANTYX_TLS_SANS", orig) }()

	dns, ips := collectCertSANs()
	var hasIP bool
	for _, ip := range ips {
		if ip.Equal(net.IPv4(192, 168, 1, 1)) {
			hasIP = true
			break
		}
	}
	if !hasIP {
		t.Error("expected 192.168.1.1 in IP SANs")
	}
	var hasDNS bool
	for _, d := range dns {
		if d == "myhost.local" {
			hasDNS = true
			break
		}
	}
	if !hasDNS {
		t.Error("expected myhost.local in DNS SANs")
	}
}

func TestCollectCertSANs_InvalidEntrySkipped(t *testing.T) {
	orig := os.Getenv("VANTYX_TLS_SANS")
	os.Setenv("VANTYX_TLS_SANS", "valid.local, bad name with space")
	defer func() { _ = os.Setenv("VANTYX_TLS_SANS", orig) }()

	dns, _ := collectCertSANs()
	var hasValid bool
	for _, d := range dns {
		if d == "valid.local" {
			hasValid = true
			break
		}
	}
	if !hasValid {
		t.Error("expected valid.local in DNS SANs")
	}
}
