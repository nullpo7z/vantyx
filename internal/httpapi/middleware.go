package httpapi

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nullpo7z/vantyx/internal/logging"
)

// csrfOriginMiddleware mitigates CSRF for cookie-authenticated browser
// requests by enforcing same-origin Origin / Referer on unsafe methods.
//
// Notes / hardening:
//
//   - VANTYX_DISABLE_ORIGIN_CHECK still exists for non-browser
//     automation, but it now logs a warning at startup so deployments
//     that flip it as a workaround for the "TLS terminator" gotcha
//     leave a visible footprint.
//   - The effective scheme is taken from X-Forwarded-Proto when the
//     request comes from a trusted proxy (VANTYX_TRUSTED_PROXIES); the
//     legacy "r.TLS only" path that broke nginx termination is gone.
//   - /ws/* is excluded because the WebSocket handlers run their own
//     Origin check (see websocket.go).
func csrfOriginMiddleware(next http.Handler) http.Handler {
	if strings.TrimSpace(os.Getenv("VANTYX_DISABLE_ORIGIN_CHECK")) == "1" {
		csrfDisabledOnce.Do(func() {
			logging.WithComponent("httpapi.csrf").Warn(
				"CSRF Origin check disabled via VANTYX_DISABLE_ORIGIN_CHECK; this is unsafe for browser-facing deployments",
			)
		})
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/ws/") {
			next.ServeHTTP(w, r)
			return
		}
		// /api/login is exempt because it has no cookie yet; the
		// handler itself calls sameOriginRequest() to keep the
		// equivalent protection.
		if r.URL.Path == "/api/login" {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie("vantyx_session"); err != nil || c.Value == "" {
			// Cookie-less unsafe /api/* calls (except /api/login) still
			// require a same-origin Origin/Referer so future auth modes
			// cannot bypass CSRF checks silently.
			if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/login" {
				if !sameOriginRequest(r) {
					writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		}
		if !sameOriginRequest(r) {
			writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var csrfDisabledOnce sync.Once

// maxBodyBytesMiddleware applies a global request body cap to mitigate
// memory-based DoS. Multipart uploads are exempted because they have
// their own [http.MaxBytesReader] enforcement in the file handlers.
func maxBodyBytesMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip multipart uploads (they enforce their own MaxBytesReader limits).
			if ct := r.Header.Get("Content-Type"); strings.Contains(ct, "multipart/form-data") {
				next.ServeHTTP(w, r)
				return
			}
			if limit > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// responseWriter wraps [http.ResponseWriter] to record the status code
// for access logging. It implements [http.Hijacker] by delegating to
// the underlying writer so WebSocket upgrade works.
type responseWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader implements [http.ResponseWriter] and captures the status.
func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack implements [http.Hijacker].
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("responseWriter: underlying ResponseWriter does not implement http.Hijacker")
}

// requestLog is the access log + audit log middleware. Every request is
// emitted as a structured slog "http" record, and API / WebSocket calls
// are also appended to the persistent audit log.
func (a *App) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrap := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrap, r)
		// Default to RemoteAddr (the IP we actually observe); only
		// honour XFF when the peer is in the trusted-proxy CIDR set
		// so audit forensics are not undermined by header spoofing
		// (CWE-348).
		remote := r.RemoteAddr
		if trustForwardedFor(r) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				if i := strings.Index(xff, ","); i > 0 {
					remote = strings.TrimSpace(xff[:i])
				} else {
					remote = strings.TrimSpace(xff)
				}
			}
		}
		dur := time.Since(start).Round(time.Millisecond)
		observeHTTPMetrics(r.Method, wrap.status, dur, r.URL.Path)
		httpLogger.Info("http",
			logging.KeyMethod, r.Method,
			logging.KeyPath, r.URL.Path,
			logging.KeyStatus, wrap.status,
			logging.KeyRemote, remote,
			logging.KeyDurationMS, dur.Milliseconds(),
		)

		// Persistent audit log for every API call (GUI / CLI / API share these endpoints).
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
			userID := ""
			if a != nil {
				userID = strings.TrimSpace(a.currentUserID(r))
			}
			audit("http_request", auditFields{
				"user_id":      userID,
				"method":       r.Method,
				"path":         r.URL.Path,
				"status":       wrap.status,
				"remote":       remote,
				"duration_ms":  dur.Milliseconds(),
				"query":        r.URL.RawQuery,
				"user_agent":   r.UserAgent(),
				"content_type": r.Header.Get("Content-Type"),
			})
		}
	})
}
