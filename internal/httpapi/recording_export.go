package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/recording"
)

const (
	recordingExportAcquireTimeout    = 5 * time.Minute
	recordingExportConvertTimeout    = 45 * time.Minute
	recordingExportStaleAfter        = 50 * time.Minute
	recordingExportConvertTimeoutEnv = "VANTYX_RECORDING_EXPORT_CONVERT_TIMEOUT"
	recordingExportCompletedTTL      = 7 * 24 * time.Hour
	recordingExportCompletedTTLEnv   = "VANTYX_RECORDING_EXPORT_COMPLETED_TTL"
	recordingExportOrphanTempMaxAge  = 24 * time.Hour
)

type recordingMediaAccess struct {
	RecordingID string
	MediaPath   string
	SessionID   string
	StartedAt   string
	OwnerID     string
}

type recordingExportState string

const (
	recordingExportQueued    recordingExportState = "queued"
	recordingExportRunning   recordingExportState = "running"
	recordingExportCompleted recordingExportState = "completed"
	recordingExportFailed    recordingExportState = "failed"
	recordingExportCancelled recordingExportState = "cancelled"
)

type recordingExportJob struct {
	ID                 string
	UserID             string
	RecordingID        string
	Format             string
	State              recordingExportState
	Error              string
	OutputPath         string
	ContentType        string
	Disposition        string
	Progress           int
	ProgressStage      string
	SessionName        string
	SessionDescription string
	ChannelType        string
	TargetID           string
	TargetName         string
	TargetPath         string
	RecordingStartedAt string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	cancel             context.CancelFunc
}

type recordingExportRegistry struct {
	mu      sync.RWMutex
	jobs    map[string]*recordingExportJob
	tempDir string
}

func newRecordingExportRegistry(tempDir string) *recordingExportRegistry {
	return &recordingExportRegistry{
		jobs:    make(map[string]*recordingExportJob),
		tempDir: tempDir,
	}
}

func newRecordingExportJobID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// countByState tallies jobs per state for the metrics exporter.
func (r *recordingExportRegistry) countByState() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]int{}
	for _, j := range r.jobs {
		out[string(j.State)]++
	}
	return out
}

func (r *recordingExportRegistry) get(id string) (*recordingExportJob, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.jobs[id]
	return j, ok
}

func (r *recordingExportRegistry) remove(id string) (*recordingExportJob, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if ok {
		delete(r.jobs, id)
	}
	return j, ok
}

// removeForRecording drops every export job derived from recordingID and
// returns them so the caller can cancel running ones and delete their
// output files. Used when the source recording itself is deleted.
func (r *recordingExportRegistry) removeForRecording(recordingID string) []*recordingExportJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*recordingExportJob
	for id, j := range r.jobs {
		if j.RecordingID == recordingID {
			out = append(out, j)
			delete(r.jobs, id)
		}
	}
	return out
}

func (r *recordingExportRegistry) listForViewer(userID string, includeAll bool) []*recordingExportJob {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*recordingExportJob, 0)
	for _, j := range r.jobs {
		if includeAll || j.UserID == userID {
			out = append(out, j)
		}
	}
	sortRecordingExports(out)
	return out
}

func (r *recordingExportRegistry) findActive(userID, recordingID, format string) *recordingExportJob {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.findActiveLocked(userID, recordingID, format)
}

func (r *recordingExportRegistry) findActiveLocked(userID, recordingID, format string) *recordingExportJob {
	for _, j := range r.jobs {
		if j.UserID != userID || j.RecordingID != recordingID || j.Format != format {
			continue
		}
		if j.State == recordingExportQueued || j.State == recordingExportRunning {
			return j
		}
	}
	return nil
}

func sortRecordingExports(jobs []*recordingExportJob) {
	for i := 0; i < len(jobs); i++ {
		for k := i + 1; k < len(jobs); k++ {
			if jobs[k].CreatedAt.After(jobs[i].CreatedAt) {
				jobs[i], jobs[k] = jobs[k], jobs[i]
			}
		}
	}
}

