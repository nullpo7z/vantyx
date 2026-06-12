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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"

	"github.com/nullpo7z/vantyx/internal/session"
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

func validateRDPString(s string, max int) error {
	if s == "" {
		return nil
	}
	if max > 0 && len(s) > max {
		return fmt.Errorf("rdpvnc: input too long")
	}
	// Reject control characters to avoid log/terminal injection and
	// surprising parsing behavior in downstream tooling.
	for _, r := range s {
		if r == 0 || r == '\r' || r == '\n' {
			return fmt.Errorf("rdpvnc: invalid control character")
		}
	}
	return nil
}

func validateRDPInputs(host string, port int, username, password string, width, height int) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("rdpvnc: host is required")
	}
	// Basic sanity limits; width/height are already bounded by the HTTP layer.
	if port <= 0 || port > 65535 {
		return fmt.Errorf("rdpvnc: invalid port")
	}
	if err := validateRDPString(host, 512); err != nil {
		return err
	}
	if err := validateRDPString(username, 256); err != nil {
		return err
	}
	// Password may be longer but still cap to avoid pathological argv sizes.
	if err := validateRDPString(password, 2048); err != nil {
		return err
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("rdpvnc: invalid geometry")
	}
	return nil
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
	if err := validateRDPInputs(host, port, username, password, width, height); err != nil {
		return nil, err
	}
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
		fmt.Sprintf("/size:%dx%d", width, height),
		// Dynamic resolution support (mstsc-like behavior). When the remote desktop
		// changes its resolution, allow FreeRDP to adjust without forcing a reconnect.
		"/dynamic-resolution",
		"/cert:ignore",
		"/network:lan",
		"+clipboard",
		"/gfx",
	}
	if username != "" || password != "" {
		credFile, credErr := writeFreerdpCredentialsFile(host, port, username, password)
		if credErr != nil {
			cancel()
			_ = b.xvfb.Process.Kill()
			return nil, credErr
		}
		defer os.Remove(credFile) // #nosec G304 -- temp credentials file removed after start.
		args = append([]string{"/from-file:" + credFile}, args...)
	} else {
		args = append([]string{rdpAddr}, args...)
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

// Display returns the Xvfb display number used by this bridge.
func (b *Bridge) Display() int { return b.display }

// Size returns the screen size used for Xvfb/xfreerdp.
func (b *Bridge) Size() (int, int) { return b.width, b.height }

// Done returns a channel closed when the bridge processes have exited.
func (b *Bridge) Done() <-chan struct{} { return b.done }

// Stop kills all child processes and waits for cleanup.
func (b *Bridge) Stop() {
	if b == nil {
		return
	}
	if b.cancel != nil {
		b.cancel()
	}
	killProc(b.freerdp)
	killProc(b.x11vnc)
	killProc(b.xvfb)
	if b.freerdpPty != nil {
		_ = b.freerdpPty.Close()
	}

	if b.done == nil {
		return
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

// Session represents one browser RDP session (xfreerdp→Xvfb→x11vnc) managed for reconnect/list/delete.
type Session struct {
	ID         string
	UserID     string
	TargetID   string
	TargetName string
	Width      int
	Height     int
	CreatedAt  time.Time
	lastSeen   time.Time
	Bridge     *Bridge
	AttachCh   chan session.AttachReq
}

// Manager tracks active RDP-to-VNC bridges for cleanup and session management.
type Manager struct {
	mu             sync.Mutex
	bridges        map[string]*Bridge  // key -> bridge (legacy key: "userID:targetID")
	sessionsByID   map[string]*Session // sessionID -> session
	sessionIDByKey map[string]string   // key -> sessionID
	now            func() time.Time
	idleWarnAfter  time.Duration // 0 = idle warnings disabled
}

// NewManager creates a new bridge manager.
func NewManager() *Manager {
	return &Manager{
		bridges:        make(map[string]*Bridge),
		sessionsByID:   make(map[string]*Session),
		sessionIDByKey: make(map[string]string),
		now:            time.Now,
	}
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
		if sid, ok := m.sessionIDByKey[key]; ok {
			if s, ok := m.sessionsByID[sid]; ok && s.Bridge == b {
				delete(m.sessionsByID, sid)
			}
			delete(m.sessionIDByKey, key)
		}
		m.mu.Unlock()
	}()
}

// RegisterSession registers a bridge as a managed session for the given key ("userID:targetID").
// If an existing session exists for the key, it is stopped and replaced.
func (m *Manager) RegisterSession(key string, sessionID string, userID, targetID, targetName string, width, height int, b *Bridge) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Replace any existing bridge/session for the key.
	if old, ok := m.bridges[key]; ok {
		old.Stop()
	}
	if oldID, ok := m.sessionIDByKey[key]; ok {
		delete(m.sessionsByID, oldID)
	}

	now := m.now()
	s := &Session{
		ID:         sessionID,
		UserID:     userID,
		TargetID:   targetID,
		TargetName: targetName,
		Width:      width,
		Height:     height,
		CreatedAt:  now,
		lastSeen:   now,
		Bridge:     b,
		AttachCh:   make(chan session.AttachReq, 16),
	}
	m.bridges[key] = b
	m.sessionsByID[sessionID] = s
	m.sessionIDByKey[key] = sessionID

	m.startActivityMonitor(sessionID, b)

	go func() {
		<-b.Done()
		m.mu.Lock()
		if m.bridges[key] == b {
			delete(m.bridges, key)
		}
		if cur, ok := m.sessionsByID[sessionID]; ok && cur.Bridge == b {
			delete(m.sessionsByID, sessionID)
		}
		if m.sessionIDByKey[key] == sessionID {
			delete(m.sessionIDByKey, key)
		}
		m.mu.Unlock()
	}()
	return s
}

// GetSessionByKey returns the managed session for the given key if present.
func (m *Manager) GetSessionByKey(key string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sid, ok := m.sessionIDByKey[key]
	if !ok {
		return nil, false
	}
	s, ok := m.sessionsByID[sid]
	return s, ok
}

// GetSession returns the managed session by session ID if present.
func (m *Manager) GetSession(sessionID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessionsByID[sessionID]
	return s, ok
}

// ActiveSessionsForUser returns managed sessions for a user (copy).
func (m *Manager) ActiveSessionsForUser(userID string) []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Session
	for _, s := range m.sessionsByID {
		if s.UserID == userID {
			out = append(out, *s)
		}
	}
	return out
}

