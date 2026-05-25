package httpapi

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/logging"
)

// requestScheme returns "https" if the request was received over TLS,
// otherwise "http". Used by the CSRF check to construct the expected
// Origin value.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// csrfOriginMiddleware mitigates CSRF for cookie-authenticated browser
// requests by enforcing same-origin Origin / Referer on unsafe methods.
//
// This is intentionally lightweight (no per-session token) and can be
// disabled via VANTYX_DISABLE_ORIGIN_CHECK for non-browser automation
// that has no Origin header to send.
func csrfOriginMiddleware(next http.Handler) http.Handler {
	if strings.TrimSpace(os.Getenv("VANTYX_DISABLE_ORIGIN_CHECK")) == "1" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/ws/") || r.URL.Path == "/api/login" {
			next.ServeHTTP(w, r)
			return
		}
		// Only enforce when cookie auth is present.
		if c, err := r.Cookie("vantyx_session"); err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		want := requestScheme(r) + "://" + r.Host
		secFetchSite := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if origin != want {
				writeJSONError(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		ref := strings.TrimSpace(r.Referer())
		// If browser fetch metadata is present but we don't have Origin/Referer, treat as suspicious.
		if ref == "" && secFetchSite != "" {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
		// Non-browser clients may not send Origin/Referer. Allow in that case.
		if ref == "" {
			next.ServeHTTP(w, r)
			return
		}
		u, err := url.Parse(ref)
		if err != nil || u.Scheme == "" || u.Host == "" {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
		if u.Scheme+"://"+u.Host != want {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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
		remote := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.Index(xff, ","); i > 0 {
				remote = strings.TrimSpace(xff[:i])
			} else {
				remote = strings.TrimSpace(xff)
			}
		}
		dur := time.Since(start).Round(time.Millisecond)
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