func (j *recordingExportJob) snapshot() map[string]interface{} {
	return map[string]interface{}{
		"job_id":               j.ID,
		"recording_id":         j.RecordingID,
		"format":               j.Format,
		"state":                string(j.State),
		"error":                j.Error,
		"progress":             j.Progress,
		"progress_stage":       j.ProgressStage,
		"session_name":         j.SessionName,
		"session_description":  j.SessionDescription,
		"channel_type":         j.ChannelType,
		"target_id":            j.TargetID,
		"target_name":          j.TargetName,
		"target_path":          j.TargetPath,
		"recording_started_at": j.RecordingStartedAt,
		"created_at":           j.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":           j.UpdatedAt.UTC().Format(time.RFC3339),
		"status_url":           fmt.Sprintf("/api/recordings/exports/%s", j.ID),
		"file_url":             fmt.Sprintf("/api/recordings/exports/%s/file", j.ID),
	}
}

type recordingExportMeta struct {
	SessionName        string
	SessionDescription string
	ChannelType        string
	TargetID           string
	TargetName         string
	TargetPath         string
	StartedAt          string
}

func (a *App) lookupRecordingExportMeta(recordingID string) recordingExportMeta {
	if a == nil || a.DB == nil || strings.TrimSpace(recordingID) == "" {
		return recordingExportMeta{}
	}
	var meta recordingExportMeta
	var startedAt sql.NullString
	err := a.DB.QueryRowContext(context.Background(),
		`SELECT r.target_id, r.channel_type, r.started_at, COALESCE(r.session_name, ''), COALESCE(r.session_description, ''),
		        COALESCE(t.name, ''), COALESCE(t.path, '')
		 FROM recordings r
		 LEFT JOIN targets t ON t.id = r.target_id
		 WHERE r.id = ?`,
		recordingID,
	).Scan(&meta.TargetID, &meta.ChannelType, &startedAt, &meta.SessionName, &meta.SessionDescription, &meta.TargetName, &meta.TargetPath)
	if err != nil {
		return recordingExportMeta{}
	}
	if startedAt.Valid {
		meta.StartedAt = startedAt.String
	}
	return meta
}

func recordingExportConvertTimeoutDuration() time.Duration {
	v := strings.TrimSpace(os.Getenv(recordingExportConvertTimeoutEnv))
	if v == "" {
		return recordingExportConvertTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 5*time.Minute {
		return recordingExportConvertTimeout
	}
	return d
}

func recordingExportStaleDuration() time.Duration {
	stale := recordingExportStaleAfter
	minStale := recordingExportConvertTimeoutDuration() + 5*time.Minute
	if stale < minStale {
		return minStale
	}
	return stale
}

func recordingExportCompletedTTLDuration() time.Duration {
	v := strings.TrimSpace(os.Getenv(recordingExportCompletedTTLEnv))
	if v == "" {
		return recordingExportCompletedTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Hour {
		return recordingExportCompletedTTL
	}
	return d
}

func (a *App) isAdminUser(userID string) bool {
	if a == nil || a.UserStore == nil {
		return false
	}
	u, err := a.UserStore.GetByID(userID)
	return err == nil && u != nil && u.Role == auth.RoleAdmin
}

func (a *App) sweepRecordingExportsMaintenance() {
	a.sweepStaleRecordingExports()
	a.sweepCompletedRecordingExports()
}

func (a *App) sweepStaleRecordingExports() {
	if a == nil || a.RecordingExports == nil {
		return
	}
	now := time.Now().UTC()
	a.RecordingExports.mu.RLock()
	stale := make([]*recordingExportJob, 0)
	for _, j := range a.RecordingExports.jobs {
		if j.State != recordingExportRunning {
			continue
		}
		if now.Sub(j.UpdatedAt) > recordingExportStaleDuration() {
			stale = append(stale, j)
		}
	}
	a.RecordingExports.mu.RUnlock()
	for _, j := range stale {
		a.requestCancelRecordingExport(j)
		a.updateRecordingExportState(j, recordingExportFailed, "export timed out (no progress)")
	}
}

func (a *App) sweepCompletedRecordingExports() {
	if a == nil || a.RecordingExports == nil {
		return
	}
	ttl := recordingExportCompletedTTLDuration()
	now := time.Now().UTC()
	a.RecordingExports.mu.Lock()
	defer a.RecordingExports.mu.Unlock()
	for id, j := range a.RecordingExports.jobs {
		switch j.State {
		case recordingExportCompleted, recordingExportFailed, recordingExportCancelled:
		default:
			continue
		}
		if now.Sub(j.UpdatedAt) <= ttl {
			continue
		}
		if j.OutputPath != "" {
			_ = os.Remove(j.OutputPath)
		}
		delete(a.RecordingExports.jobs, id)
	}
}

func cleanupOldGlobTemps(dir string, patterns []string, maxAge time.Duration) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	now := time.Now()
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			continue
		}
		for _, p := range matches {
			st, err := os.Stat(p)
			if err != nil || now.Sub(st.ModTime()) < maxAge {
				continue
			}
			_ = os.Remove(p)
		}
	}
}

