package httpapi

import (
	"net/http"
	"os"
	"strings"
)

// DefaultCSP is the Content-Security-Policy for the Vantyx SPA and /docs (Swagger).
//
// script-src includes SHA-256 hashes for:
//   - /docs Swagger UI bootstrap
//   - web/index.html theme/locale bootstrap (FOUC guard)
//
// wasm-unsafe-eval: asciinema-player; img-src data: noVNC cursors.
var DefaultCSP = strings.TrimSpace(
	"default-src 'self'; " +
		"script-src 'self' https://unpkg.com " +
		"'sha256-s8+L0bCTMcFupV+e7ZrRCMZiZTxI6IiNza6yMagaWHs=' " +
		"'sha256-35xcTuqYk4DXbahDhOkqFlRN1S9LOUWqKshTkAh51qQ=' 'wasm-unsafe-eval'; " +
		"style-src 'self' https://unpkg.com 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"connect-src 'self' ws: wss:; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"object-src 'none'; " +
		"form-action 'self'",
)

const (
	hstsMaxAge             = "31536000"
	hstsIncludeSubdomains  = "includeSubDomains"
	permissionsPolicyValue = "accelerometer=(), camera=(), geolocation=(), gyroscope=(), " +
		"magnetometer=(), microphone=(), payment=(), usb=()"
)

// SecurityHeadersMiddleware sets OWASP-recommended response headers on every route,
// including static assets and WebSocket upgrade responses (ASVS V8/V14).
// HSTS is sent only when r.TLS != nil (RFC 6797).
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	if strings.TrimSpace(os.Getenv("VANTYX_DISABLE_SECURITY_HEADERS")) == "1" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applySecurityHeaders(w, r.TLS != nil)
		next.ServeHTTP(w, r)
	})
}

func applySecurityHeaders(w http.ResponseWriter, https bool) {
	if https {
		w.Header().Set("Strict-Transport-Security", "max-age="+hstsMaxAge+"; "+hstsIncludeSubdomains)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", DefaultCSP)
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", permissionsPolicyValue)
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	// unsafe-none: required for noVNC/wasm and /docs (unpkg); explicit value satisfies scanners.
	w.Header().Set("Cross-Origin-Embedder-Policy", "unsafe-none")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
}
