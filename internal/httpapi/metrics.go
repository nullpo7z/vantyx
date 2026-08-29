package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/metrics"
)

// Prometheus exposition at GET /metrics. Scrapes authenticate with
// either an admin session cookie or `Authorization: Bearer
// $VANTYX_METRICS_TOKEN`; without a token configured, only admins can
// read it. Nothing user-identifying is exported -- counters are keyed
// by method / status class / audit event name only.

const metricsTokenEnv = "VANTYX_METRICS_TOKEN"

// observeHTTPMetrics is called by the request logging middleware.
func observeHTTPMetrics(method string, status int, dur time.Duration, path string) {
	if path == "/metrics" || path == "/healthz" {
		return
	}
	class := "2xx"
	switch {
	case status >= 500:
		class = "5xx"
	case status >= 400:
		class = "4xx"
	case status >= 300:
		class = "3xx"
	}
	metrics.Default.Inc("vantyx_http_requests_total", "HTTP requests by method and status class.", metrics.Labels{{"method", method}, {"status", class}}, 1)
	metrics.Default.Observe("vantyx_http_request_duration_seconds", "HTTP request latency.", metrics.Labels{{"method", method}}, dur)
}

// observeAuditMetrics is called by audit() for every event; a few
// security-relevant events also feed dedicated counters.
func observeAuditMetrics(event string) {
	if event == "http_request" {
		return // already covered by the HTTP counters, and very chatty
	}
	metrics.Default.Inc("vantyx_audit_events_total", "Audit events by name.", metrics.Labels{{"event", event}}, 1)
	switch event {
	case "login_ok", "login_success":
		metrics.Default.Inc("vantyx_logins_total", "Web logins by outcome.", metrics.Labels{{"result", "success"}}, 1)
	case "login_failed", "login_totp_failed":
		metrics.Default.Inc("vantyx_logins_total", "Web logins by outcome.", metrics.Labels{{"result", "failure"}}, 1)
	case "login_rate_limited":
		metrics.Default.Inc("vantyx_logins_total", "Web logins by outcome.", metrics.Labels{{"result", "rate_limited"}}, 1)
	case "oidc_login_ok":
		metrics.Default.Inc("vantyx_logins_total", "Web logins by outcome.", metrics.Labels{{"result", "oidc_success"}}, 1)
	case "oidc_login_failed":
		metrics.Default.Inc("vantyx_logins_total", "Web logins by outcome.", metrics.Labels{{"result", "oidc_failure"}}, 1)
	case "session_terminated_by_admin", "session_watched_by_admin", "access_request_created", "access_request_approved", "access_request_denied", "retention_purge", "user_role_update", "totp_enabled", "totp_disabled", "totp_reset_by_admin":
		metrics.Default.Inc("vantyx_security_events_total", "Security-relevant administrative events.", metrics.Labels{{"event", event}}, 1)
	}
}

// registerMetricsGauges wires scrape-time gauges to the App's state.
// Called once from NewApp; gauges read live data at each scrape.
func (a *App) registerMetricsGauges() {
	a.metricsReg = metrics.New()
	reg := a.metricsReg
	reg.Gauge("vantyx_active_sessions", "Live sessions by kind.", func() []metrics.Sample {
		term, vnc, rdp := 0, 0, 0
		if l, ok := a.TerminalSessionManager.(listTerminalSessions); ok {
			term = len(l.ActiveIDs())
		}
		if a.VNCSessionManager != nil {
			vnc = len(a.VNCSessionManager.ActiveIDs())
		}
		if a.RDPVNCManager != nil {
			rdp = len(a.RDPVNCManager.AllSessions())
		}
		return []metrics.Sample{
			{Labels: metrics.Labels{{"kind", "terminal"}}, Value: float64(term)},
			{Labels: metrics.Labels{{"kind", "vnc"}}, Value: float64(vnc)},
			{Labels: metrics.Labels{{"kind", "rdp"}}, Value: float64(rdp)},
		}
	})
	reg.Gauge("vantyx_sharing_rooms", "Collaborative session rooms currently open.", func() []metrics.Sample {
		n := 0
		if a.SharingRegistry != nil {
			n = a.SharingRegistry.Len()
		}
		return []metrics.Sample{{Value: float64(n)}}
	})
	reg.Gauge("vantyx_users_total", "Registered users by role.", func() []metrics.Sample {
		admins, users := 0, 0
		if list, err := a.UserStore.ListUsers(1000, 0); err == nil {
			for _, u := range list {
				if u != nil && u.Role == "admin" {
					admins++
				} else {
					users++
				}
			}
		}
		return []metrics.Sample{
			{Labels: metrics.Labels{{"role", "admin"}}, Value: float64(admins)},
			{Labels: metrics.Labels{{"role", "user"}}, Value: float64(users)},
		}
	})
	reg.Gauge("vantyx_targets_total", "Registered targets.", func() []metrics.Sample {
		n := 0
		if a.DB != nil {
			_ = a.DB.QueryRow(`SELECT COUNT(*) FROM targets`).Scan(&n)
		}
		return []metrics.Sample{{Value: float64(n)}}
	})
	reg.Gauge("vantyx_groups_total", "Access groups.", func() []metrics.Sample {
		n := 0
		if a.AccessGroupStore != nil {
			if ids, err := a.AccessGroupStore.AllGroupIDs(contextBackground(), &access.ListOpts{Limit: 1000}); err == nil {
				n = len(ids)
			}
		}
		return []metrics.Sample{{Value: float64(n)}}
	})
	reg.Gauge("vantyx_access_requests_pending", "Access requests awaiting a decision.", func() []metrics.Sample {
		n := 0
		if a.AccessRequests != nil {
			n, _ = a.AccessRequests.CountPending(contextBackground())
		}
		return []metrics.Sample{{Value: float64(n)}}
	})
	reg.Gauge("vantyx_recording_export_jobs", "Recording export jobs by state.", func() []metrics.Sample {
		counts := map[string]int{}
		if a.RecordingExports != nil {
			counts = a.RecordingExports.countByState()
		}
		out := make([]metrics.Sample, 0, len(counts))
		for _, st := range []string{"queued", "running", "completed", "failed", "cancelled"} {
			out = append(out, metrics.Sample{Labels: metrics.Labels{{"state", st}}, Value: float64(counts[st])})
		}
		return out
	})
	reg.Gauge("vantyx_recordings_total", "Recordings stored (rows).", func() []metrics.Sample {
		n := 0
		if a.DB != nil {
			_ = a.DB.QueryRow(`SELECT COUNT(*) FROM recordings`).Scan(&n)
		}
		return []metrics.Sample{{Value: float64(n)}}
	})
	reg.Gauge("vantyx_build_info", "Build information (always 1).", func() []metrics.Sample {
		return []metrics.Sample{{Labels: metrics.Labels{{"timezone", a.displayTimezone()}}, Value: 1}}
	})
}

// handleMetrics: GET /metrics.
func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(os.Getenv(metricsTokenEnv))
	authorized := false
	if token != "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			presented := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			authorized = subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1
		}
	}
	if !authorized {
		if isAdmin, err := a.currentUserIsAdmin(r); err == nil && isAdmin {
			authorized = true
		}
	}
	if !authorized {
		if a.currentUserID(r) != "" {
			writeJSONErrorKey(w, r, "common.forbiddenAdminOnly", http.StatusForbidden)
			return
		}
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics.Default.Write(w)
	if a.metricsReg != nil {
		a.metricsReg.WriteGauges(w)
	}
}

func contextBackground() context.Context { return context.Background() }
