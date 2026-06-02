package httpapi

import (
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/i18n"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
)

var cryptoRandReader io.Reader = cryptorand.Reader

// setAttachmentDisposition writes a Content-Disposition header that
// safely encodes filename, preventing header injection via CR/LF
// (CWE-93) or quote-breakouts (CWE-79) from attacker-controlled
// filenames. Non-ASCII names are exposed via the RFC 5987 filename*
// parameter while a sanitised ASCII fallback satisfies legacy clients.
func setAttachmentDisposition(w http.ResponseWriter, filename string) {
	clean := sanitiseDispositionFilename(filename)
	if clean == "" {
		clean = "download"
	}
	// quoted-string per RFC 6266 / RFC 7230 forbids CR/LF and `"`.
	// We already strip those in sanitiseDispositionFilename; escape
	// remaining backslashes to keep the parser happy.
	ascii := strings.ReplaceAll(clean, `\`, `\\`)
	encoded := url.PathEscape(clean)
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+ascii+`"; filename*=UTF-8''`+encoded)
}

// escapeLikeOperand escapes the SQLite/PostgreSQL LIKE wildcards
// (`%`, `_`) and the escape byte itself so caller-supplied substrings
// can be passed through `column LIKE ? ESCAPE '\'` without smuggling
// pattern operators or triggering full-table scans (M-12).
func escapeLikeOperand(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sanitiseDispositionFilename removes characters that would let an
// attacker break out of the Content-Disposition header (HTTP response
// splitting) or smuggle quote characters into the filename parameter.
func sanitiseDispositionFilename(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\r' || r == '\n' || r == '"' || r == 0:
			b.WriteByte('_')
		case r < 0x20 || r == 0x7f:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

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

// localizedMessage resolves an i18n key for non-JSON responses (for
// example WebSocket text frames).
func localizedMessage(r *http.Request, key string, vars ...any) string {
	return i18n.TR(r, key, vars...)
}

// writeJSONErrorKeyAudited returns a localized message for key without
// embedding err.Error() in the API response. err is recorded in audit
// logs for operators.
func writeJSONErrorKeyAudited(w http.ResponseWriter, r *http.Request, key string, code int, err error) {
	if err != nil {
		audit("api_error", auditFields{
			"i18n_key": key,
			"error":    err.Error(),
		})
	}
	writeJSONErrorKey(w, r, key, code)
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

// writeCredentialSchemaError maps SQLite schema drift (missing table/column)
// to a localized 503 for credential library endpoints.
func writeCredentialSchemaError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "no such table") || strings.Contains(msg, "no such column") {
		writeJSONErrorKeyAudited(w, r, "credentials.notReady", http.StatusServiceUnavailable, err)
		return true
	}
	return false
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
	if err != nil {
		audit("proxy_error", auditFields{
			"error": err.Error(),
		})
	}
	writeJSONErrorKey(w, r, "common.gatewayFailed", code)
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
	case errors.Is(err, access.ErrHostRestricted):
		writeJSONErrorKey(w, r, "validation.hostRestricted", http.StatusBadRequest)
	case errors.Is(err, access.ErrProtocolInvalid):
		writeJSONErrorKey(w, r, "targets.protocolInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrTagLength):
		writeJSONErrorKey(w, r, "tags.lengthInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrTagChars):
		writeJSONErrorKey(w, r, "tags.charsInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrHostKeyFingerprintInvalid):
		writeJSONErrorKey(w, r, "targets.hostKeyFingerprintInvalid", http.StatusBadRequest)
	case errors.Is(err, access.ErrSSHKeyLabelReq), errors.Is(err, access.ErrCredentialIdentityLabelReq):
		writeJSONErrorKey(w, r, "validation.nameEmpty", http.StatusBadRequest)
	case errors.Is(err, access.ErrSSHKeyPrivateReq):
		writeJSONErrorKey(w, r, "sshKeys.privateKeyRequired", http.StatusBadRequest)
	case errors.Is(err, access.ErrCredentialIdentityUserReq):
		writeJSONErrorKey(w, r, "credentialIdentities.usernameRequired", http.StatusBadRequest)
	case errors.Is(err, access.ErrCredentialIdentityAuthReq):
		writeJSONErrorKey(w, r, "credentialIdentities.authRequired", http.StatusBadRequest)
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
// Secure flag on local development runs.
func isLoopbackHost(host string) bool {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	hostname = strings.TrimPrefix(strings.TrimSuffix(hostname, "]"), "[")
	return hostname == "127.0.0.1" || hostname == "localhost" || hostname == "::1"
}

// auditUsernameHashKey is loaded once at startup so log forging
// attempts that leak a username into the audit table cannot be reversed
// by external attackers (CWE-532). When VANTYX_AUDIT_HMAC_KEY or
// VANTYX_SSH_PASSWORD_ENCRYPTION_KEY is set the key is stable across
// restarts so operators can correlate failed-login events offline.
var (
	auditUsernameHashOnce sync.Once
	auditUsernameHashKey  []byte
)

func initAuditUsernameHashKey() {
	if k := strings.TrimSpace(os.Getenv("VANTYX_AUDIT_HMAC_KEY")); k != "" {
		auditUsernameHashKey = []byte(k)
		return
	}
	if k := strings.TrimSpace(os.Getenv("VANTYX_SSH_PASSWORD_ENCRYPTION_KEY")); k != "" {
		sum := sha256.Sum256([]byte("vantyx-audit-username:" + k))
		auditUsernameHashKey = sum[:]
		return
	}
	auditUsernameHashKey = make([]byte, 32)
	_, _ = randReadFull(auditUsernameHashKey)
}

func auditUsernameHash(username string) string {
	auditUsernameHashOnce.Do(initAuditUsernameHashKey)
	mac := hmac.New(sha256.New, auditUsernameHashKey)
	mac.Write([]byte(strings.ToLower(strings.TrimSpace(username))))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// cookieSecure reports whether the Secure cookie attribute should be
// set for the current request. The flag is on whenever the request
// arrived over TLS *or* over a trusted reverse proxy that signals
// HTTPS via X-Forwarded-Proto: https. Loopback is exempt so local
// developer builds keep working over plain HTTP.
func cookieSecure(r *http.Request) bool {
	if isLoopbackHost(r.Host) {
		return false
	}
	return effectiveScheme(r) == "https"
}

// effectiveScheme honors X-Forwarded-Proto when the remote peer is
// within the trusted-proxy CIDR set, otherwise it falls back to the
// transport seen on the listener.
func effectiveScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if trustForwardedFor(r) {
		if proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); proto == "https" || proto == "http" {
			return proto
		}
	}
	return "http"
}

// sameOriginRequest verifies that the Origin (or, as a fall-back,
// Referer) of a state-changing request matches the listener's
// effective scheme + host. Used by /api/login which is exempted from
// the global CSRF middleware.
func sameOriginRequest(r *http.Request) bool {
	want := effectiveScheme(r) + "://" + r.Host
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return origin == want
	}
	if ref := strings.TrimSpace(r.Referer()); ref != "" {
		u, err := url.Parse(ref)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return false
		}
		return u.Scheme+"://"+u.Host == want
	}
	// Origin and Referer both missing: refuse for browsers (sec-
	// fetch-site presence indicates a browser caller) and allow for
	// non-browser tooling that has neither header.
	if strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")) != "" {
		return false
	}
	return true
}

// trustForwardedFor reports whether the request arrived from a CIDR
// listed in VANTYX_TRUSTED_PROXIES. The legacy boolean toggle
// VANTYX_TRUST_X_FORWARDED_FOR remains supported for backwards
// compatibility, but it is now restricted to loopback peers so a
// misconfigured deployment cannot trust arbitrary client headers.
func trustForwardedFor(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "" {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	cidrs := trustedProxyCIDRs()
	for _, c := range cidrs {
		if c.Contains(ip) {
			return true
		}
	}
	if os.Getenv("VANTYX_TRUST_X_FORWARDED_FOR") == "1" && ip.IsLoopback() {
		return true
	}
	return false
}

var (
	trustedProxyOnce  sync.Once
	trustedProxyCache []*net.IPNet
)

func trustedProxyCIDRs() []*net.IPNet {
	trustedProxyOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("VANTYX_TRUSTED_PROXIES"))
		if raw == "" {
			return
		}
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, "/") {
				if ip := net.ParseIP(part); ip != nil {
					if ip.To4() != nil {
						part += "/32"
					} else {
						part += "/128"
					}
				}
			}
			if _, ipnet, err := net.ParseCIDR(part); err == nil {
				trustedProxyCache = append(trustedProxyCache, ipnet)
			}
		}
	})
	return trustedProxyCache
}

// randReadFull reads len(buf) bytes from crypto/rand; declared here so
// auditUsernameHash can call it without pulling crypto/rand directly
// into the file's import set in multiple places.
var randReadFull = func(buf []byte) (int, error) {
	return cryptoRandReader.Read(buf)
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
