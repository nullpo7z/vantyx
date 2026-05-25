package httpapi

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	loginRateLimitWindow = 15 * time.Minute
	loginRateLimitN      = 5
)

// loginRateLimiter throttles failed login attempts per IP to mitigate
// brute-force attacks (OWASP ASVS V2.5).
//
// Counts older than the configured window are pruned on each [allow]
// call so the data structure stays bounded. The trip threshold can be
// overridden via VANTYX_LOGIN_RATE_LIMIT_N.
type loginRateLimiter struct {
	mu     sync.Mutex
	byIP   map[string][]time.Time
	window time.Duration
	maxTry int
}

// newLoginRateLimiter constructs a [loginRateLimiter] using
// VANTYX_LOGIN_RATE_LIMIT_N when set, or [loginRateLimitN] otherwise.
func newLoginRateLimiter() *loginRateLimiter {
	maxTry := loginRateLimitN
	if n := strings.TrimSpace(os.Getenv("VANTYX_LOGIN_RATE_LIMIT_N")); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			maxTry = v
		}
	}
	return &loginRateLimiter{
		byIP:   make(map[string][]time.Time),
		window: loginRateLimitWindow,
		maxTry: maxTry,
	}
}

// clientIP returns the source IP for rate-limit bookkeeping.
//
// By default RemoteAddr is used. When VANTYX_TRUST_X_FORWARDED_FOR=1
// the first entry from X-Forwarded-For is honoured instead, which is
// only safe when Vantyx sits behind a trusted reverse proxy.
func (l *loginRateLimiter) clientIP(r *http.Request) string {
	if strings.TrimSpace(os.Getenv("VANTYX_TRUST_X_FORWARDED_FOR")) == "1" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.Index(xff, ","); i > 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "" {
		return host
	}
	return r.RemoteAddr
}

// allow reports whether another login attempt from ip should be
// permitted. It also prunes expired entries.
func (l *loginRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	times := l.byIP[ip]
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[n] = t
			n++
		}
	}
	times = times[:n]
	l.byIP[ip] = times
	return len(times) < l.maxTry
}

// recordFailure increments the failure counter for ip.
func (l *loginRateLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byIP[ip] = append(l.byIP[ip], time.Now())
}
