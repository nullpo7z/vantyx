package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
)

// errorResponse is the JSON shape returned by [writeJSONError].
type errorResponse struct {
	Message string `json:"message"`
}

// writeJSONError writes a JSON-encoded error response with the given
// HTTP status code.
func writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{Message: message})
}

// writeInternalError audits err and returns a generic 500 response so
// the client never sees raw error text (OWASP ASVS V8.1).
func writeInternalError(w http.ResponseWriter, err error) {
	audit("internal_error", auditFields{
		"error": err.Error(),
	})
	writeJSONError(w, "internal error", http.StatusInternalServerError)
}

// writeServiceUnavailableError audits err and returns a generic 503
// response so the client never sees raw error text.
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
