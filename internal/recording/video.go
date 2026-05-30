package recording

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"time"
)

const defaultVideoFPS = 8

var vncDisplayCounter int64 = 149

func nextVNCDisplay() int {
	return int(atomic.AddInt64(&vncDisplayCounter, 1))
}

// VideoRecorder captures an X11 display to WebM via ffmpeg x11grab.
type VideoRecorder struct {
	cmd  *exec.Cmd
	path string
}

// StartX11Grab begins ffmpeg screen capture on the given X display number.
func StartX11Grab(ctx context.Context, display, width, height, fps, ffmpegThreads int, outputPath string) (*VideoRecorder, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}
	if display <= 0 || width <= 0 || height <= 0 {
		return nil, fmt.Errorf("recording: invalid geometry")
	}
	if fps <= 0 {
		fps = defaultVideoFPS
	}
	if ffmpegThreads < 1 {
		ffmpegThreads = DefaultGovernor().FFmpegThreads()
	}
	displayStr := fmt.Sprintf(":%d", display)
	args := []string{
		"-nostdin", "-y",
		"-threads", strconv.Itoa(ffmpegThreads),
		"-f", "x11grab",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", strconv.Itoa(fps),
		"-i", fmt.Sprintf("%s.0", displayStr),
		"-c:v", "libvpx-vp9",
		"-pix_fmt", "yuv420p",
		"-an",
		"-b:v", "0",
		"-crf", "32",
		outputPath,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...) // #nosec G204 -- fixed tool, validated paths.
	cmd.Env = append(os.Environ(), "DISPLAY="+displayStr)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	return &VideoRecorder{cmd: cmd, path: outputPath}, nil
}

// Path returns the output file path.
func (r *VideoRecorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// Stop signals ffmpeg to finalize the WebM file and waits for exit.
func (r *VideoRecorder) Stop() error {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	_ = r.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		_ = r.cmd.Process.Kill()
		return r.cmd.Wait()
	}
}
