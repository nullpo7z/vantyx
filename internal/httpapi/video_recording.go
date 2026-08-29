package httpapi

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/nullpo7z/vantyx/internal/recording"
)

type videoRecordingStopper interface {
	Stop() error
}

type videoRecordingHandle struct {
	stop videoRecordingStopper
	path string
	// cancel cancels the recording context created in
	// startVideoRecording. It is invoked once the capture has been
	// stopped so the parent-cancellation monitor goroutine exits instead
	// of blocking forever (the capture is decoupled from the HTTP request
	// via context.Background(), so nothing else ever cancels recCtx).
	cancel  context.CancelFunc
	release func()
}

type governedCapture struct {
	inner videoRecordingStopper
	once  sync.Once
	done  func()
}

func (g *governedCapture) Stop() error {
	var err error
	g.once.Do(func() {
		if g.inner != nil {
			err = g.inner.Stop()
		}
		if g.done != nil {
			g.done()
		}
	})
	return err
}

// videoRecordingRegistry tracks in-flight RDP/VNC screen recordings keyed by session ID.
type videoRecordingRegistry struct {
	mu      sync.Mutex
	active  map[string]*videoRecordingHandle
	started map[string]bool
	pending map[string]context.CancelFunc
}

func newVideoRecordingRegistry() *videoRecordingRegistry {
	return &videoRecordingRegistry{
		active:  make(map[string]*videoRecordingHandle),
		started: make(map[string]bool),
		pending: make(map[string]context.CancelFunc),
	}
}

func (a *App) videoRegistry() *videoRecordingRegistry {
	if a == nil {
		return nil
	}
	if a.videoRecordings == nil {
		a.videoRecordings = newVideoRecordingRegistry()
	}
	return a.videoRecordings
}

func (r *videoRecordingRegistry) beginPending(sessionID string, cancel context.CancelFunc) bool {
	if r == nil || sessionID == "" || cancel == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.active[sessionID]; ok {
		return false
	}
	if _, ok := r.pending[sessionID]; ok {
		return false
	}
	r.pending[sessionID] = cancel
	return true
}

func (r *videoRecordingRegistry) endPending(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	delete(r.pending, sessionID)
	r.mu.Unlock()
}

func (r *videoRecordingRegistry) cancelPending(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	cancel := r.pending[sessionID]
	delete(r.pending, sessionID)
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) beginVideoRecordingRow(ctx context.Context, sessionID, userID, targetID, channelType, videoPath string) {
	if a == nil || a.DB == nil {
		return
	}
	startedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err := a.InsertRecording(ctx, sessionID, userID, targetID, sessionID, channelType, videoPath, startedAt, "", ""); err != nil {
		audit("recording_insert_failed", auditFields{
			"session_id": sessionID,
			"channel":    channelType,
			"error":      err.Error(),
		})
		return
	}
	reg := a.videoRegistry()
	if reg != nil {
		reg.mu.Lock()
		reg.started[sessionID] = true
		reg.mu.Unlock()
	}
}

func (a *App) registerVideoRecording(sessionID string, stopper videoRecordingStopper, path string, release func()) {
	reg := a.videoRegistry()
	if reg == nil || stopper == nil {
		if release != nil {
			release()
		}
		return
	}
	reg.mu.Lock()
	if prev, ok := reg.active[sessionID]; ok && prev != nil {
		reg.mu.Unlock()
		a.stopVideoRecordingHandle(prev)
		reg.mu.Lock()
	}
	// Carry the recording's cancel func (registered by beginPending) onto
	// the handle so stopVideoRecordingHandle can release recCtx and let
	// the monitor goroutine exit. It may be absent if finishVideoRecording
	// already cancelled+removed it in a race, in which case recCtx is
	// already done and nil here is correct.
	cancel := reg.pending[sessionID]
	reg.active[sessionID] = &videoRecordingHandle{stop: stopper, path: path, cancel: cancel, release: release}
	delete(reg.pending, sessionID)
	reg.mu.Unlock()
}

func (a *App) stopVideoRecordingHandle(handle *videoRecordingHandle) {
	if handle == nil {
		return
	}
	if handle.stop != nil {
		_ = handle.stop.Stop()
	}
	// Cancel recCtx only after the capture is stopped: Stop() SIGINTs
	// ffmpeg so it finalizes the mp4, then cancelling its (already-exited)
	// exec context is a harmless no-op that also unblocks the monitor
	// goroutine from startVideoRecording. Cancelling before Stop would
	// SIGKILL ffmpeg and truncate the recording.
	if handle.cancel != nil {
		handle.cancel()
	}
	if handle.release != nil {
		handle.release()
	}
}