func cleanupOrphanExportTemps(exportDir string) {
	cleanupOldGlobTemps(exportDir, []string{"rec-*.gif", "rec-*.mp4"}, recordingExportOrphanTempMaxAge)
}

func cleanupLegacyRecordingDirExportTemps(recordingsDir string) {
	cleanupOldGlobTemps(recordingsDir, []string{"rec-*.gif", "rec-*.mp4"}, recordingExportOrphanTempMaxAge)
}

func (a *App) enqueueRecordingExport(userID, recordingID, format, mediaPath string) (*recordingExportJob, error) {
	if a.RecordingExports == nil {
		return nil, errors.New("recording export unavailable")
	}
	a.sweepRecordingExportsMaintenance()
	a.RecordingExports.mu.Lock()
	defer a.RecordingExports.mu.Unlock()
	if existing := a.RecordingExports.findActiveLocked(userID, recordingID, format); existing != nil {
		return existing, nil
	}
	jobID, err := newRecordingExportJobID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	meta := a.lookupRecordingExportMeta(recordingID)
	jobCtx, jobCancel := context.WithCancel(context.Background())
	job := &recordingExportJob{
		ID:                 jobID,
		UserID:             userID,
		RecordingID:        recordingID,
		Format:             format,
		State:              recordingExportQueued,
		SessionName:        meta.SessionName,
		SessionDescription: meta.SessionDescription,
		ChannelType:        meta.ChannelType,
		TargetID:           meta.TargetID,
		TargetName:         meta.TargetName,
		TargetPath:         meta.TargetPath,
		RecordingStartedAt: meta.StartedAt,
		CreatedAt:          now,
		UpdatedAt:          now,
		cancel:             jobCancel,
	}
	a.RecordingExports.jobs[job.ID] = job
	go a.runRecordingExportJob(job, mediaPath, jobCtx)
	return job, nil
}

func (a *App) runRecordingExportJob(job *recordingExportJob, mediaPath string, jobCtx context.Context) {
	defer func() {
		a.RecordingExports.mu.Lock()
		cancel := job.cancel
		job.cancel = nil
		a.RecordingExports.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}()

	a.updateRecordingExportProgress(job, 0, "queued")

	acquireCtx, acquireCancel := context.WithTimeout(jobCtx, recordingExportAcquireTimeout)
	defer acquireCancel()

	a.updateRecordingExportProgress(job, 2, "waiting")

	gov := recording.DefaultGovernor()
	if err := gov.AcquireExport(acquireCtx); err != nil {
		if errors.Is(err, context.Canceled) {
			a.finishRecordingExportJob(job, "", "", "", context.Canceled)
			return
		}
		a.finishRecordingExportJob(job, "", "", "", fmt.Errorf("export queue wait failed"))
		return
	}
	defer gov.ReleaseExport()

	convertCtx, convertCancel := context.WithTimeout(jobCtx, recordingExportConvertTimeoutDuration())
	defer convertCancel()

	a.updateRecordingExportState(job, recordingExportRunning, "")
	a.updateRecordingExportProgress(job, 5, "starting")
	threads := gov.FFmpegThreads()
	report := a.recordingExportProgressReporter(job)
	temps := &exportTempTracker{}
	defer func() {
		if job.State != recordingExportCompleted {
			temps.cleanup()
		}
	}()

	var (
		outPath     string
		contentType string
		disposition string
		err         error
	)
	workDir := a.RecordingExports.tempDir
	switch {
	case recording.IsMP4Path(mediaPath) && job.Format == "gif":
		outPath, contentType, disposition, err = convertVideoToGIF(convertCtx, mediaPath, workDir, threads, report, temps)
	case job.Format == "gif":
		outPath, contentType, disposition, err = convertCastToGIF(convertCtx, mediaPath, workDir, report, temps)
	case job.Format == "mp4":
		outPath, contentType, disposition, err = convertCastToMP4(convertCtx, mediaPath, workDir, threads, report, temps)
	default:
		err = fmt.Errorf("unsupported export format %q", job.Format)
	}
	a.finishRecordingExportJob(job, outPath, contentType, disposition, err)
}

