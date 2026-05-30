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
	errRecordingVideoTools  = errors.New("recording video tools missing")
	errRecordingVideoExport = errors.New("recording video export failed")
)

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
	// NOTE: multi-use link invitations only record the first consumer's user ID
	// today (invitee_user_id is COALESCE'd), so this check is best-effort for that
	// mode until we introduce a per-consumer usage table.
	err := a.DB.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM session_invitations
		WHERE session_id = ?
		  AND invitee_user_id = ?
		  AND revoked_at IS NULL
		  AND use_count > 0
	`, sessionID, userID).Scan(&n)
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

	query := `SELECT id, user_id, target_id, session_id, channel_type, started_at, ended_at, COALESCE(session_name, ''), COALESCE(session_description, '') FROM recordings WHERE `
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
		))`
		args = append(args, filterUserID, userID)
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
			"media_type":          recordingMediaType(channelType, ""),
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
		return os.Open(path) // #nosec G304 -- path validated above (no .. or prefix).
	}
	sanitizeBasename := func(name string) string {
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

	if recording.IsWebMPath(mediaPath) {
		f.Close()
		switch format {
		case "cast":
			writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
			return
		case "webm":
			serveFile, err := tryOpen(mediaPath)
			if err != nil {
				writeJSONErrorKey(w, r, "recordings.fileNotFound", http.StatusNotFound)
				return
			}
			defer serveFile.Close()
			w.Header().Set("Content-Type", "video/webm")
			setAttachmentDisposition(w, filepath.Base(mediaPath))
			_, _ = io.Copy(w, serveFile)
			return
		case "gif":
			wmText := watermarkTextForRecording(userID, sessionID.String, startedAt.String)
			job, err := a.enqueueRecordingExport(userID, recordingID, "gif", mediaPath, wmText)
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
		wmText := watermarkTextForRecording(userID, sessionID.String, startedAt.String)
		job, err := a.enqueueRecordingExport(userID, recordingID, format, castPath, wmText)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		writeRecordingExportAccepted(w, job)
		return
	}
	writeJSONErrorKey(w, r, "recordings.formatInvalid", http.StatusBadRequest)
}

func watermarkTextForRecording(userID, sessionID, startedAt string) string {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	startedAt = strings.TrimSpace(startedAt)
	stamp := ""
	if startedAt != "" {
		stamp = "Recorded: " + startedAt
	} else {
		stamp = "Generated: " + time.Now().UTC().Format(time.RFC3339)
	}
	base := ""
	if userID != "" && sessionID != "" {
		base = "User: " + userID + " · Session: " + sessionID
	} else if userID != "" {
		base = "User: " + userID
	} else if sessionID != "" {
		base = "Session: " + sessionID
	} else {
		base = "Vantyx"
	}
	return base + " · " + stamp
}

func writeWatermarkTextFile(dir, text string) (string, error) {
	f, err := os.CreateTemp(dir, "wm-*.txt")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_, werr := f.WriteString(strings.ReplaceAll(text, "\n", " ") + "\n")
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(name)
		return "", werr
	}
	if cerr != nil {
		_ = os.Remove(name)
		return "", cerr
	}
	return name, nil
}

// Watermark tile geometry matches web playback (recordings_page.js).
const (
	watermarkTileW     = 420
	watermarkTileH     = 300
	watermarkFontSize  = "16"
	watermarkFontAlpha = "0.28"
	// Tailwind -rotate-12
	watermarkRotateRad = "-12*PI/180"
)

func probeVideoSize(ctx context.Context, path string) (width, height int, err error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, 0, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, ffprobe, "-nostdin", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", path) // #nosec G204 G702 -- ffprobe path from LookPath; media path from validated recording dir.
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("ffprobe: unexpected size %q", strings.TrimSpace(string(out)))
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("ffprobe: invalid dimensions %q", strings.TrimSpace(string(out)))
	}
	return w, h, nil
}

