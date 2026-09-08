package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Retention policy: age-based purge of recordings (rows + media files +
// export artifacts), audit log rows, command log rows and long-expired
// memberships. Everything is opt-in via environment (empty / 0 = keep
// forever) so an upgrade never starts deleting data on its own. The job
// runs shortly after start-up and then hourly; admins can inspect the
// policy and trigger a run from System settings.
//
//	VANTYX_RECORDING_RETENTION          e.g. 2160h (90 days)
//	VANTYX_AUDIT_RETENTION              e.g. 8760h (1 year)
//	VANTYX_COMMAND_LOG_RETENTION        defaults to the audit retention
//	VANTYX_MEMBERSHIP_EXPIRED_RETENTION how long expired membership rows
//	                                    stay listed (default 720h)

const (
	retentionEnvRecordings  = "VANTYX_RECORDING_RETENTION"
	retentionEnvAudit       = "VANTYX_AUDIT_RETENTION"
	retentionEnvCommandLogs = "VANTYX_COMMAND_LOG_RETENTION"
	retentionEnvMemberships = "VANTYX_MEMBERSHIP_EXPIRED_RETENTION"
	retentionInterval       = time.Hour
	retentionInitialDelay   = time.Minute
	retentionMinimum        = time.Hour
	retentionDefaultMembers = 30 * 24 * time.Hour
)

type retentionPolicy struct {
	Recordings  time.Duration
	Audit       time.Duration
	CommandLogs time.Duration
	Memberships time.Duration
}

func parseRetentionEnv(name string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	if v == "0" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	if d < retentionMinimum {
		return retentionMinimum
	}
	return d
}

func retentionPolicyFromEnv() retentionPolicy {
	p := retentionPolicy{
		Recordings:  parseRetentionEnv(retentionEnvRecordings, 0),
		Audit:       parseRetentionEnv(retentionEnvAudit, 0),
		Memberships: parseRetentionEnv(retentionEnvMemberships, retentionDefaultMembers),
	}
	p.CommandLogs = parseRetentionEnv(retentionEnvCommandLogs, p.Audit)
	return p
}

func (p retentionPolicy) enabled() bool {
	return p.Recordings > 0 || p.Audit > 0 || p.CommandLogs > 0 || p.Memberships > 0
}

// retentionReport is what one purge run did.
type retentionReport struct {
	RanAt              time.Time `json:"ran_at"`
	Trigger            string    `json:"trigger"`
	RecordingsDeleted  int       `json:"recordings_deleted"`
	RecordingFilesGone int       `json:"recording_files_removed"`
	AuditRowsDeleted   int64     `json:"audit_rows_deleted"`
	CommandRowsDeleted int64     `json:"command_rows_deleted"`
	MembershipsPurged  int64     `json:"memberships_purged"`
	Errors             []string  `json:"errors,omitempty"`
}

type retentionState struct {
	mu      sync.Mutex
	policy  retentionPolicy
	last    *retentionReport
	running bool
}

func (a *App) retentionPolicy() retentionPolicy {
	if a.retention == nil {
		return retentionPolicy{}
	}
	return a.retention.policy
}

// startRetentionLoop schedules purge runs when any policy is enabled.
func (a *App) startRetentionLoop() {
	if a.retention == nil {
		a.retention = &retentionState{policy: retentionPolicyFromEnv()}
	}
	if !a.retention.policy.enabled() || a.DB == nil {
		return
	}
	go func() {
		time.Sleep(retentionInitialDelay)
		a.runRetention(context.Background(), "scheduled")
		t := time.NewTicker(retentionInterval)
		defer t.Stop()
		for range t.C {
			a.runRetention(context.Background(), "scheduled")
		}
	}()
}

// runRetention applies the policy once. Concurrent runs are collapsed.
func (a *App) runRetention(ctx context.Context, trigger string) *retentionReport {
	if a.retention == nil {
		a.retention = &retentionState{policy: retentionPolicyFromEnv()}
	}
	st := a.retention
	st.mu.Lock()
	if st.running {
		last := st.last
		st.mu.Unlock()
		return last
	}
	st.running = true
	policy := st.policy
	st.mu.Unlock()

	rep := &retentionReport{RanAt: time.Now(), Trigger: trigger}
	now := time.Now()
	if a.DB != nil && policy.Recordings > 0 {
		a.purgeOldRecordings(ctx, now.Add(-policy.Recordings), rep)
	}
	if a.DB != nil && policy.Audit > 0 {
		res, err := a.DB.ExecContext(ctx, `DELETE FROM audit_logs WHERE time < ?`, now.Add(-policy.Audit).UTC())
		if err != nil {
			rep.Errors = append(rep.Errors, "audit_logs: "+err.Error())
		} else {
			rep.AuditRowsDeleted, _ = res.RowsAffected()
		}
	}
	if a.DB != nil && policy.CommandLogs > 0 {
		res, err := a.DB.ExecContext(ctx, `DELETE FROM command_logs WHERE time < ?`, now.Add(-policy.CommandLogs).UTC())
		if err != nil {
			rep.Errors = append(rep.Errors, "command_logs: "+err.Error())
		} else {
			rep.CommandRowsDeleted, _ = res.RowsAffected()
		}
	}
	if a.AccessGroupStore != nil && policy.Memberships > 0 {
		n, err := a.AccessGroupStore.PurgeExpiredMemberships(ctx, now.Add(-policy.Memberships))
		if err != nil {
			rep.Errors = append(rep.Errors, "memberships: "+err.Error())
		} else {
			rep.MembershipsPurged = n
		}
	}

	st.mu.Lock()
	st.last = rep
	st.running = false
	st.mu.Unlock()

	if trigger != "scheduled" || rep.RecordingsDeleted > 0 || rep.AuditRowsDeleted > 0 || rep.CommandRowsDeleted > 0 || rep.MembershipsPurged > 0 || len(rep.Errors) > 0 {
		audit("retention_purge", auditFields{
			"trigger":                 trigger,
			"recordings_deleted":      rep.RecordingsDeleted,
			"recording_files_removed": rep.RecordingFilesGone,
			"audit_rows_deleted":      rep.AuditRowsDeleted,
			"command_rows_deleted":    rep.CommandRowsDeleted,
			"memberships_purged":      rep.MembershipsPurged,
			"errors":                  rep.Errors,
		})
	}
	return rep
}

// purgeOldRecordings removes recordings that ended before cutoff: DB row
// first (so the recording is unreachable even if the file lingers), then
// the media file and any export artifacts, mirroring the manual delete.
func (a *App) purgeOldRecordings(ctx context.Context, cutoff time.Time, rep *retentionReport) {
	bound := cutoff.UTC().Format("2006-01-02 15:04:05")
	rows, err := a.DB.QueryContext(ctx, `SELECT id, file_path FROM recordings WHERE ended_at IS NOT NULL AND ended_at <> '' AND ended_at < ? ORDER BY ended_at LIMIT 500`, bound)
	if err != nil {
		rep.Errors = append(rep.Errors, "recordings: "+err.Error())
		return
	}
	type rec struct{ id, path string }
	var victims []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.path); err == nil {
			victims = append(victims, r)
		}
	}
	rows.Close()
	for _, v := range victims {
		if _, err := a.DB.ExecContext(ctx, `DELETE FROM recordings WHERE id = ?`, v.id); err != nil {
			rep.Errors = append(rep.Errors, "recording "+v.id+": "+err.Error())
			continue
		}
		rep.RecordingsDeleted++
		if removed, ferr := removeRecordingFile(v.path); ferr != nil {
			rep.Errors = append(rep.Errors, "file "+v.path+": "+ferr.Error())
		} else if removed {
			rep.RecordingFilesGone++
		}
		if a.RecordingExports != nil {
			for _, job := range a.RecordingExports.removeForRecording(v.id) {
				if job.State == recordingExportQueued || job.State == recordingExportRunning {
					a.requestCancelRecordingExport(job)
				}
				if job.OutputPath != "" {
					_ = os.Remove(job.OutputPath)
				}
			}
		}
	}
}

type retentionPolicyResponse struct {
	Recordings  string           `json:"recordings"` // Go duration or "" when disabled
	Audit       string           `json:"audit"`
	CommandLogs string           `json:"command_logs"`
	Memberships string           `json:"memberships_expired"`
	Enabled     bool             `json:"enabled"`
	LastRun     *retentionReport `json:"last_run,omitempty"`
}

func durationOrEmpty(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

// handleGetRetention: GET /api/settings/retention (admin).
func (a *App) handleGetRetention(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	p := a.retentionPolicy()
	resp := retentionPolicyResponse{
		Recordings:  durationOrEmpty(p.Recordings),
		Audit:       durationOrEmpty(p.Audit),
		CommandLogs: durationOrEmpty(p.CommandLogs),
		Memberships: durationOrEmpty(p.Memberships),
		Enabled:     p.enabled(),
	}
	if a.retention != nil {
		a.retention.mu.Lock()
		resp.LastRun = a.retention.last
		a.retention.mu.Unlock()
	}
	writeJSON(w, resp)
}

// handleRunRetention: POST /api/settings/retention/run (admin) applies
// the policy immediately and returns the report.
func (a *App) handleRunRetention(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	rep := a.runRetention(r.Context(), "manual:"+a.currentUserID(r))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}
