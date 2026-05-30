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

const recordingExportPollInterval = 500 * time.Millisecond

const (
	recordingExportAcquireTimeout = 5 * time.Minute
	recordingExportConvertTimeout = 15 * time.Minute
	recordingExportStaleAfter     = 20 * time.Minute
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

func (r *recordingExportRegistry) get(id string) (*recordingExportJob, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.jobs[id]
	return j, ok
}

func (r *recordingExportRegistry) set(j *recordingExportJob) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[j.ID] = j
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

func (r *recordingExportRegistry) listForUser(userID string) []*recordingExportJob {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*recordingExportJob, 0)
	for _, j := range r.jobs {
		if j.UserID == userID {
			out = append(out, j)
		}
	}
	sortRecordingExports(out)
	return out
}

func (r *recordingExportRegistry) findActive(userID, recordingID, format string) *recordingExportJob {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, j := range r.jobs {
		if j.UserID != userID || j.RecordingID != recordingID || j.Format != format {
			continue
		}
		if j.State == recordingExportQueued || j.State == recordingExportRunning {
			if j.State == recordingExportRunning && time.Since(j.UpdatedAt) > recordingExportStaleAfter {
				continue
			}
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
	StartedAt          string
}

func (a *App) lookupRecordingExportMeta(recordingID string) recordingExportMeta {
	if a == nil || a.DB == nil || strings.TrimSpace(recordingID) == "" {
		return recordingExportMeta{}
	}
	var meta recordingExportMeta
	var startedAt sql.NullString
	err := a.DB.QueryRowContext(context.Background(),
		`SELECT target_id, channel_type, started_at, COALESCE(session_name, ''), COALESCE(session_description, '') FROM recordings WHERE id = ?`,
		recordingID,
	).Scan(&meta.TargetID, &meta.ChannelType, &startedAt, &meta.SessionName, &meta.SessionDescription)
	if err != nil {
		return recordingExportMeta{}
	}
	if startedAt.Valid {
		meta.StartedAt = startedAt.String
	}
	return meta
}

func (a *App) enqueueRecordingExport(userID, recordingID, format, mediaPath, wmText string) (*recordingExportJob, error) {
	if a.RecordingExports == nil {
		return nil, errors.New("recording export unavailable")
	}
	if existing := a.RecordingExports.findActive(userID, recordingID, format); existing != nil {
		return existing, nil
	}
	jobID, err := newRecordingExportJobID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	meta := a.lookupRecordingExportMeta(recordingID)
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
		RecordingStartedAt: meta.StartedAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	a.RecordingExports.set(job)
	go a.runRecordingExportJob(job, mediaPath, wmText)
	return job, nil
}

func (a *App) runRecordingExportJob(job *recordingExportJob, mediaPath, wmText string) {
	jobCtx, jobCancel := context.WithCancel(context.Background())
	defer jobCancel()

	a.RecordingExports.mu.Lock()
	job.cancel = jobCancel
	a.RecordingExports.mu.Unlock()
	defer func() {
		a.RecordingExports.mu.Lock()
		job.cancel = nil
		a.RecordingExports.mu.Unlock()
	}()

	a.updateRecordingExportProgress(job, 0, "queued")

	acquireCtx, acquireCancel := context.WithTimeout(jobCtx, recordingExportAcquireTimeout)
	defer acquireCancel()

	a.updateRecordingExportProgress(job, 2, "waiting")

	gov := recording.DefaultGovernor()
	if err := gov.AcquireExport(acquireCtx); err != nil {
		a.finishRecordingExportJob(job, "", "", "", fmt.Errorf("export queue wait: %w", err))
		return
	}
	defer gov.ReleaseExport()

	convertCtx, convertCancel := context.WithTimeout(jobCtx, recordingExportConvertTimeout)
	defer convertCancel()

	a.updateRecordingExportState(job, recordingExportRunning, "")
	a.updateRecordingExportProgress(job, 5, "starting")
	threads := gov.FFmpegThreads()
	report := a.recordingExportProgressReporter(job)

	var (
		outPath     string
		contentType string
		disposition string
		err         error
	)
	switch {
	case recording.IsWebMPath(mediaPath) && job.Format == "gif":
		outPath, contentType, disposition, err = convertWebMToGIF(convertCtx, mediaPath, wmText, threads, report)
	case job.Format == "gif" || job.Format == "webm":
		outPath, contentType, disposition, err = convertCastToVideo(convertCtx, mediaPath, job.Format, wmText, threads, report)
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
			a.updateRecordingExportState(job, recordingExportFailed, runErr.Error())
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
	out, err := os.Open(job.OutputPath) // #nosec G304 -- path from our temp export job.
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
	if format == "gif" {
		return true
	}
	return format == "webm" && !recording.IsWebMPath(mediaPath)
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
	for _, ext := range []string{".cast", ".webm"} {
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
		return absPath, nil
	}
	dir, base := filepath.Dir(absPath), filepath.Base(absPath)
	safeBase := sanitizeRecordingBasename(base)
	if safeBase != base {
		alt := filepath.Join(dir, safeBase)
		if _, err := os.Stat(alt); err == nil {
			return alt, nil
		}
	}
	return "", os.ErrNotExist
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
	jobs := a.RecordingExports.listForUser(userID)
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

// handlePostRecordingExport queues GIF/WebM generation for a recording.
func (a *App) handlePostRecordingExport(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "recording_id")
	if rawID == "" {
		writeJSONErrorKey(w, r, "recordings.idRequired", http.StatusBadRequest)
		return
	}
	recordingID := decodeRecordingID(rawID)
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	switch format {
	case "gif", "webm":
	default:
		writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
		return
	}
	access, ok := a.resolveRecordingMediaAccess(w, r, recordingID)
	if !ok {
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if !recordingNeedsAsyncExport(format, access.MediaPath) {
		writeJSONErrorKey(w, r, "recordings.exportDirectOnly", http.StatusBadRequest)
		return
	}
	wmText := watermarkTextForRecording(userID, access.SessionID, access.StartedAt)
	job, err := a.enqueueRecordingExport(userID, recordingID, format, access.MediaPath, wmText)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeRecordingExportAccepted(w, job)
}
