// Package rdpvnc manages the lifecycle of xfreerdp → Xvfb → x11vnc bridges
// so that an RDP target can be viewed via noVNC in the browser.
package rdpvnc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
)

// Bridge holds the three child processes that form a single RDP-to-VNC session.
type Bridge struct {
	display    int
	vncPort    int
	width      int
	height     int
	xvfb       *exec.Cmd
	freerdp    *exec.Cmd
	freerdpPty *os.File
	x11vnc     *exec.Cmd
	cancel     context.CancelFunc
	done       chan struct{}
}

var displayCounter int64 = 49

func nextDisplay() int {
	return int(atomic.AddInt64(&displayCounter, 1))
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}

// Start launches Xvfb, xfreerdp, and x11vnc. The returned Bridge exposes a
// local VNC port that noVNC can connect to. Call Stop() when done.
func Start(ctx context.Context, host string, port int, username, password string, width, height int) (*Bridge, error) {
	display := nextDisplay()
	displayStr := fmt.Sprintf(":%d", display)

	vncPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("find free port: %w", err)
	}

	bridgeCtx, cancel := context.WithCancel(ctx)
	b := &Bridge{
		display: display,
		vncPort: vncPort,
		width:   width,
		height:  height,
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	screenSpec := fmt.Sprintf("%dx%dx24", width, height)
	b.xvfb = exec.CommandContext(bridgeCtx, "Xvfb", displayStr, "-screen", "0", screenSpec, "-ac", "-nolisten", "tcp") // #nosec G204 -- args are constructed from validated internal values
	if err := b.xvfb.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start Xvfb: %w", err)
	}

	if err := waitForDisplay(bridgeCtx, displayStr, 5*time.Second); err != nil {
		cancel()
		_ = b.xvfb.Process.Kill()
		return nil, fmt.Errorf("Xvfb not ready: %w", err)
	}
	// Log the actual X screen size so we can confirm the bridge matches requested geometry.
	if w, h, err := xDisplaySize(bridgeCtx, displayStr); err != nil {
		slog.Warn("rdpvnc: failed to read X display size", "display", display, "err", err)
	} else {
		slog.Info("rdpvnc: X display ready", "display", display, "requested_w", width, "requested_h", height, "x_w", w, "x_h", h)
	}

	rdpAddr := fmt.Sprintf("/v:%s:%d", host, port)
	args := []string{
		rdpAddr,
		fmt.Sprintf("/size:%dx%d", width, height),
		// Dynamic resolution support (mstsc-like behavior). When the remote desktop
		// changes its resolution, allow FreeRDP to adjust without forcing a reconnect.
		"/dynamic-resolution",
		"/cert:ignore",
		"/network:lan",
		"+clipboard",
		"/gfx",
	}
	if username != "" {
		args = append(args, "/u:"+username)
	}
	if password != "" {
		args = append(args, "/p:"+password)
	}

	var freerdpStderr bytes.Buffer
	b.freerdp = exec.CommandContext(bridgeCtx, "xfreerdp3", args...) // #nosec G204 -- args are constructed from validated target fields
	b.freerdp.Env = append(b.freerdp.Environ(), "DISPLAY="+displayStr)
	b.freerdp.Stderr = &freerdpStderr

	ptmx, err := pty.Start(b.freerdp)
	if err != nil {
		cancel()
		_ = b.xvfb.Process.Kill()
		return nil, fmt.Errorf("start xfreerdp with pty: %w", err)
	}
	b.freerdpPty = ptmx
	go func() { _, _ = io.Copy(io.Discard, ptmx) }()

	freerdpDone := make(chan error, 1)
	go func() { freerdpDone <- b.freerdp.Wait() }()
	select {
	case err := <-freerdpDone:
		_ = ptmx.Close()
		cancel()
		_ = b.xvfb.Process.Kill()
		errMsg := freerdpStderr.String()
		slog.Error("rdpvnc: xfreerdp exited early", "err", err, "stderr", errMsg)
		return nil, fmt.Errorf("xfreerdp exited immediately: %s", errMsg)
	case <-time.After(2 * time.Second):
	}

	b.x11vnc = exec.CommandContext(bridgeCtx, "x11vnc", // #nosec G204 -- args are constructed from validated internal values
		"-display", displayStr,
		"-rfbport", fmt.Sprintf("%d", vncPort),
		"-nopw",
		"-forever",
		"-shared",
		// Follow X RandR screen size changes (framebuffer resize events).
		"-xrandr",
		// Prefer incremental updates for responsiveness.
		// (Disabling XDamage/XFixes tends to increase bandwidth/CPU and feels sluggish.)
	)
	if err := b.x11vnc.Start(); err != nil {
		cancel()
		_ = b.freerdp.Process.Kill()
		_ = b.xvfb.Process.Kill()
		return nil, fmt.Errorf("start x11vnc: %w", err)
	}

	if err := waitForPort(bridgeCtx, vncPort, 5*time.Second); err != nil {
		cancel()
		_ = b.x11vnc.Process.Kill()
		_ = b.freerdp.Process.Kill()
		_ = b.xvfb.Process.Kill()
		return nil, fmt.Errorf("x11vnc not ready: %w", err)
	}
	// After xfreerdp and x11vnc are up, re-check X size (dynamic-resolution may change it).
	if w, h, err := xDisplaySize(bridgeCtx, displayStr); err != nil {
		slog.Warn("rdpvnc: failed to read X display size (post-start)", "display", display, "err", err)
	} else {
		slog.Info("rdpvnc: bridge display size", "display", display, "x_w", w, "x_h", h, "vnc_port", vncPort)
	}

	go func() {
		defer close(b.done)
		err := <-freerdpDone
		slog.Info("rdpvnc: xfreerdp exited", "display", display, "err", err, "stderr", freerdpStderr.String())
		cancel()
		killProc(b.x11vnc)
		killProc(b.xvfb)
	}()

	slog.Info("rdpvnc: bridge started", "display", display, "vnc_port", vncPort, "target", fmt.Sprintf("%s:%d", host, port))
	return b, nil
}

