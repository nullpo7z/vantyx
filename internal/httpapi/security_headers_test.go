package httpapi

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSecurityHeadersMiddleware_HTTPNoHSTS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := SecurityHeadersMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if res.Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must not be set on plain HTTP")
	}
	assertSecurityHeaders(t, res.Header)
}

func TestSecurityHeadersMiddleware_HTTPSSetsHSTS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := SecurityHeadersMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	res := w.Result()
	if res.Header.Get("Strict-Transport-Security") == "" {
		t.Error("missing HSTS on HTTPS")
	}
	assertSecurityHeaders(t, res.Header)
}

func TestNewRouter_SecurityHeadersOnIndex(t *testing.T) {
	app := newTestApp(t)
	router := app.NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz status=%d", w.Code)
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("router must apply CSP on /healthz")
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff on /healthz")
	}
}

func assertSecurityHeaders(t *testing.T, h http.Header) {
	t.Helper()
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options")
	}
	if h.Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options")
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Error("missing CSP")
	}
	if h.Get("Permissions-Policy") == "" {
		t.Error("missing Permissions-Policy")
	}
	if h.Get("Cross-Origin-Opener-Policy") != "same-origin" {
		t.Error("missing COOP")
	}
	if h.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Error("missing CORP")
	}
	if h.Get("Cross-Origin-Embedder-Policy") != "unsafe-none" {
		t.Error("missing COEP")
	}
}

func TestCSP_InlineScriptHashesWhitelisted(t *testing.T) {
	htmlPath := filepath.Join("..", "..", "web", "index.html")
	raw, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Skipf("web/index.html: %v", err)
	}
	re := regexp.MustCompile(`(?s)<script>(.*?)</script>`)
	matches := re.FindAllSubmatch(raw, -1)
	if len(matches) == 0 {
		t.Skip("no inline scripts")
	}
	for i, m := range matches {
		sum := sha256.Sum256(m[1])
		hash := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
		token := fmt.Sprintf("'%s'", hash)
		if !strings.Contains(DefaultCSP, token) {
			t.Errorf("inline script[%d] hash %s not in DefaultCSP", i, hash)
		}
	}
}
