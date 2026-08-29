package recording

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// VNCCapture records a remote VNC server by rendering it on a local Xvfb
// display (via vncviewer) and capturing with ffmpeg x11grab.
type VNCCapture struct {
	display    int
	xvfb       *exec.Cmd
	viewer     *exec.Cmd
	video      *VideoRecorder
	cancel     context.CancelFunc
	path       string
	passwdFile string
}

// StartVNCCapture launches Xvfb, vncviewer, and ffmpeg for the target VNC server.
func StartVNCCapture(ctx context.Context, host string, port int, password string, width, height, fps int, outputPath string) (*VNCCapture, error) {
	if host == "" || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("vnc capture: invalid target")
	}
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		return nil, fmt.Errorf("Xvfb not found: %w", err)
	}
	if _, err := exec.LookPath("vncviewer"); err != nil {
		return nil, fmt.Errorf("vncviewer not found: %w", err)
	}

	display := nextVNCDisplay()
	displayStr := fmt.Sprintf(":%d", display)
	captureCtx, cancel := context.WithCancel(ctx)
	c := &VNCCapture{
		display: display,
		cancel:  cancel,
		path:    outputPath,
	}

	screenSpec := fmt.Sprintf("%dx%dx24", width, height)
	c.xvfb = exec.CommandContext(captureCtx, "Xvfb", displayStr, "-screen", "0", screenSpec, "-ac", "-nolisten", "tcp") // #nosec G204
	if err := c.xvfb.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start Xvfb: %w", err)
	}
	if err := waitXDisplay(captureCtx, displayStr, 5*time.Second); err != nil {
		cancel()
		killCmd(c.xvfb)
		return nil, err
	}

	passwdFile, err := vncPasswdFile(password)
	if err != nil {
		cancel()
		killCmd(c.xvfb)
		return nil, err
	}
	c.passwdFile = passwdFile

	viewerArgs := []string{
		"-display", displayStr,
		"-geometry", fmt.Sprintf("%dx%d", width, height),
		"-ViewOnly",
		fmt.Sprintf("%s:%d", host, port),
	}
	if passwdFile != "" {
		viewerArgs = append([]string{"-PasswdFile", passwdFile}, viewerArgs...)
	} else {
		viewerArgs = append([]string{"-SecurityTypes", "None"}, viewerArgs...)
	}
	c.viewer = exec.CommandContext(captureCtx, "vncviewer", viewerArgs...) // #nosec G204
	c.viewer.Env = append(os.Environ(), "DISPLAY="+displayStr)
	if err := c.viewer.Start(); err != nil {
		cancel()
		_ = os.Remove(c.passwdFile)
		killCmd(c.xvfb)
		return nil, fmt.Errorf("start vncviewer: %w", err)
	}

	select {
	case <-captureCtx.Done():
		cancel()
		killCmd(c.viewer)
		killCmd(c.xvfb)
		return nil, captureCtx.Err()
	case <-time.After(2 * time.Second):
	}

	video, err := StartX11Grab(captureCtx, display, width, height, fps, DefaultGovernor().FFmpegThreads(), outputPath)
	if err != nil {
		cancel()
		_ = os.Remove(c.passwdFile)
		killCmd(c.viewer)
		killCmd(c.xvfb)
		return nil, err
	}
	c.video = video
	return c, nil
}

// Path returns the MP4 output path.
func (c *VNCCapture) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

// Stop terminates capture processes and finalizes the MP4 file.
func (c *VNCCapture) Stop() error {
	if c == nil {
		return nil
	}
	// Stop the ffmpeg recorder FIRST, while Xvfb/vncviewer are still
	// running, so it receives SIGINT (via VideoRecorder.Stop) and cleanly
	// finalizes the MP4 -- writing the moov atom / trailer -- before its
	// input display goes away. Cancelling captureCtx first would instead
	// SIGKILL ffmpeg mid-write and truncate the recording's tail.
	var err error
	if c.video != nil {
		err = c.video.Stop()
	}
	if c.cancel != nil {
		c.cancel()
	}
	killCmd(c.viewer)
	killCmd(c.xvfb)
	if c.passwdFile != "" {
		_ = os.Remove(c.passwdFile)
	}
	return err
}

func waitXDisplay(ctx context.Context, display string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for display %s", display)
		default:
		}
		cmd := exec.CommandContext(ctx, "xdpyinfo", "-display", display) // #nosec G204
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func vncPasswdFile(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", nil
	}
	if _, err := exec.LookPath("vncpasswd"); err != nil {
		return "", fmt.Errorf("vncpasswd not found: %w", err)
	}
	f, err := os.CreateTemp("", "vnc-pass-*.pw")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_ = f.Close()
	cmd := exec.Command("vncpasswd", "-f") // #nosec G204
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := os.OpenFile(name, os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G703 G304
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	cmd.Stdout = out
	runErr := cmd.Run()
	_ = out.Close()
	if runErr != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("vncpasswd: %w", runErr)
	}
	return name, nil
}

func killCmd(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
