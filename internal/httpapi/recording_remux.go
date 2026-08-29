package httpapi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nullpo7z/vantyx/internal/recording"
)

// remuxBackfillDelay is how long after start-up the backfill waits before
// touching existing recordings, so it never competes with the server
// coming up. Overridable in tests.
var remuxBackfillDelay = 30 * time.Second

// remuxRecordingAsync finalises a just-stopped RDP/VNC recording in the
// background: the live capture is written as a fragmented MP4 (crash
// safe, but with no total duration), and this rewrites it in place as a
// regular faststart MP4 so the player shows the full length and the
// timeline is fully seekable (B-1).
func (a *App) remuxRecordingAsync(recordingID, path string) {
	go a.remuxRecording(context.Background(), recordingID, path, "recording_stop")
}

// remuxRecording remuxes path if (and only if) it is a fragmented MP4
// under VANTYX_RECORDINGS_DIR. Returns true when a remux was performed.
func (a *App) remuxRecording(ctx context.Context, recordingID, path, reason string) bool {
	if !recording.IsMP4Path(path) {
		return false
	}
	recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
	if recordingDir == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	resolved, err := openRecordingPath(recordingDir, absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			audit("recording_remux_failed", auditFields{
				"recording_id": recordingID,
				"path":         path,
				"reason":       reason,
				"error":        err.Error(),
			})
		}
		return false
	}
	frag, err := recording.IsFragmentedMP4(resolved)
	if err != nil {
		audit("recording_remux_failed", auditFields{
			"recording_id": recordingID,
			"path":         path,
			"reason":       reason,
			"error":        err.Error(),
		})
		return false
	}
	if !frag {
		return false
	}
	gov := recording.DefaultGovernor()
	acquireCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	// Use the export slot: like GIF/MP4 exports this is post-processing
	// and must never delay a live capture from starting.
	if err := gov.AcquireExport(acquireCtx); err != nil {
		audit("recording_remux_failed", auditFields{
			"recording_id": recordingID,
			"path":         path,
			"reason":       reason,
			"error":        err.Error(),
		})
		return false
	}
	defer gov.ReleaseExport()
	started := time.Now()
	if err := recording.RemuxFaststart(ctx, resolved, gov.FFmpegThreads()); err != nil {
		audit("recording_remux_failed", auditFields{
			"recording_id": recordingID,
			"path":         path,
			"reason":       reason,
			"error":        err.Error(),
		})
		return false
	}
	audit("recording_remux_ok", auditFields{
		"recording_id": recordingID,
		"path":         path,
		"reason":       reason,
		"duration_ms":  time.Since(started).Milliseconds(),
	})
	return true
}

// startRecordingRemuxBackfill converts, in the background and one at a
// time, every finished MP4 recording that is still fragmented (written
// before remux-on-stop existed). It is a no-op when recordings or ffmpeg
// are not configured, and it only runs after remuxBackfillDelay.
func (a *App) startRecordingRemuxBackfill() {
	if a == nil || a.DB == nil {
		return
	}
	if os.Getenv("VANTYX_RECORDINGS_DIR") == "" {
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return
	}
	go func() {
		time.Sleep(remuxBackfillDelay)
		ctx := context.Background()
		rows, err := a.DB.QueryContext(ctx, `SELECT id, file_path FROM recordings WHERE ended_at IS NOT NULL AND ended_at <> '' AND lower(file_path) LIKE '%.mp4' ORDER BY started_at`)
		if err != nil {
			httpLogger.Warn("recording remux backfill: query failed", "error", err)
			return
		}
		type rec struct{ id, path string }
		var todo []rec
		for rows.Next() {
			var r rec
			if err := rows.Scan(&r.id, &r.path); err == nil {
				todo = append(todo, r)
			}
		}
		rows.Close()
		done := 0
		for _, r := range todo {
			if a.remuxRecording(ctx, r.id, r.path, "backfill") {
				done++
			}
		}
		if done > 0 {
			httpLogger.Info("recording remux backfill finished", "remuxed", done, "scanned", len(todo))
		}
	}()
}