func (a *App) recordingExportProgressReporter(job *recordingExportJob) exportProgressFunc {
	return func(stage string, percent int) {
		a.updateRecordingExportProgress(job, percent, stage)
	}
}

func (a *App) updateRecordingExportProgress(job *recordingExportJob, percent int, stage string) {
	if a.RecordingExports == nil || job == nil {
		return
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	a.RecordingExports.mu.Lock()
	job.Progress = percent
	job.ProgressStage = stage
	job.UpdatedAt = time.Now().UTC()
	a.RecordingExports.mu.Unlock()
}

func (a *App) requestCancelRecordingExport(job *recordingExportJob) {
	if job == nil {
		return
	}
	a.RecordingExports.mu.Lock()
	cancel := job.cancel
	a.RecordingExports.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) updateRecordingExportState(job *recordingExportJob, state recordingExportState, errMsg string) {
	if a.RecordingExports == nil || job == nil {
		return
	}
	a.RecordingExports.mu.Lock()
	job.State = state
	job.Error = errMsg
	job.UpdatedAt = time.Now().UTC()
	a.RecordingExports.mu.Unlock()
}

func (a *App) finishRecordingExportJob(job *recordingExportJob, outPath, contentType, disposition string, runErr error) {
	if job == nil {
		if outPath != "" {
			_ = os.Remove(outPath)
		}
		return
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			a.updateRecordingExportState(job, recordingExportCancelled, "")
			a.updateRecordingExportProgress(job, 0, "cancelled")
		} else {
			a.updateRecordingExportState(job, recordingExportFailed, userVisibleExportError(runErr))
		}
		if outPath != "" {
			_ = os.Remove(outPath)
		}
		return
	}
	a.RecordingExports.mu.Lock()
	job.State = recordingExportCompleted
	job.OutputPath = outPath
	job.ContentType = contentType
	job.Disposition = disposition
	job.Progress = 100
	job.ProgressStage = "completed"
	job.UpdatedAt = time.Now().UTC()
	a.RecordingExports.mu.Unlock()
}

func (a *App) userCanAccessRecordingExport(w http.ResponseWriter, r *http.Request, job *recordingExportJob) bool {
	if job == nil {
		writeJSONErrorKey(w, r, "recordings.exportNotFound", http.StatusNotFound)
		return false
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return false
	}
	if job.UserID != userID {
		if a.UserStore != nil {
			if u, err := a.UserStore.GetByID(userID); err == nil && u != nil && u.Role == auth.RoleAdmin {
				return true
			}
		}
		writeJSONErrorKey(w, r, "recordings.exportNotFound", http.StatusNotFound)
		return false
	}
	return true
}

// handleGetRecordingExportStatus returns export job progress.
func (a *App) handleGetRecordingExportStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "export_id")
	if jobID == "" {
		writeJSONErrorKey(w, r, "recordings.exportIdRequired", http.StatusBadRequest)
		return
	}
	if a.RecordingExports == nil {
		writeJSONErrorKey(w, r, "recordings.exportUnavailable", http.StatusServiceUnavailable)
		return
	}
	job, ok := a.RecordingExports.get(jobID)
	if !ok || !a.userCanAccessRecordingExport(w, r, job) {
		return
	}
	writeJSON(w, job.snapshot())
}

