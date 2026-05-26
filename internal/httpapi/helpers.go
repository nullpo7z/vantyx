package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/nullpo7z/vantyx/internal/i18n"
)

// errorResponse is the JSON shape returned by [writeJSONError].
type errorResponse struct {
	Message string `json:"message"`
}

// writeJSONError writes a JSON-encoded error response with the given
// HTTP status code.
//
// Note: this helper takes an already-prepared message string for
// backwards compatibility. New handlers should prefer
// [writeJSONErrorKey] so the response is localized based on the
// caller's resolved locale.
func writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{Message: message})
}

// writeJSONErrorKey writes a JSON-encoded error response, looking up
// the message in the i18n catalog using the locale resolved for the
// current request. Optional `vars` are key/value pairs substituted
// into `{name}` placeholders inside the template; the variadic surface
// is part of the public contract so handlers can localize dynamic
// messages later without changing the helper signature.
//
//nolint:unparam // `vars` is intentionally variadic for future callers.
func writeJSONErrorKey(w http.ResponseWriter, r *http.Request, key string, code int, vars ...any) {
	writeJSONError(w, i18n.TR(r, key, vars...), code)
}

// writeInternalError audits err and returns a generic 500 response so
// the client never sees raw error text (OWASP ASVS V8.1).
//
// The body is intentionally always English: the generic "internal
// error" wording is not user-actionable, and keeping the legacy
// `(w, err)` signature lets the entire codebase continue to compile
// while the localized key-based helpers are rolled out gradually.
// When this helper needs to be localized, switch to
// [writeJSONErrorKey] with the `common.internalError` key.
func writeInternalError(w http.ResponseWriter, err error) {
	audit("internal_error", auditFields{
		"error": err.Error(),
	})
	writeJSONError(w, "internal error", http.StatusInternalServerError)
}

// writeServiceUnavailableError audits err and returns a generic 503
// response so the client never sees raw error text. See
// [writeInternalError] for the rationale behind keeping the English
// body and the legacy signature.
func writeServiceUnavailableError(w http.ResponseWriter, err error) {
	audit("service_unavailable", auditFields{
		"error": err.Error(),
	})
	writeJSONError(w, "service unavailable", http.StatusServiceUnavailable)
}

// isLoopbackHost reports whether host is 127.0.0.1, localhost, or [::1]
// (with an optional port). Used to decide whether to set the cookie
// Secure flag on local / E2E runs.
func isLoopbackHost(host string) bool {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	hostname = strings.TrimPrefix(strings.TrimSuffix(hostname, "]"), "[")
	return hostname == "127.0.0.1" || hostname == "localhost" || hostname == "::1"
}

// staticDirForTest overrides [staticDir] in tests; set to a temp dir
// containing index.html to cover the SPA fallback branch.
var staticDirForTest string

// staticDir returns "web/dist" when it exists as a directory, otherwise
// an empty string.
func staticDir() string {
	if staticDirForTest != "" {
		return staticDirForTest
	}
	dir := "web/dist"
	if d, err := os.Stat(dir); err == nil && d.IsDir() {
		return dir
	}
	return ""
}
