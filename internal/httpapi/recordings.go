package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
)

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
		writeTimeRangeError(w, err)
		return
	}

	targetID := strings.TrimSpace(q.Get("target_id"))
	channelType := strings.TrimSpace(q.Get("channel_type"))
	sessionID := strings.TrimSpace(q.Get("session_id"))

	query := `SELECT id, user_id, target_id, session_id, channel_type, started_at, ended_at, COALESCE(session_name, ''), COALESCE(session_description, '') FROM recordings WHERE user_id = ?`
	args := []interface{}{filterUserID}
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
		var id, recUserID, tID, sessID, channelType, startedAt, sessName, sessDesc string
		var endedAt sql.NullString
		if err := rows.Scan(&id, &recUserID, &tID, &sessID, &channelType, &startedAt, &endedAt, &sessName, &sessDesc); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"id":                  id,
			"user_id":             recUserID,
			"target_id":           tID,
			"session_id":          sessID,
			"channel_type":        channelType,
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
// Query parameter format=cast|gif|webm (default cast). cast is the
// raw asciinema .cast; gif/webm require agg (and ffmpeg for webm) on
// PATH, otherwise the handler returns 503.
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
	case "cast", "gif", "webm":
	default:
		writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
		return
	}
	if a.DB == nil {
		writeJSONErrorKey(w, r, "recordings.notAvailable", http.StatusServiceUnavailable)
		return
	}
	var filePath string
	err := a.DB.QueryRowContext(r.Context(), `SELECT file_path FROM recordings WHERE id = ? AND user_id = ?`, recordingID, userID).Scan(&filePath)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONErrorKey(w, r, "recordings.notFound", http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
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
		return os.Open(path) // #nosec G304 -- path validated above (no .. or prefix).
	}
	sanitizeBasename := func(name string) string {
		const ext = ".cast"
		if !strings.HasSuffix(name, ext) {
			s := strings.ReplaceAll(name, ":", "-")
			return strings.ReplaceAll(s, ".", "-")
		}
		prefix := name[:len(name)-len(ext)]
		safe := strings.ReplaceAll(prefix, ":", "-")
		safe = strings.ReplaceAll(safe, ".", "-")
		return safe + ext
	}
	castPath := filePath
	f, err := tryOpen(filePath)
	if err != nil {
		dir, base := filepath.Dir(filePath), filepath.Base(filePath)
		safeBase := sanitizeBasename(base)
		if safeBase != base {
			castPath = filepath.Join(dir, safeBase)
			f, err = tryOpen(castPath)
		}
	}
	if err != nil {
		writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
		return
	}
	if format == "cast" {
		defer f.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(castPath))
		_, _ = io.Copy(w, f)
		return
	}
	f.Close()
	// GIF or WebM: convert with agg (and ffmpeg for webm).
	outPath, contentType, disposition, err := convertCastToVideo(castPath, format)
	if err != nil {
		audit("recording_convert_failed", auditFields{
			"id":     recordingID,
			"format": format,
			"error":  err.Error(),
		})
		writeJSONErrorKey(w, r, "recordings.videoUnavailable", http.StatusServiceUnavailable, "error", err)
		return
	}
	defer os.Remove(outPath)
	out, err := os.Open(outPath) // #nosec G304 -- path from convertCastToVideo (temp file we created).
	if err != nil {
		writeJSONErrorKey(w, r, "recordings.convertReadFailed", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	_, _ = io.Copy(w, out)
}

// convertCastToVideo converts a .cast file to gif or webm using agg
// (and ffmpeg for webm). It returns (outPath, contentType,
// contentDisposition, error); the caller is responsible for
// os.Remove(outPath).
func convertCastToVideo(castPath, format string) (string, string, string, error) {
	aggPath, err := exec.LookPath("agg")
	if err != nil {
		return "", "", "", errors.New("agg not found in PATH (install asciinema-agg for GIF/WebM export)")
	}
	dir := filepath.Dir(castPath)
	gifFile, err := os.CreateTemp(dir, "rec-*.gif")
	if err != nil {
		return "", "", "", err
	}
	gifPath := gifFile.Name()
	gifFile.Close()
	defer func() {
		if format == "webm" {
			_ = os.Remove(gifPath)
		}
	}()
	cmd := exec.Command(aggPath, castPath, gifPath) // #nosec G204 -- paths from validated castPath and temp file.
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(gifPath)
		return "", "", "", errors.New(strings.TrimSpace(string(out)) + ": " + runErr.Error())
	}
	if format == "gif" {
		return gifPath, "image/gif", `attachment; filename="recording.gif"`, nil
	}
	// WebM: ffmpeg -i gifPath -c:v libvpx-vp9 -pix_fmt yuv420p -an -b:v 0 -crf 30 out.webm.
	// Use yuv420p (not yuva420p) because terminal GIFs from agg are
	// opaque. -vf scale ensures even dimensions.
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", "", "", errors.New("ffmpeg not found in PATH (required for WebM export)")
	}
	webmFile, err := os.CreateTemp(dir, "rec-*.webm")
	if err != nil {
		return "", "", "", err
	}
	webmPath := webmFile.Name()
	webmFile.Close()
	cmd = exec.Command(ffmpegPath, "-y", "-i", gifPath,
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-c:v", "libvpx-vp9", "-pix_fmt", "yuv420p", "-an", "-b:v", "0", "-crf", "30",
		webmPath) // #nosec G204
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(webmPath)
		return "", "", "", errors.New("ffmpeg: " + strings.TrimSpace(string(out)))
	}
	return webmPath, "video/webm", `attachment; filename="recording.webm"`, nil
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
