package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/i18n"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
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

// writeProxyError responds with a localized message for proxy dial /
// bridge failures wrapped by [proxyerrors.WrapTCPDialError]. Plain
// errors (no [proxyerrors.UserFacingError] in the chain) fall back to
// the raw English text: this only happens for transport-level errors
// that bypass our wrapper, where preserving the underlying detail is
// more useful than masking it.
func writeProxyError(w http.ResponseWriter, r *http.Request, err error, code int) {
	if key, vars, ok := proxyerrors.BridgeErrorKey(err); ok {
		writeJSONErrorKey(w, r, key, code, vars...)
		return
	}
	writeJSONError(w, proxyerrors.BridgeErrorMessage(err), code)
}

// writeAccessValidationError responds with a localized 400 when err is
// one of the [access.Err*] validation sentinels. The boolean reports
// whether the error matched: callers should fall back to
// [writeInternalError] when it returns false so DB / encryption / I/O
// failures are not surfaced verbatim.
func writeAccessValidationError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, access.ErrGroupIDEmpty):
		writeJSONErrorKey(w, r, "validation.groupIDEmpty", http.StatusBadRequest)
	case errors.Is(err, access.ErrGroupIDTooLong):
		writeJSONErrorKey(w, r, "validation.groupIDTooLong", http.StatusBadRequest)
	case errors.Is(err, access.ErrGroupIDInvalid):
		writeJSONErrorKey(w, r, "validation.groupIDInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrTargetIDEmpty):
		writeJSONErrorKey(w, r, "validation.targetIDEmpty", http.StatusBadRequest)
	case errors.Is(err, access.ErrTargetIDTooLong):
		writeJSONErrorKey(w, r, "validation.targetIDTooLong", http.StatusBadRequest)
	case errors.Is(err, access.ErrTargetIDInvalid):
		writeJSONErrorKey(w, r, "validation.targetIDInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrNameEmpty):
		writeJSONErrorKey(w, r, "validation.nameEmpty", http.StatusBadRequest)
	case errors.Is(err, access.ErrNameTooLong):
		writeJSONErrorKey(w, r, "validation.nameTooLong", http.StatusBadRequest)
	case errors.Is(err, access.ErrNameInvalid):
		writeJSONErrorKey(w, r, "validation.nameInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrHostEmpty):
		writeJSONErrorKey(w, r, "validation.hostEmpty", http.StatusBadRequest)
	case errors.Is(err, access.ErrHostTooLong):
		writeJSONErrorKey(w, r, "validation.hostTooLong", http.StatusBadRequest)
	case errors.Is(err, access.ErrHostInvalid):
		writeJSONErrorKey(w, r, "validation.hostInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrProtocolInvalid):
		writeJSONErrorKey(w, r, "targets.protocolInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrTagLength):
		writeJSONErrorKey(w, r, "tags.lengthInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrTagChars):
		writeJSONErrorKey(w, r, "tags.charsInvalid", http.StatusBadRequest)
	default:
		return false
	}
	return true
}

// localizedBridgeMessage returns the locale-appropriate user-facing
// message for a proxy bridge error. Used by non-HTTP surfaces (e.g. the
// terminal WebSocket close frame) where there is no `*http.Request` to
// hand to [writeJSONErrorKey] but a [context.Context] carries the
// locale resolved by the session middleware.
func localizedBridgeMessage(ctx context.Context, err error) string {
	if key, vars, ok := proxyerrors.BridgeErrorKey(err); ok {
		return i18n.TC(ctx, key, vars...)
	}
	return proxyerrors.BridgeErrorMessage(err)
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
