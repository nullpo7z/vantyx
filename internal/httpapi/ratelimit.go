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
	loginRateLimitWindow    = 15 * time.Minute
	loginRateLimitN         = 5
	loginUserRateLimitN     = 10
	rateLimiterGCInterval   = 5 * time.Minute
	rateLimiterPurgeBackoff = 30 * time.Minute
	// loginRateLimitMaxEntries caps the number of distinct IPs (and
	// usernames) tracked simultaneously, so a flood of synthetic
	// X-Forwarded-For / username values can not grow the map without
	// bound between GC sweeps (M-1 / CWE-770).
	loginRateLimitMaxEntries = 50_000
)

// loginRateLimiter throttles failed login attempts to mitigate brute
// force attacks (OWASP ASVS V2.5 / CWE-307).
//
// Two independent buckets are tracked per request:
//
//   - byIP    : per-source-IP failures, defending against high-volume
//     low-cost scanning from a single host.
//   - byUser  : per-username failures, defending against distributed
//     attacks that spread their requests across many IPs (the IP
//     bucket alone would let each IP try N times before being shut
//     down, so coordinated /16-sized botnets would still succeed).
//
// Trip thresholds are configurable via VANTYX_LOGIN_RATE_LIMIT_N (IP)
// and VANTYX_LOGIN_USER_RATE_LIMIT_N (username); both default to safe
// values defined above.
type loginRateLimiter struct {
	mu       sync.Mutex
	byIP     map[string][]time.Time
	byUser   map[string][]time.Time
	window   time.Duration
	maxIP    int
	maxUser  int
	lastSwep time.Time
}

// newLoginRateLimiter constructs a [loginRateLimiter].
func newLoginRateLimiter() *loginRateLimiter {
	maxIP := loginRateLimitN
	if n := strings.TrimSpace(os.Getenv("VANTYX_LOGIN_RATE_LIMIT_N")); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			maxIP = v
		}
	}
	maxUser := loginUserRateLimitN
	if n := strings.TrimSpace(os.Getenv("VANTYX_LOGIN_USER_RATE_LIMIT_N")); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			maxUser = v
		}
	}
	return &loginRateLimiter{
		byIP:    make(map[string][]time.Time),
		byUser:  make(map[string][]time.Time),
		window:  loginRateLimitWindow,
		maxIP:   maxIP,
		maxUser: maxUser,
	}
}

// clientIP returns the source IP for rate-limit bookkeeping.
//
// When the request originates from a trusted reverse proxy (see
// VANTYX_TRUSTED_PROXIES in helpers.go) the left-most entry of
// X-Forwarded-For is honoured. Otherwise RemoteAddr is used so an
// attacker cannot spoof their source by adding the header themselves
// (CWE-348 / CWE-290).
func (l *loginRateLimiter) clientIP(r *http.Request) string {
	if trustForwardedFor(r) {
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

func normalizeUserKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// pruneLocked drops timestamps older than cutoff and deletes empty
// bucket entries to bound memory under sustained scanning. Must be
// called with l.mu held.
func pruneLocked(m map[string][]time.Time, cutoff time.Time) {
	for k, ts := range m {
		n := 0
		for _, t := range ts {
			if t.After(cutoff) {
				ts[n] = t
				n++
			}
		}
		if n == 0 {
			delete(m, k)
			continue
		}
		m[k] = ts[:n]
	}
}

func (l *loginRateLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastSwep) < rateLimiterGCInterval {
		return
	}
	cutoff := now.Add(-rateLimiterPurgeBackoff)
	pruneLocked(l.byIP, cutoff)
	pruneLocked(l.byUser, cutoff)
	l.lastSwep = now
}

// allowIP reports whether the source IP is still under the per-IP
// budget. Pruning happens at the same time so the bucket cannot grow
// without bound.
func (l *loginRateLimiter) allowIP(ip string) bool {
	if ip == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.gcLocked(now)
	cutoff := now.Add(-l.window)
	times := l.byIP[ip]
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[n] = t
			n++
		}
	}
	if n == 0 {
		delete(l.byIP, ip)
	} else {
		l.byIP[ip] = times[:n]
	}
	return n < l.maxIP
}

// allowUser reports whether further attempts against username are
// allowed.
func (l *loginRateLimiter) allowUser(username string) bool {
	key := normalizeUserKey(username)
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.gcLocked(now)
	cutoff := now.Add(-l.window)
	times := l.byUser[key]
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[n] = t
			n++
		}
	}
	if n == 0 {
		delete(l.byUser, key)
	} else {
		l.byUser[key] = times[:n]
	}
	return n < l.maxUser
}

// recordFailureIP / recordFailureUser increment the per-IP / per-user
// counters after a failed attempt.
func (l *loginRateLimiter) recordFailureIP(ip string) {
	if ip == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// Force a sweep when the map is approaching saturation so a flood
	// of unique IPs cannot grow it without bound between scheduled
	// GCs.
	if _, present := l.byIP[ip]; !present && len(l.byIP) >= loginRateLimitMaxEntries {
		l.lastSwep = time.Time{}
		l.gcLocked(time.Now())
		if len(l.byIP) >= loginRateLimitMaxEntries {
			// Still saturated after pruning: silently drop the new
			// entry rather than commit additional memory.
			return
		}
	}
	l.byIP[ip] = append(l.byIP[ip], time.Now())
}

func (l *loginRateLimiter) recordFailureUser(username string) {
	key := normalizeUserKey(username)
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, present := l.byUser[key]; !present && len(l.byUser) >= loginRateLimitMaxEntries {
		l.lastSwep = time.Time{}
		l.gcLocked(time.Now())
		if len(l.byUser) >= loginRateLimitMaxEntries {
			return
		}
	}
	l.byUser[key] = append(l.byUser[key], time.Now())
}

// recordSuccess clears the failure buckets for an IP / user pair so
// legitimate users are not penalized after a fat-fingered password.
func (l *loginRateLimiter) recordSuccess(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ip != "" {
		delete(l.byIP, ip)
	}
	if key := normalizeUserKey(username); key != "" {
		delete(l.byUser, key)
	}
}

// allow is kept for callers that have not been migrated to
// allowIP/allowUser yet.
//
// Deprecated: use allowIP / allowUser directly so the audit / metric
// surface can distinguish IP-scope from user-scope blocks.
func (l *loginRateLimiter) allow(ip string) bool { return l.allowIP(ip) }

func (l *loginRateLimiter) recordFailure(ip string) { l.recordFailureIP(ip) }