// RemoveSession stops and removes the managed session by session ID.
func (m *Manager) RemoveSession(sessionID string) {
	var (
		key string
		b   *Bridge
		ok  bool
	)
	m.mu.Lock()
	if s, exists := m.sessionsByID[sessionID]; exists {
		b = s.Bridge
		ok = true
		key = s.UserID + ":" + s.TargetID
		delete(m.sessionsByID, sessionID)
		if m.sessionIDByKey[key] == sessionID {
			delete(m.sessionIDByKey, key)
		}
		if m.bridges[key] == b {
			delete(m.bridges, key)
		}
	}
	m.mu.Unlock()
	if ok && b != nil {
		b.Stop()
	}
}

// Remove stops and removes the bridge for the given key.
func (m *Manager) Remove(key string) {
	m.mu.Lock()
	b, ok := m.bridges[key]
	if ok {
		delete(m.bridges, key)
	}
	if sid, ok2 := m.sessionIDByKey[key]; ok2 {
		delete(m.sessionIDByKey, key)
		delete(m.sessionsByID, sid)
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

func writeFreerdpCredentialsFile(host string, port int, username, password string) (string, error) {
	if err := validateRDPString(host, 253); err != nil {
		return "", err
	}
	if err := validateRDPString(username, 512); err != nil {
		return "", err
	}
	if err := validateRDPString(password, 512); err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "freerdp-*.rdp")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := f.Chmod(0o600); err != nil { // #nosec G302 -- credentials file must be owner-only.
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := fmt.Fprintf(f, "full address:s:%s:%d\n", host, port); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if username != "" {
		if _, err := fmt.Fprintf(f, "username:s:%s\n", username); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return "", err
		}
	}
	if password != "" {
		if _, err := fmt.Fprintf(f, "password:s:%s\n", password); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return "", err
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
