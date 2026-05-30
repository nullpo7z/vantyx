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
	stop    videoRecordingStopper
	path    string
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
	started map[string]bool // session IDs with a DB row (including finished)
}

func newVideoRecordingRegistry() *videoRecordingRegistry {
	return &videoRecordingRegistry{
		active:  make(map[string]*videoRecordingHandle),
		started: make(map[string]bool),
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

func (r *videoRecordingRegistry) hasActive(sessionID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.active[sessionID]
	return ok
}

func (a *App) beginVideoRecordingRow(ctx context.Context, sessionID, userID, targetID, channelType, webmPath string) {
	if a == nil || a.DB == nil {
		return
	}
	startedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err := a.InsertRecording(ctx, sessionID, userID, targetID, sessionID, channelType, webmPath, startedAt, "", ""); err != nil {
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
	defer reg.mu.Unlock()
	reg.active[sessionID] = &videoRecordingHandle{stop: stopper, path: path, release: release}
}

func (a *App) finishVideoRecording(sessionID string) {
	reg := a.videoRegistry()
	if reg == nil || sessionID == "" {
		return
	}
	reg.mu.Lock()
	handle, ok := reg.active[sessionID]
	delete(reg.active, sessionID)
	_, started := reg.started[sessionID]
	reg.mu.Unlock()
	if ok && handle != nil {
		if handle.stop != nil {
			_ = handle.stop.Stop()
		}
		if handle.release != nil {
			handle.release()
		}
	}
	if started && a != nil && a.DB != nil {
		endedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
		_ = a.UpdateRecordingEnded(context.Background(), sessionID, endedAt)
	}
}

func (a *App) startRDPVideoRecording(ctx context.Context, sessionID, userID, targetID string, display, width, height int) {
	if a.videoRegistry().hasActive(sessionID) {
		return
	}
	go a.runGovernedCapture(ctx, sessionID, userID, targetID, "rdp", func(ctx context.Context, threads int) (videoRecordingStopper, string, error) {
		recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
		if recordingDir == "" || a == nil || a.DB == nil {
			return nil, "", os.ErrInvalid
		}
		_ = os.MkdirAll(recordingDir, 0o750) // #nosec G703
		webmPath := recording.WebMPath(recordingDir, sessionID)
		rec, err := recording.StartX11Grab(ctx, display, width, height, 0, threads, webmPath)
		if err != nil {
			return nil, "", err
		}
		return rec, webmPath, nil
	})
}

func (a *App) startVNCVideoRecording(ctx context.Context, sessionID, userID, targetID, host string, port int, password string) {
	go a.runGovernedCapture(ctx, sessionID, userID, targetID, "vnc", func(ctx context.Context, threads int) (videoRecordingStopper, string, error) {
		recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
		if recordingDir == "" || a == nil || a.DB == nil {
			return nil, "", os.ErrInvalid
		}
		_ = os.MkdirAll(recordingDir, 0o750) // #nosec G703
		webmPath := recording.WebMPath(recordingDir, sessionID)
		cap, err := recording.StartVNCCapture(ctx, host, port, password, 1920, 1080, 0, webmPath)
		if err != nil {
			return nil, "", err
		}
		return cap, webmPath, nil
	})
}

type captureStarter func(ctx context.Context, ffmpegThreads int) (videoRecordingStopper, string, error)

func (a *App) runGovernedCapture(ctx context.Context, sessionID, userID, targetID, channelType string, start captureStarter) {
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
	inner, webmPath, err := start(ctx, gov.FFmpegThreads())
	if err != nil {
		release()
		audit("recording_create_failed", auditFields{
			"session_id": sessionID,
			"channel":    channelType,
			"path":       webmPath,
			"error":      err.Error(),
		})
		return
	}
	a.beginVideoRecordingRow(ctx, sessionID, userID, targetID, channelType, webmPath)
	wrapped := &governedCapture{
		inner: inner,
		done:  release,
	}
	a.registerVideoRecording(sessionID, wrapped, webmPath, nil)
}

func recordingMediaType(channelType, filePath string) string {
	switch channelType {
	case "rdp", "vnc":
		return "video"
	}
	if recording.IsWebMPath(filePath) {
		return "video"
	}
	return "cast"
}