// handleGetRecordingExportFile serves a completed export artifact.
func (a *App) handleGetRecordingExportFile(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "export_id")
	if jobID == "" {
		writeJSONErrorKey(w, r, "recordings.exportIdRequired", http.StatusBadRequest)
		return
	}
	if a.RecordingExports == nil {
		writeJSONErrorKey(w, r, "recordings.exportUnavailable", http.StatusServiceUnavailable)
		return
	}
	job, ok := a.RecordingExports.get(jobID)
	if !ok || !a.userCanAccessRecordingExport(w, r, job) {
		return
	}
	if job.State != recordingExportCompleted || job.OutputPath == "" {
		writeJSONErrorKey(w, r, "recordings.exportNotReady", http.StatusConflict)
		return
	}
	exportDir := a.RecordingExports.tempDir
	safePath, err := openExportPath(exportDir, job.OutputPath)
	if err != nil {
		writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
		return
	}
	out, err := os.Open(safePath) // #nosec G304 -- path validated under export dir.
	if err != nil {
		writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
		return
	}
	defer out.Close()
	if st, statErr := out.Stat(); statErr == nil && st.Size() >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", st.Size()))
	}
	w.Header().Set("Content-Type", job.ContentType)
	if job.Disposition != "" {
		w.Header().Set("Content-Disposition", job.Disposition)
	}
	_, _ = io.Copy(w, out)
}

func recordingNeedsAsyncExport(format, mediaPath string) bool {
	switch format {
	case "mp4":
		return !recording.IsMP4Path(mediaPath)
	case "gif":
		return true
	default:
		return false
	}
}

func writeRecordingExportAccepted(w http.ResponseWriter, job *recordingExportJob) {
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, job.snapshot())
}

func decodeRecordingID(raw string) string {
	recordingID := raw
	if decoded, e := url.PathUnescape(raw); e == nil {
		recordingID = decoded
	}
	return recordingID
}

func sanitizeRecordingBasename(name string) string {
	for _, ext := range []string{".cast", ".mp4"} {
		if strings.HasSuffix(strings.ToLower(name), ext) {
			prefix := name[:len(name)-len(ext)]
			safe := strings.ReplaceAll(prefix, ":", "-")
			safe = strings.ReplaceAll(safe, ".", "-")
			return safe + ext
		}
	}
	s := strings.ReplaceAll(name, ":", "-")
	return strings.ReplaceAll(s, ".", "-")
}

func resolveRecordingMediaPath(recordingDir, filePath string) (string, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(recordingDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid recording path")
	}
	if _, err := os.Stat(absPath); err == nil {
		return openRecordingPath(recordingDir, absPath)
	}
	dir, base := filepath.Dir(absPath), filepath.Base(absPath)
	safeBase := sanitizeRecordingBasename(base)
	if safeBase != base {
		alt := filepath.Join(dir, safeBase)
		if _, err := os.Stat(alt); err == nil {
			return openRecordingPath(recordingDir, alt)
		}
	}
	return "", os.ErrNotExist
}

// openRecordingPath resolves symlinks and verifies the target stays inside recordingDir.
func openRecordingPath(recordingDir, absPath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", os.ErrNotExist
		}
		return "", err
	}
	absDir, err := filepath.Abs(recordingDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid recording path")
	}
	return resolved, nil
}

func userVisibleExportError(runErr error) string {
	if runErr == nil {
		return ""
	}
	if errors.Is(runErr, errRecordingVideoTools) {
		return "Video export requires agg and ffmpeg on the server"
	}
	return runErr.Error()
}

// openExportPath resolves symlinks and verifies the target stays inside exportDir.
func openExportPath(exportDir, absPath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", os.ErrNotExist
		}
		return "", err
	}
	absDir, err := filepath.Abs(exportDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid export path")
	}
	return resolved, nil
}

