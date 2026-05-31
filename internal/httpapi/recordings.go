package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/recording"
)

var (
	errRecordingVideoTools = errors.New("recording video tools missing")
)

// exportStageError turns a tool failure into a user-visible message without
// leaking ffmpeg/agg stderr (details are audit-logged at call sites).
func exportStageError(stage string, ctx context.Context, out []byte, runErr error) error {
	if runErr == nil {
		return nil
	}
	if errors.Is(runErr, context.Canceled) || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)) {
		return context.Canceled
	}
	if errors.Is(runErr, context.DeadlineExceeded) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return fmt.Errorf("export %s timed out", stage)
	}
	return fmt.Errorf("export %s failed", stage)
}

// exportProgressFunc reports conversion stage and percent (0–100) for export jobs.
type exportProgressFunc func(stage string, percent int)

func (a *App) userCanViewSessionRecordings(ctx context.Context, userID, sessionID string) bool {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" || a == nil || a.DB == nil {
		return false
	}
	var n int
	// A user may view a session's recordings once they have consumed an invitation
	// for that session (named invite or single-use link). This is backed by the
	// persistent session_invitations table rather than the in-memory room state.
	//
	// NOTE: multi-use link consumers are tracked in session_invitation_consumers.
	err := a.DB.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM (
			SELECT 1 FROM session_invitations
			WHERE session_id = ?
			  AND invitee_user_id = ?
			  AND revoked_at IS NULL
			  AND use_count > 0
			UNION
			SELECT 1 FROM session_invitation_consumers sic
			INNER JOIN session_invitations si ON si.id = sic.invitation_id
			WHERE si.session_id = ?
			  AND sic.user_id = ?
			  AND si.revoked_at IS NULL
		)
	`, sessionID, userID, sessionID, userID).Scan(&n)
	return err == nil && n > 0
}

// auditRecordingExportFailure logs tool/export failures for operators; never
// returned to API clients.
func auditRecordingExportFailure(stage string, out []byte, runErr error) {
	detail := strings.TrimSpace(string(out))
	if detail == "" && runErr != nil {
		detail = runErr.Error()
	}
	const maxDetail = 4096
	if len(detail) > maxDetail {
		detail = detail[:maxDetail] + "…"
	}
	audit("recording_export_failed", auditFields{
		"stage":  stage,
		"detail": detail,
	})
}

// handleListRecordings returns recordings for the current user
// (metadata only, no file_path). Admins may pass user_id to list
// another user's recordings.
//
//nolint:gocyclo // listing assembles a multi-field WHERE clause from query parameters.
func (a *App) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.DB == nil {
		writeJSON(w, map[string]interface{}{"items": []interface{}{}})
		return
	}
	ctx := r.Context()
	q := r.URL.Query()

	filterUserID := userID
	if requestedUser := strings.TrimSpace(q.Get("user_id")); requestedUser != "" && requestedUser != userID {
		u, err := a.UserStore.GetByID(userID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if u == nil || u.Role != auth.RoleAdmin {
			writeJSONErrorKey(w, r, "common.forbiddenAdminOnly", http.StatusForbidden)
			return
		}
		filterUserID = requestedUser
	}

	from, to, err := parseTimeRange(q.Get("from"), q.Get("to"), time.Now().UTC())
	if err != nil {
		writeTimeRangeError(w, r, err)
		return
	}

	targetID := strings.TrimSpace(q.Get("target_id"))
	channelType := strings.TrimSpace(q.Get("channel_type"))
	sessionID := strings.TrimSpace(q.Get("session_id"))

	query := `SELECT id, user_id, target_id, session_id, channel_type, started_at, ended_at, COALESCE(session_name, ''), COALESCE(session_description, ''), file_path FROM recordings WHERE `
	args := []interface{}{}
	// Normal users can list:
	//  - their own recordings
	//  - recordings of sessions they joined via invitations
	// Admins listing another user stay scoped to that user's recordings only.
	if filterUserID == userID {
		query += `(user_id = ? OR session_id IN (
			SELECT session_id FROM session_invitations
			WHERE invitee_user_id = ?
			  AND revoked_at IS NULL
			  AND use_count > 0
			UNION
			SELECT si.session_id FROM session_invitation_consumers sic
			INNER JOIN session_invitations si ON si.id = sic.invitation_id
			WHERE sic.user_id = ?
			  AND si.revoked_at IS NULL
		))`
		args = append(args, filterUserID, userID, userID)
	} else {
		query += `user_id = ?`
		args = append(args, filterUserID)
	}
	if targetID != "" {
		query += ` AND target_id = ?`
		args = append(args, targetID)
	}
	if channelType != "" {
		query += ` AND channel_type = ?`
		args = append(args, channelType)
	}
	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	// Recordings may use "2006-01-02 15:04:05" (browser) or RFC3339
	// (CLI / tests). Extend the upper bound slightly so rows inserted
	// at "now" are included (to is exclusive).
	toRec := to.Add(time.Minute)
	fromSpace := formatRecordingTime(from)
	toSpace := formatRecordingTime(toRec)
	fromRFC := from.UTC().Format(time.RFC3339)
	toRFC := toRec.UTC().Format(time.RFC3339)
	query += ` AND ((started_at >= ? AND started_at < ?) OR (started_at >= ? AND started_at < ?))`
	args = append(args, fromSpace, toSpace, fromRFC, toRFC)
	query += ` ORDER BY started_at DESC LIMIT 200`
	rows, err := a.DB.QueryContext(ctx, query, args...)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	var items []map[string]interface{}
	for rows.Next() {
		var id, recUserID, tID, sessID, channelType, startedAt, sessName, sessDesc, filePath string
		var endedAt sql.NullString
		if err := rows.Scan(&id, &recUserID, &tID, &sessID, &channelType, &startedAt, &endedAt, &sessName, &sessDesc, &filePath); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"id":                  id,
			"user_id":             recUserID,
			"target_id":           tID,
			"session_id":          sessID,
			"channel_type":        channelType,
			"media_type":          recordingMediaType(channelType, filePath),
			"storage_format":      recording.StorageFormat(filePath),
			"started_at":          startedAt,
			"ended_at":            endedAt.String,
			"session_name":        sessName,
			"session_description": sessDesc,
		})
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleGetRecordingFile serves the recording file.
//
// Query parameter format=cast|gif|mp4 (default cast). cast is the
// raw asciinema .cast or native MP4. gif/mp4 conversion for terminal
// recordings and MP4→GIF are queued asynchronously (202 Accepted).
//
//nolint:gocyclo // open + path resolution + format conversion all need to live together.
func (a *App) handleGetRecordingFile(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
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
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "cast"
	}
	switch format {
	case "cast", "gif", "mp4":
	default:
		writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
		return
	}
	if a.DB == nil {
		writeJSONErrorKey(w, r, "recordings.notAvailable", http.StatusServiceUnavailable)
		return
	}
	var filePath string
	var sessionID sql.NullString
	var startedAt sql.NullString
	var ownerID string
	err := a.DB.QueryRowContext(r.Context(), `SELECT file_path, session_id, started_at, user_id FROM recordings WHERE id = ?`, recordingID).
		Scan(&filePath, &sessionID, &startedAt, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONErrorKey(w, r, "recordings.notFound", http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	// Authorization: owner can always fetch; admins can fetch any; invitees can fetch
	// recordings for sessions they joined.
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
			// Fail closed and avoid leaking that the recording exists.
			writeJSONErrorKey(w, r, "recordings.notFound", http.StatusNotFound)
			return
		}
	}
	recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
	if recordingDir == "" {
		writeJSONErrorKey(w, r, "recordings.notConfigured", http.StatusServiceUnavailable)
		return
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		writeJSONErrorKey(w, r, "common.invalidPath", http.StatusBadRequest)
		return
	}
	absDir, err := filepath.Abs(recordingDir)
	if err != nil {
		writeJSONErrorKey(w, r, "common.invalidPath", http.StatusBadRequest)
		return
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeJSONErrorKey(w, r, "common.invalidPath", http.StatusBadRequest)
		return
	}
	tryOpen := func(path string) (*os.File, error) {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		safe, err := openRecordingPath(recordingDir, absPath)
		if err != nil {
			return nil, err
		}
		return os.Open(safe) // #nosec G304 -- path validated under recordings dir.
	}
	sanitizeBasename := func(name string) string {
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
	mediaPath := filePath
	f, err := tryOpen(filePath)
	if err != nil {
		dir, base := filepath.Dir(filePath), filepath.Base(filePath)
		safeBase := sanitizeBasename(base)
		if safeBase != base {
			mediaPath = filepath.Join(dir, safeBase)
			f, err = tryOpen(mediaPath)
		}
	}
	if err != nil {
		writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
		return
	}

	if strings.HasSuffix(strings.ToLower(mediaPath), ".webm") {
		f.Close()
		writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
		return
	}

	if recording.IsMP4Path(mediaPath) {
		f.Close()
		switch format {
		case "cast":
			writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
			return
		case "mp4":
			serveFile, err := tryOpen(mediaPath)
			if err != nil {
				writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
				return
			}
			defer serveFile.Close()
			w.Header().Set("Content-Type", "video/mp4")
			setAttachmentDisposition(w, filepath.Base(mediaPath))
			_, _ = io.Copy(w, serveFile)
			return
		case "gif":
			job, err := a.enqueueRecordingExport(userID, recordingID, "gif", mediaPath)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			writeRecordingExportAccepted(w, job)
			return
		default:
			writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
			return
		}
	}

	castPath := mediaPath
	if format == "cast" {
		defer f.Close()
		w.Header().Set("Content-Type", "application/json")
		setAttachmentDisposition(w, filepath.Base(castPath))
		_, _ = io.Copy(w, f)
		return
	}
	f.Close()
	if recordingNeedsAsyncExport(format, castPath) {
		job, err := a.enqueueRecordingExport(userID, recordingID, format, castPath)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeRecordingExportAccepted(w, job)
		return
	}
	writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
}

func ffmpegGlobalArgs(ffmpegThreads int) []string {
	if ffmpegThreads < 1 {
		ffmpegThreads = 1
	}
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-threads", strconv.Itoa(ffmpegThreads)}
}

func runFFmpeg(ctx context.Context, ffmpegPath string, ffmpegThreads int, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	allArgs := append(ffmpegGlobalArgs(ffmpegThreads), args...)
	cmd := exec.CommandContext(ctx, ffmpegPath, allArgs...) // #nosec G204 G702 -- ffmpeg from LookPath; args built from validated recording paths.
	return cmd.CombinedOutput()
}

func h264MP4Args(outputPath string) []string {
	return []string{
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "animation",
		"-crf", "28",
		"-pix_fmt", "yuv420p",
		"-an",
		"-movflags", "+faststart",
		outputPath,
	}
}

// renderCastGIFIntermediate runs agg on a .cast file. The returned GIF path
// is tracked in temps when non-nil.
func renderCastGIFIntermediate(ctx context.Context, castPath, workDir string, onProgress exportProgressFunc, temps *exportTempTracker) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if onProgress != nil {
		onProgress("agg", 10)
	}
	aggPath, err := exec.LookPath("agg")
	if err != nil {
		auditRecordingExportFailure("agg_lookup", nil, err)
		return "", errRecordingVideoTools
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = filepath.Dir(castPath)
	}
	gifFile, err := os.CreateTemp(workDir, "rec-*.gif")
	if err != nil {
		return "", err
	}
	gifPath := gifFile.Name()
	gifFile.Close()
	if temps != nil {
		temps.track(gifPath)
	}
	cmd := exec.CommandContext(ctx, aggPath, "--", castPath, gifPath) // #nosec G204 G702 -- paths from validated castPath and temp file; `--` blocks option injection.
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(gifPath)
		auditRecordingExportFailure("agg", out, runErr)
		return "", exportStageError("agg", ctx, out, runErr)
	}
	if onProgress != nil {
		onProgress("agg", 45)
	}
	return gifPath, nil
}

// convertCastToGIF converts a .cast file to GIF using agg.
func convertCastToGIF(ctx context.Context, castPath, workDir string, onProgress exportProgressFunc, temps *exportTempTracker) (string, string, string, error) {
	gifPath, err := renderCastGIFIntermediate(ctx, castPath, workDir, onProgress, temps)
	if err != nil {
		return "", "", "", err
	}
	if onProgress != nil {
		onProgress("finalize", 95)
	}
	if temps != nil {
		temps.release(gifPath)
	}
	return gifPath, "image/gif", `attachment; filename="recording.gif"`, nil
}

// convertCastToMP4 converts a .cast file to MP4 (H.264) via agg and ffmpeg.
func convertCastToMP4(ctx context.Context, castPath, workDir string, ffmpegThreads int, onProgress exportProgressFunc, temps *exportTempTracker) (string, string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	gifPath, err := renderCastGIFIntermediate(ctx, castPath, workDir, onProgress, temps)
	if err != nil {
		return "", "", "", err
	}
	defer func() { _ = os.Remove(gifPath) }()

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		auditRecordingExportFailure("ffmpeg_lookup", nil, err)
		return "", "", "", errRecordingVideoTools
	}
	if ffmpegThreads < 1 {
		ffmpegThreads = 1
	}
	if onProgress != nil {
		onProgress("ffmpeg", 55)
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = filepath.Dir(castPath)
	}
	mp4File, err := os.CreateTemp(workDir, "rec-*.mp4")
	if err != nil {
		return "", "", "", err
	}
	mp4Path := mp4File.Name()
	mp4File.Close()
	if temps != nil {
		temps.track(mp4Path)
	}
	args := []string{
		"-y", "-i", gifPath,
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
	}
	args = append(args, h264MP4Args(mp4Path)...)
	if out, runErr := runFFmpeg(ctx, ffmpegPath, ffmpegThreads, args...); runErr != nil {
		_ = os.Remove(mp4Path)
		auditRecordingExportFailure("ffmpeg_mp4", out, runErr)
		return "", "", "", exportStageError("ffmpeg mp4", ctx, out, runErr)
	}
	if onProgress != nil {
		onProgress("finalize", 95)
	}
	if temps != nil {
		temps.release(mp4Path)
	}
	return mp4Path, "video/mp4", `attachment; filename="recording.mp4"`, nil
}

// convertVideoToGIF transcodes an MP4 screen recording to GIF.
func convertVideoToGIF(ctx context.Context, videoPath, workDir string, ffmpegThreads int, onProgress exportProgressFunc, temps *exportTempTracker) (string, string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if onProgress != nil {
		onProgress("ffmpeg", 15)
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		auditRecordingExportFailure("ffmpeg_lookup", nil, err)
		return "", "", "", errRecordingVideoTools
	}
	if ffmpegThreads < 1 {
		ffmpegThreads = 1
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = filepath.Dir(videoPath)
	}
	outGif, err := os.CreateTemp(workDir, "rec-*.gif")
	if err != nil {
		return "", "", "", err
	}
	outGifPath := outGif.Name()
	outGif.Close()
	if temps != nil {
		temps.track(outGifPath)
	}
	args := []string{"-y", "-i", videoPath, "-an", "-loop", "0", outGifPath}
	if out, runErr := runFFmpeg(ctx, ffmpegPath, ffmpegThreads, args...); runErr != nil {
		_ = os.Remove(outGifPath)
		auditRecordingExportFailure("ffmpeg_video_gif", out, runErr)
		return "", "", "", exportStageError("ffmpeg gif", ctx, out, runErr)
	}
	if onProgress != nil {
		onProgress("finalize", 95)
	}
	if temps != nil {
		temps.release(outGifPath)
	}
	return outGifPath, "image/gif", `attachment; filename="recording.gif"`, nil
}

// InsertRecording inserts a recording row (for CLI or other
// non-browser channels). Used by sshd when RecordingsDir and
// RecordingStore are set. No-op if a.DB is nil.
func (a *App) InsertRecording(ctx context.Context, id, userID, targetID, sessionID, channelType, filePath, startedAt, sessionName, sessionDesc string) error {
	if a.DB == nil {
		return nil
	}
	_, err := a.DB.ExecContext(ctx, `INSERT INTO recordings (id, user_id, target_id, session_id, channel_type, file_path, started_at, session_name, session_description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, targetID, sessionID, channelType, filePath, startedAt, sessionName, sessionDesc)
	return err
}

// UpdateRecordingEnded sets ended_at for a recording. No-op if a.DB is
// nil.
func (a *App) UpdateRecordingEnded(ctx context.Context, id, endedAt string) error {
	if a.DB == nil {
		return nil
	}
	_, err := a.DB.ExecContext(ctx, `UPDATE recordings SET ended_at = ? WHERE id = ?`, endedAt, id)
	return err
}
