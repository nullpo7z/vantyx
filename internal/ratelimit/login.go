// Package ratelimit provides shared login brute-force throttling used
// by both the HTTP API and the CLI SSH gateway.
package ratelimit

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
	defaultWindow  = 15 * time.Minute
	defaultMaxIP   = 5
	defaultMaxUser = 10
	gcInterval     = 5 * time.Minute
	purgeBackoff   = 30 * time.Minute
	maxEntries     = 50_000
	envMaxIP       = "VANTYX_LOGIN_RATE_LIMIT_N"
	envMaxUser     = "VANTYX_LOGIN_USER_RATE_LIMIT_N"
)

// LoginLimiter throttles failed login attempts to mitigate brute force
// attacks (OWASP ASVS V2.5 / CWE-307).
type LoginLimiter struct {
	mu       sync.Mutex
	byIP     map[string][]time.Time
	byUser   map[string][]time.Time
	window   time.Duration
	maxIP    int
	maxUser  int
	lastSwep time.Time
}

// NewLoginLimiter constructs a [LoginLimiter] with defaults overridable
// via VANTYX_LOGIN_RATE_LIMIT_N and VANTYX_LOGIN_USER_RATE_LIMIT_N.
func NewLoginLimiter() *LoginLimiter {
	maxIP := defaultMaxIP
	if n := strings.TrimSpace(os.Getenv(envMaxIP)); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			maxIP = v
		}
	}
	maxUser := defaultMaxUser
	if n := strings.TrimSpace(os.Getenv(envMaxUser)); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			maxUser = v
		}
	}
	return &LoginLimiter{
		byIP:    make(map[string][]time.Time),
		byUser:  make(map[string][]time.Time),
		window:  defaultWindow,
		maxIP:   maxIP,
		maxUser: maxUser,
	}
}

// ClientIPFromHTTP returns the source IP for rate-limit bookkeeping.
func (l *LoginLimiter) ClientIPFromHTTP(r *http.Request, trustForwardedFor func(*http.Request) bool) string {
	if trustForwardedFor != nil && trustForwardedFor(r) {
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

// ClientIPFromAddr extracts the host portion from a [net.Addr].
func ClientIPFromAddr(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, _ := net.SplitHostPort(addr.String())
	if host != "" {
		return host
	}
	return addr.String()
}

func normalizeUserKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

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

func (l *LoginLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastSwep) < gcInterval {
		return
	}
	cutoff := now.Add(-purgeBackoff)
	pruneLocked(l.byIP, cutoff)
	pruneLocked(l.byUser, cutoff)
	l.lastSwep = now
}

func (l *LoginLimiter) allowBucket(m map[string][]time.Time, key string, max int) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.gcLocked(now)
	cutoff := now.Add(-l.window)
	times := m[key]
	n := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[n] = t
			n++
		}
	}
	if n == 0 {
		delete(m, key)
	} else {
		m[key] = times[:n]
	}
	return n < max
}

// AllowIP reports whether the source IP is still under the per-IP budget.
func (l *LoginLimiter) AllowIP(ip string) bool {
	return l.allowBucket(l.byIP, ip, l.maxIP)
}

// AllowUser reports whether further attempts against username are allowed.
func (l *LoginLimiter) AllowUser(username string) bool {
	return l.allowBucket(l.byUser, normalizeUserKey(username), l.maxUser)
}

func (l *LoginLimiter) recordBucket(m map[string][]time.Time, key string) {
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, present := m[key]; !present && len(m) >= maxEntries {
		l.lastSwep = time.Time{}
		l.gcLocked(time.Now())
		if len(m) >= maxEntries {
			return
		}
	}
	m[key] = append(m[key], time.Now())
}

// RecordFailureIP increments the per-IP counter after a failed attempt.
func (l *LoginLimiter) RecordFailureIP(ip string) {
	l.recordBucket(l.byIP, ip)
}

// RecordFailureUser increments the per-username counter after a failed attempt.
func (l *LoginLimiter) RecordFailureUser(username string) {
	l.recordBucket(l.byUser, normalizeUserKey(username))
}

// RecordSuccess clears the failure buckets for an IP / user pair.
func (l *LoginLimiter) RecordSuccess(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ip != "" {
		delete(l.byIP, ip)
	}
	if key := normalizeUserKey(username); key != "" {
		delete(l.byUser, key)
	}
}