func (a *App) finishVideoRecording(sessionID string) {
	reg := a.videoRegistry()
	if reg == nil || sessionID == "" {
		return
	}
	reg.cancelPending(sessionID)

	reg.mu.Lock()
	handle, ok := reg.active[sessionID]
	delete(reg.active, sessionID)
	_, started := reg.started[sessionID]
	reg.mu.Unlock()

	if ok {
		a.stopVideoRecordingHandle(handle)
	}
	if started && a != nil && a.DB != nil {
		endedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
		_ = a.UpdateRecordingEnded(context.Background(), sessionID, endedAt)
	}
	// ffmpeg has exited (Stop waited for it), so the fragmented MP4 is
	// complete: rewrite it as a faststart MP4 in the background (B-1).
	if ok && started && handle != nil && handle.path != "" {
		a.remuxRecordingAsync(sessionID, handle.path)
	}
}

func (a *App) startRDPVideoRecording(parentCtx context.Context, sessionID, userID, targetID string, display, width, height int) {
	a.startVideoRecording(parentCtx, sessionID, userID, targetID, "rdp", func(ctx context.Context, threads int) (videoRecordingStopper, string, error) {
		recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
		if recordingDir == "" || a == nil || a.DB == nil {
			return nil, "", os.ErrInvalid
		}
		_ = os.MkdirAll(recordingDir, 0o750) // #nosec G703
		mp4Path := recording.MP4Path(recordingDir, sessionID)
		rec, err := recording.StartX11Grab(ctx, display, width, height, 0, threads, mp4Path)
		if err != nil {
			return nil, "", err
		}
		return rec, mp4Path, nil
	})
}

func (a *App) startVNCVideoRecording(parentCtx context.Context, sessionID, userID, targetID, host string, port int, password string) {
	a.startVideoRecording(parentCtx, sessionID, userID, targetID, "vnc", func(ctx context.Context, threads int) (videoRecordingStopper, string, error) {
		recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
		if recordingDir == "" || a == nil || a.DB == nil {
			return nil, "", os.ErrInvalid
		}
		_ = os.MkdirAll(recordingDir, 0o750) // #nosec G703
		mp4Path := recording.MP4Path(recordingDir, sessionID)
		cap, err := recording.StartVNCCapture(ctx, host, port, password, 1920, 1080, 0, mp4Path)
		if err != nil {
			return nil, "", err
		}
		return cap, mp4Path, nil
	})
}

type captureStarter func(ctx context.Context, ffmpegThreads int) (videoRecordingStopper, string, error)

func (a *App) startVideoRecording(parentCtx context.Context, sessionID, userID, targetID, channelType string, start captureStarter) {
	recCtx, recCancel := context.WithCancel(context.Background())
	if parentCtx != nil {
		go func() {
			select {
			case <-parentCtx.Done():
				recCancel()
			case <-recCtx.Done():
			}
		}()
	}
	reg := a.videoRegistry()
	if reg == nil || !reg.beginPending(sessionID, recCancel) {
		recCancel()
		return
	}
	go func() {
		defer reg.endPending(sessionID)
		a.runGovernedCapture(recCtx, sessionID, userID, targetID, channelType, start)
	}()
}

func (a *App) runGovernedCapture(ctx context.Context, sessionID, userID, targetID, channelType string, start captureStarter) {
	if ctx.Err() != nil {
		return
	}
	gov := recording.DefaultGovernor()
	acquireCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := gov.Acquire(acquireCtx); err != nil {
		audit("recording_create_failed", auditFields{
			"session_id": sessionID,
			"channel":    channelType,
			"error":      err.Error(),
		})
		return
	}
	released := false
	release := func() {
		if !released {
			released = true
			gov.Release()
		}
	}
	defer release()

	if ctx.Err() != nil {
		return
	}
	inner, videoPath, err := start(ctx, gov.FFmpegThreads())
	if err != nil {
		audit("recording_create_failed", auditFields{
			"session_id": sessionID,
			"channel":    channelType,
			"path":       videoPath,
			"error":      err.Error(),
		})
		return
	}
	if ctx.Err() != nil {
		_ = inner.Stop()
		return
	}
	a.beginVideoRecordingRow(ctx, sessionID, userID, targetID, channelType, videoPath)
	wrapped := &governedCapture{
		inner: inner,
		done:  release,
	}
	a.registerVideoRecording(sessionID, wrapped, videoPath, nil)
}

func recordingMediaType(channelType, filePath string) string {
	switch channelType {
	case "rdp", "vnc":
		return "video"
	}
	if recording.IsMP4Path(filePath) {
		return "video"
	}
	return "cast"
}
