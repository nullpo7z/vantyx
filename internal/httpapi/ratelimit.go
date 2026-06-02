package httpapi

import (
	"net/http"

	"github.com/nullpo7z/vantyx/internal/ratelimit"
)

type loginRateLimiter struct {
	*ratelimit.LoginLimiter
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{LoginLimiter: ratelimit.NewLoginLimiter()}
}

func (l *loginRateLimiter) clientIP(r *http.Request) string {
	return l.ClientIPFromHTTP(r, trustForwardedFor)
}

func (l *loginRateLimiter) allowIP(ip string) bool { return l.AllowIP(ip) }

func (l *loginRateLimiter) allowUser(username string) bool { return l.AllowUser(username) }

func (l *loginRateLimiter) recordFailureIP(ip string) { l.RecordFailureIP(ip) }

func (l *loginRateLimiter) recordFailureUser(username string) { l.RecordFailureUser(username) }

func (l *loginRateLimiter) recordSuccess(ip, username string) { l.RecordSuccess(ip, username) }
