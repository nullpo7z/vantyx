package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// handleDeleteRecording removes a recording: the DB row, the media file
// (.cast / .mp4) under VANTYX_RECORDINGS_DIR, and any GIF/MP4 export jobs
// derived from it. Admin only -- recordings are an audit artifact, so
// ordinary users cannot delete even their own.
func (a *App) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	rawID := chi.URLParam(r, "recording_id")
	if rawID == "" {
		writeJSONErrorKey(w, r, "recordings.idRequired", http.StatusBadRequest)
		return
	}
	recordingID := rawID
	if decoded, e := url.PathUnescape(rawID); e == nil {
		recordingID = decoded
	}
	if a.DB == nil {
		writeJSONErrorKey(w, r, "recordings.notAvailable", http.StatusServiceUnavailable)
		return
	}
	var filePath, ownerID, sessionID string
	err := a.DB.QueryRowContext(r.Context(), `SELECT file_path, user_id, COALESCE(session_id, '') FROM recordings WHERE id = ?`, recordingID).
		Scan(&filePath, &ownerID, &sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONErrorKey(w, r, "recordings.notFound", http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}

	// Drop the row first: once it is gone the recording is unreachable
	// through the API even if the file removal below fails (in which
	// case the orphaned file is reported in the audit entry).
	if _, err := a.DB.ExecContext(r.Context(), `DELETE FROM recordings WHERE id = ?`, recordingID); err != nil {
		writeInternalError(w, err)
		return
	}

	fileRemoved, fileErr := removeRecordingFile(filePath)

	exportsRemoved := 0
	if a.RecordingExports != nil {
		for _, job := range a.RecordingExports.removeForRecording(recordingID) {
			if job.State == recordingExportQueued || job.State == recordingExportRunning {
				a.requestCancelRecordingExport(job)
			}
			if job.OutputPath != "" {
				_ = os.Remove(job.OutputPath)
			}
			exportsRemoved++
		}
	}

	fields := auditFields{
		"user_id":         a.currentUserID(r),
		"recording_id":    recordingID,
		"owner_user_id":   ownerID,
		"session_id":      sessionID,
		"file_removed":    fileRemoved,
		"exports_removed": exportsRemoved,
	}
	if fileErr != nil {
		fields["file_error"] = fileErr.Error()
	}
	audit("recording_deleted", fields)
	w.WriteHeader(http.StatusNoContent)
}

// removeRecordingFile deletes the media file behind a recordings row,
// applying the same containment rule as handleGetRecordingFile: the path
// must resolve to somewhere under VANTYX_RECORDINGS_DIR. It also tries
// the sanitised basename fallback (":"/"." -> "-") that the read path
// uses for legacy rows whose stored path predates the Windows-safe
// naming. Returns (true, nil) when a file was removed, (false, nil) when
// there was nothing to remove, and (false, err) on a containment or I/O
// failure.
func removeRecordingFile(filePath string) (bool, error) {
	recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
	if recordingDir == "" || strings.TrimSpace(filePath) == "" {
		return false, nil
	}
	absDir, err := filepath.Abs(recordingDir)
	if err != nil {
		return false, err
	}
	candidates := []string{filePath}
	dir, base := filepath.Dir(filePath), filepath.Base(filePath)
	safe := strings.ReplaceAll(strings.ReplaceAll(base, ":", "-"), ".", "-")
	for _, ext := range []string{".cast", ".mp4"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			prefix := base[:len(base)-len(ext)]
			safe = strings.ReplaceAll(strings.ReplaceAll(prefix, ":", "-"), ".", "-") + ext
			break
		}
	}
	if safe != base {
		candidates = append(candidates, filepath.Join(dir, safe))
	}
	for _, cand := range candidates {
		absPath, err := filepath.Abs(cand)
		if err != nil {
			continue
		}
		resolved, err := openRecordingPath(absDir, absPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, err
		}
		if err := os.Remove(resolved); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		return true, nil
	}
	return false, nil
}