func watermarkTileLayout(videoW, videoH int) (cols, rows int) {
	cols = (videoW + watermarkTileW - 1) / watermarkTileW
	rows = (videoH + watermarkTileH - 1) / watermarkTileH
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

func exportVideoDimensions(ctx context.Context, path string) (width, height int) {
	w, h, err := probeVideoSize(ctx, path)
	if err != nil || w <= 0 || h <= 0 {
		return 1920, 1080
	}
	return w, h
}

func lavfiWatermarkTileInput() string {
	return fmt.Sprintf("color=c=black@0:s=%dx%d,format=rgba", watermarkTileW, watermarkTileH)
}

// buildFastTiledWatermarkOverlay tiles a rotated watermark cell across the
// full frame (matching SPA playback) using ffmpeg's tile filter and a single
// overlay, which is much faster than chaining dozens of overlay filters.
func buildFastTiledWatermarkOverlay(videoW, videoH int, textFile string) string {
	cols, rows := watermarkTileLayout(videoW, videoH)
	draw := drawtextOnWatermarkTile(textFile)
	return fmt.Sprintf(
		"[1:v]%s[wmcell];"+
			"[wmcell]tile=layout=%dx%d:margin=0:padding=0:init_padding=0[fullwm];"+
			"[fullwm]crop=%d:%d:0:0[fullwm2];"+
			"[0:v][fullwm2]overlay=0:0:format=auto",
		draw, cols, rows, videoW, videoH,
	)
}

func drawtextOnWatermarkTile(textFile string) string {
	// Two stacked lines like the SPA watermark block.
	base := "drawtext=textfile=" + textFile +
		":reload=0:fontsize=" + watermarkFontSize +
		":fontcolor=white@" + watermarkFontAlpha +
		":shadowcolor=black@0.35:shadowx=1:shadowy=1"
	return base + ":x=(w-text_w)/2:y=(h-text_h)/2-10," +
		base + ":x=(w-text_w)/2:y=(h-text_h)/2+10," +
		"rotate=angle=" + watermarkRotateRad + ":fillcolor=black@0:ow=iw:oh=ih"
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

func vp9VideoArgs(outputPath string) []string {
	return []string{
		"-c:v", "libvpx-vp9",
		"-pix_fmt", "yuv420p",
		"-an",
		"-b:v", "0",
		"-crf", "30",
		"-deadline", "realtime",
		"-cpu-used", "5",
		outputPath,
	}
}

// convertCastToVideo converts a .cast file to gif or webm using agg
// (and ffmpeg for webm). It returns (outPath, contentType,
// contentDisposition, error); the caller is responsible for
// os.Remove(outPath).
func convertCastToVideo(ctx context.Context, castPath, format, watermarkText string, ffmpegThreads int, onProgress exportProgressFunc) (string, string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if onProgress != nil {
		onProgress("agg", 10)
	}
	aggPath, err := exec.LookPath("agg")
	if err != nil {
		auditRecordingExportFailure("agg_lookup", nil, err)
		return "", "", "", errRecordingVideoTools
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		auditRecordingExportFailure("ffmpeg_lookup", nil, err)
		return "", "", "", errRecordingVideoTools
	}
	if ffmpegThreads < 1 {
		ffmpegThreads = 1
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
	// `--` forces every subsequent argument to be treated as a
	// positional file path, so a basename that happens to start with
	// "-" cannot be re-interpreted as an agg flag (CWE-88).
	cmd := exec.CommandContext(ctx, aggPath, "--", castPath, gifPath) // #nosec G204 G702 -- paths from validated castPath and temp file; `--` blocks option injection.
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(gifPath)
		auditRecordingExportFailure("agg", out, runErr)
		return "", "", "", errRecordingVideoExport
	}
	if onProgress != nil {
		onProgress("agg", 45)
	}
	wmFile := ""
	if strings.TrimSpace(watermarkText) != "" {
		wmFile, err = writeWatermarkTextFile(dir, watermarkText)
		if err != nil {
			_ = os.Remove(gifPath)
			return "", "", "", err
		}
		defer os.Remove(wmFile)
	}

	wmTextFileEsc := ""
	if wmFile != "" {
		// Path may contain ":"; escape it for ffmpeg filters.
		wmTextFileEsc = strings.ReplaceAll(wmFile, ":", "\\:")
	}

	if format == "gif" {
		if onProgress != nil {
			onProgress("ffmpeg", 50)
		}
		outGif, err := os.CreateTemp(dir, "rec-wm-*.gif")
		if err != nil {
			_ = os.Remove(gifPath)
			return "", "", "", err
		}
		outGifPath := outGif.Name()
		outGif.Close()
		// Better GIF quality: palettegen/paletteuse after watermark.
		filter := "[0:v]split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse"
		var args []string
		if wmTextFileEsc != "" {
			vw, vh := exportVideoDimensions(ctx, gifPath)
			filter = buildFastTiledWatermarkOverlay(vw, vh, wmTextFileEsc) + ",split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse"
			args = []string{
				"-y", "-i", gifPath,
				"-f", "lavfi", "-i", lavfiWatermarkTileInput(),
				"-filter_complex", filter,
				"-loop", "0", outGifPath,
			}
		} else {
			args = []string{"-y", "-i", gifPath, "-filter_complex", filter, "-loop", "0", outGifPath}
		}
		if out, runErr := runFFmpeg(ctx, ffmpegPath, ffmpegThreads, args...); runErr != nil {
			_ = os.Remove(outGifPath)
			auditRecordingExportFailure("ffmpeg_gif", out, runErr)
			return "", "", "", errRecordingVideoExport
		}
		if onProgress != nil {
			onProgress("finalize", 95)
		}
		_ = os.Remove(gifPath)
		return outGifPath, "image/gif", `attachment; filename="recording.gif"`, nil
	}

	if onProgress != nil {
		onProgress("ffmpeg", 50)
	}
	webmFile, err := os.CreateTemp(dir, "rec-*.webm")
	if err != nil {
		return "", "", "", err
	}
	webmPath := webmFile.Name()
	webmFile.Close()
	args := []string{
		"-y", "-i", gifPath,
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
	}
	args = append(args, vp9VideoArgs(webmPath)...)
	if wmTextFileEsc != "" {
		vw, vh := exportVideoDimensions(ctx, gifPath)
		filter := buildFastTiledWatermarkOverlay(vw, vh, wmTextFileEsc) + ",scale=trunc(iw/2)*2:trunc(ih/2)*2"
		args = []string{
			"-y", "-i", gifPath,
			"-f", "lavfi", "-i", lavfiWatermarkTileInput(),
			"-filter_complex", filter,
		}
		args = append(args, vp9VideoArgs(webmPath)...)
	}
	if out, runErr := runFFmpeg(ctx, ffmpegPath, ffmpegThreads, args...); runErr != nil {
		_ = os.Remove(webmPath)
		auditRecordingExportFailure("ffmpeg_webm", out, runErr)
		return "", "", "", errRecordingVideoExport
	}
	if onProgress != nil {
		onProgress("finalize", 95)
	}
	return webmPath, "video/webm", `attachment; filename="recording.webm"`, nil
}

// convertWebMToGIF transcodes a WebM screen recording to GIF with optional watermark.
func convertWebMToGIF(ctx context.Context, webmPath, watermarkText string, ffmpegThreads int, onProgress exportProgressFunc) (string, string, string, error) {
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
	dir := filepath.Dir(webmPath)
	outGif, err := os.CreateTemp(dir, "rec-wm-*.gif")
	if err != nil {
		return "", "", "", err
	}
	outGifPath := outGif.Name()
	outGif.Close()

	wmFile := ""
	if strings.TrimSpace(watermarkText) != "" {
		wmFile, err = writeWatermarkTextFile(dir, watermarkText)
		if err != nil {
			_ = os.Remove(outGifPath)
			return "", "", "", err
		}
		defer os.Remove(wmFile)
	}
	wmTextFileEsc := ""
	if wmFile != "" {
		wmTextFileEsc = strings.ReplaceAll(wmFile, ":", "\\:")
	}

	filter := "[0:v]split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse"
	var args []string
	if wmTextFileEsc != "" {
		vw, vh := exportVideoDimensions(ctx, webmPath)
		filter = buildFastTiledWatermarkOverlay(vw, vh, wmTextFileEsc) + ",split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse"
		args = []string{
			"-y", "-i", webmPath,
			"-f", "lavfi", "-i", lavfiWatermarkTileInput(),
			"-filter_complex", filter,
			"-loop", "0", outGifPath,
		}
	} else {
		args = []string{"-y", "-i", webmPath, "-filter_complex", filter, "-loop", "0", outGifPath}
	}
	if out, runErr := runFFmpeg(ctx, ffmpegPath, ffmpegThreads, args...); runErr != nil {
		_ = os.Remove(outGifPath)
		auditRecordingExportFailure("ffmpeg_webm_gif", out, runErr)
		return "", "", "", errRecordingVideoExport
	}
	if onProgress != nil {
		onProgress("finalize", 95)
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