// VNCPort returns the local TCP port where x11vnc listens.
func (b *Bridge) VNCPort() int { return b.vncPort }

// Size returns the screen size used for Xvfb/xfreerdp.
func (b *Bridge) Size() (int, int) { return b.width, b.height }

// Done returns a channel closed when the bridge processes have exited.
func (b *Bridge) Done() <-chan struct{} { return b.done }

// Stop kills all child processes and waits for cleanup.
func (b *Bridge) Stop() {
	b.cancel()
	killProc(b.freerdp)
	killProc(b.x11vnc)
	killProc(b.xvfb)
	if b.freerdpPty != nil {
		_ = b.freerdpPty.Close()
	}

	select {
	case <-b.done:
	case <-time.After(5 * time.Second):
	}
}

func killProc(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// waitForDisplay polls xdpyinfo until the X display is available.
func waitForDisplay(ctx context.Context, display string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for display %s", display)
		default:
		}
		cmd := exec.CommandContext(ctx, "xdpyinfo", "-display", display) // #nosec G204 -- display is an internally generated value
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var (
	xrandrCurrentRe = regexp.MustCompile(`(?m)\bcurrent\s+(\d+)\s+x\s+(\d+)\b`)
	xdpyDimsRe      = regexp.MustCompile(`(?m)dimensions:\s+(\d+)x(\d+)\s+pixels`)
)

// xDisplaySize returns the current pixel size of the X display.
// It tries xrandr first (RandR), then falls back to xdpyinfo.
func xDisplaySize(ctx context.Context, display string) (int, int, error) {
	// Prefer xrandr because it reflects RandR changes (dynamic resize).
	{
		cmd := exec.CommandContext(ctx, "xrandr", "-display", display) // #nosec G204 -- display is internally generated
		out, err := cmd.CombinedOutput()
		if err == nil {
			if m := xrandrCurrentRe.FindSubmatch(out); len(m) == 3 {
				w, _ := strconv.Atoi(string(m[1]))
				h, _ := strconv.Atoi(string(m[2]))
				if w > 0 && h > 0 {
					return w, h, nil
				}
			}
		}
	}
	{
		cmd := exec.CommandContext(ctx, "xdpyinfo", "-display", display) // #nosec G204 -- display is internally generated
		out, err := cmd.CombinedOutput()
		if err != nil {
			return 0, 0, fmt.Errorf("xdpyinfo failed: %w", err)
		}
		if m := xdpyDimsRe.FindSubmatch(out); len(m) == 3 {
			w, _ := strconv.Atoi(string(m[1]))
			h, _ := strconv.Atoi(string(m[2]))
			if w > 0 && h > 0 {
				return w, h, nil
			}
		}
		return 0, 0, fmt.Errorf("could not parse X size from xdpyinfo")
	}
}

// waitForPort polls a TCP port until it accepts connections.
func waitForPort(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.After(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for port %d", port)
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Manager tracks active RDP-to-VNC bridges for cleanup.
type Manager struct {
	mu      sync.Mutex
	bridges map[string]*Bridge
}

// NewManager creates a new bridge manager.
func NewManager() *Manager {
	return &Manager{bridges: make(map[string]*Bridge)}
}

// Register adds a bridge under the given key.
func (m *Manager) Register(key string, b *Bridge) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.bridges[key]; ok {
		old.Stop()
	}
	m.bridges[key] = b
	go func() {
		<-b.Done()
		m.mu.Lock()
		if m.bridges[key] == b {
			delete(m.bridges, key)
		}
		m.mu.Unlock()
	}()
}

// Remove stops and removes the bridge for the given key.
func (m *Manager) Remove(key string) {
	m.mu.Lock()
	b, ok := m.bridges[key]
	if ok {
		delete(m.bridges, key)
	}
	m.mu.Unlock()
	if ok {
		b.Stop()
	}
}

// ActiveTargetIDsForUser returns target IDs that have an active bridge for the given user.
// Bridge keys are "userID:targetID".
func (m *Manager) ActiveTargetIDsForUser(userID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := userID + ":"
	var out []string
	for key := range m.bridges {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			out = append(out, key[len(prefix):])
		}
	}
	return out
}

// GetBridge returns the bridge for the given key if it exists.
func (m *Manager) GetBridge(key string) (*Bridge, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bridges[key]
	return b, ok
}

// StopAll terminates all active bridges.
func (m *Manager) StopAll() {
	m.mu.Lock()
	all := make([]*Bridge, 0, len(m.bridges))
	for _, b := range m.bridges {
		all = append(all, b)
	}
	m.bridges = make(map[string]*Bridge)
	m.mu.Unlock()
	for _, b := range all {
		b.Stop()
	}
}