func (a *App) resolveRecordingMediaAccess(w http.ResponseWriter, r *http.Request, recordingID string) (*recordingMediaAccess, bool) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	if a.DB == nil {
		writeJSONErrorKey(w, r, "recordings.notAvailable", http.StatusServiceUnavailable)
		return nil, false
	}
	var filePath string
	var sessionID sql.NullString
	var startedAt sql.NullString
	var ownerID string
	err := a.DB.QueryRowContext(r.Context(), `SELECT file_path, session_id, started_at, user_id FROM recordings WHERE id = ?`, recordingID).
		Scan(&filePath, &sessionID, &startedAt, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONErrorKey(w, r, "recordings.notFound", http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		writeInternalError(w, err)
		return nil, false
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != userID {
		allowed := false
		if a.UserStore != nil {
			if u, uerr := a.UserStore.GetByID(userID); uerr == nil && u != nil && u.Role == auth.RoleAdmin {
				allowed = true
			}
		}
		if !allowed && sessionID.Valid && strings.TrimSpace(sessionID.String) != "" {
			allowed = a.userCanViewSessionRecordings(r.Context(), userID, sessionID.String)
		}
		if !allowed {
			writeJSONErrorKey(w, r, "recordings.notFound", http.StatusNotFound)
			return nil, false
		}
	}
	recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
	if recordingDir == "" {
		writeJSONErrorKey(w, r, "recordings.notConfigured", http.StatusServiceUnavailable)
		return nil, false
	}
	mediaPath, err := resolveRecordingMediaPath(recordingDir, filePath)
	if err != nil {
		writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
		return nil, false
	}
	return &recordingMediaAccess{
		RecordingID: recordingID,
		MediaPath:   mediaPath,
		SessionID:   sessionID.String,
		StartedAt:   startedAt.String,
		OwnerID:     ownerID,
	}, true
}

// handleListRecordingExports lists background export jobs for the current user.
func (a *App) handleListRecordingExports(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.RecordingExports == nil {
		writeJSON(w, map[string]interface{}{"items": []interface{}{}})
		return
	}
	a.sweepRecordingExportsMaintenance()
	includeAll := a.isAdminUser(userID)
	jobs := a.RecordingExports.listForViewer(userID, includeAll)
	items := make([]map[string]interface{}, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, j.snapshot())
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleDeleteRecordingExport cancels active jobs or removes finished ones.
func (a *App) handleDeleteRecordingExport(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "export_id")
	if jobID == "" {
		writeJSONErrorKey(w, r, "recordings.exportIdRequired", http.StatusBadRequest)
		return
	}
	if a.RecordingExports == nil {
		writeJSONErrorKey(w, r, "recordings.exportUnavailable", http.StatusServiceUnavailable)
		return
	}
	job, ok := a.RecordingExports.get(jobID)
	if !ok || !a.userCanAccessRecordingExport(w, r, job) {
		return
	}
	if job.State == recordingExportQueued || job.State == recordingExportRunning {
		a.requestCancelRecordingExport(job)
		a.updateRecordingExportState(job, recordingExportCancelled, "")
		a.updateRecordingExportProgress(job, 0, "cancelled")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	removed, ok := a.RecordingExports.remove(jobID)
	if !ok {
		writeJSONErrorKey(w, r, "recordings.exportNotFound", http.StatusNotFound)
		return
	}
	if removed.OutputPath != "" {
		_ = os.Remove(removed.OutputPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePostRecordingExport queues GIF/MP4 generation for a recording.
func (a *App) handlePostRecordingExport(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "recording_id")
	if rawID == "" {
		writeJSONErrorKey(w, r, "recordings.idRequired", http.StatusBadRequest)
		return
	}
	recordingID := decodeRecordingID(rawID)
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "gif" && format != "mp4" {
		writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
		return
	}
	access, ok := a.resolveRecordingMediaAccess(w, r, recordingID)
	if !ok {
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if format == "mp4" && recording.IsMP4Path(access.MediaPath) {
		writeJSONErrorKey(w, r, "recordings.exportDirectOnly", http.StatusBadRequest)
		return
	}
	if !recordingNeedsAsyncExport(format, access.MediaPath) {
		writeJSONErrorKey(w, r, "recordings.exportDirectOnly", http.StatusBadRequest)
		return
	}
	job, err := a.enqueueRecordingExport(userID, recordingID, format, access.MediaPath)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeRecordingExportAccepted(w, job)
}
