package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

// allowedWebSocketOrigin enforces CSWSH protection on WebSocket upgrades.
//
// It returns true when:
//
//   - The Origin header is missing and VANTYX_ALLOW_WS_NO_ORIGIN=1 with
//     a loopback peer (for local development).
//   - The origin appears in VANTYX_WS_ALLOWED_ORIGINS (exact match).
//   - The origin matches the request's own scheme + host.
func allowedWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Some non-browser clients omit Origin. Disallow by default
		// (CSWSH protection); enable explicitly for local dev / tests.
		if strings.TrimSpace(os.Getenv("VANTYX_ALLOW_WS_NO_ORIGIN")) != "1" {
			return false
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	// Explicit allow-list (comma-separated full origins like
	// https://example.com).
	if v := strings.TrimSpace(os.Getenv("VANTYX_WS_ALLOWED_ORIGINS")); v != "" {
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(s) == origin {
				return true
			}
		}
	}

	// Default: same-origin (scheme + host) only, honoring the
	// effective scheme reported by a trusted reverse proxy so a TLS
	// terminator does not require operators to populate
	// VANTYX_WS_ALLOWED_ORIGINS (which previously meant they had to
	// drop the protection entirely for browser callers).
	wantScheme := effectiveScheme(r)
	wantHost := r.Host
	if strings.EqualFold(u.Scheme, wantScheme) && strings.EqualFold(u.Host, wantHost) {
		return true
	}
	return false
}

// wsMaxInitialMessageBytes caps the size of the first WebSocket frame
// (credentials, geometry, etc.). Subsequent frames are bounded by the
// proxy-specific handlers (e.g. SSH bridges set their own limits).
//
// Using a generous-but-finite limit prevents a DoS where the upgrade
// succeeds but the next frame is a 1 GB string (CWE-770).
const wsMaxInitialMessageBytes = 64 << 10 // 64 KiB

// wsUpgrader is the shared WebSocket upgrader. CheckOrigin defers to
// [allowedWebSocketOrigin] and a server-side read buffer cap is set.
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4 << 10,
	WriteBufferSize: 4 << 10,
	CheckOrigin: func(r *http.Request) bool {
		return allowedWebSocketOrigin(r)
	},
}
