// Package rdpvnc manages the lifecycle of xfreerdp → Xvfb → x11vnc bridges
// so that an RDP target can be viewed via noVNC in the browser.
package rdpvnc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Bridge holds the three child processes that form a single RDP-to-VNC session.
type Bridge struct {
	display int
	vncPort int
	xvfb    *exec.Cmd
	freerdp *exec.Cmd
	x11vnc  *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
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

	rdpAddr := fmt.Sprintf("/v:%s:%d", host, port)
	args := []string{
		rdpAddr,
		fmt.Sprintf("/size:%dx%d", width, height),
		"/cert:ignore",
		"/bpp:16",
		"/network:lan",
		"+clipboard",
	}
	if username != "" {
		args = append(args, "/u:"+username)
	}
	if password != "" {
		args = append(args, "/p:"+password)
	}

	b.freerdp = exec.CommandContext(bridgeCtx, "xfreerdp", args...) // #nosec G204 -- args are constructed from validated target fields
	b.freerdp.Env = append(b.freerdp.Environ(), "DISPLAY="+displayStr)
	if err := b.freerdp.Start(); err != nil {
		cancel()
		_ = b.xvfb.Process.Kill()
		return nil, fmt.Errorf("start xfreerdp: %w", err)
	}

	time.Sleep(2 * time.Second)

	b.x11vnc = exec.CommandContext(bridgeCtx, "x11vnc", // #nosec G204 -- args are constructed from validated internal values
		"-display", displayStr,
		"-rfbport", fmt.Sprintf("%d", vncPort),
		"-nopw",
		"-forever",
		"-shared",
		"-noxdamage",
		"-noxfixes",
		"-noxrecord",
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

	go func() {
		defer close(b.done)
		_ = b.freerdp.Wait()
		slog.Info("rdpvnc: xfreerdp exited", "display", display)
		cancel()
		killProc(b.x11vnc)
		killProc(b.xvfb)
	}()

	slog.Info("rdpvnc: bridge started", "display", display, "vnc_port", vncPort, "target", fmt.Sprintf("%s:%d", host, port))
	return b, nil
}

// VNCPort returns the local TCP port where x11vnc listens.
func (b *Bridge) VNCPort() int { return b.vncPort }

// Done returns a channel closed when the bridge processes have exited.
func (b *Bridge) Done() <-chan struct{} { return b.done }

// Stop kills all child processes and waits for cleanup.
func (b *Bridge) Stop() {
	b.cancel()
	killProc(b.freerdp)
	killProc(b.x11vnc)
	killProc(b.xvfb)

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
