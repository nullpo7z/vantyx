package sshproxy

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/proxyerrors"
	"github.com/nullpo7z/vantyx/internal/session"
)

// maxClientMessageBytes bounds WebSocket frames from the browser client
// to mitigate memory-based DoS in gorilla/websocket's ReadMessage path (CWE-770).
// Terminal input is typically small (keystrokes); large payloads are unexpected.
const maxClientMessageBytes int64 = 1 << 20 // 1 MiB

// sessionFactory opens an SSH connection and returns stdin/stdout/stderr pipes, a window-change hook, and a cleanup function.
// Used so tests can inject a fake that fails at specific steps for coverage.
var sessionFactory = defaultSessionFactory

// ClientCiphers returns cipher names for SSH client connections, restricted to algorithms
// supported by older or restricted servers (e.g. that do not support aes256-gcm@openssh.com).
func ClientCiphers() []string {
	return []string{"aes256-ctr", "aes256-cbc", "aes128-ctr", "aes128-cbc", "3des-cbc"}
}

// HostKeyAlgorithms note: every SSH client built here on top of
// ssh.ClientConfig deliberately leaves HostKeyAlgorithms unset so it
// inherits the package-default preference order. The default order
// puts RSA before ED25519, which sounds backwards but matches what
// stock OpenSSH offers when both keys are advertised. Mixing the
// default here with an explicit override anywhere else (e.g. the
// host-key probe endpoint in internal/httpapi/targets_hostkey.go)
// produces the bug reported in 2026-05: probe and bridge end up
// negotiating different host keys, so the user adopts a fingerprint
// the bridge will never see and the next connect always reports a
// mismatch. Keep both code paths algorithm-agnostic; if a stronger
// policy is wanted, change every site at once.

// Test hooks for defaultSessionFactory error paths (set from bridge_test.go).
var (
	testHookNewSession func(*ssh.Client) (*ssh.Session, error)
	testHookStdinPipe  func(*ssh.Session) (io.WriteCloser, error)
	testHookStdoutPipe func(*ssh.Session) (io.Reader, error)
	testHookStderrPipe func(*ssh.Session) (io.Reader, error)
	testHookRequestPty func(*ssh.Session) error
	testHookShell      func(*ssh.Session) error
)

func defaultSessionFactory(addr string, config *ssh.ClientConfig, cols, rows int) (stdin io.WriteCloser, stdout, stderr io.Reader, windowChange func(cols, rows int) error, cleanup func(), err error) {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var sess *ssh.Session
	if testHookNewSession != nil {
		sess, err = testHookNewSession(client)
	} else {
		sess, err = client.NewSession()
	}
	if err != nil {
		_ = client.Close()
		return nil, nil, nil, nil, nil, err
	}
	if testHookStdinPipe != nil {
		stdin, err = testHookStdinPipe(sess)
	} else {
		stdin, err = sess.StdinPipe()
	}
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, err
	}
	if testHookStdoutPipe != nil {
		stdout, err = testHookStdoutPipe(sess)
	} else {
		stdout, err = sess.StdoutPipe()
	}
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, err
	}
	if testHookStderrPipe != nil {
		stderr, err = testHookStderrPipe(sess)
	} else {
		stderr, err = sess.StderrPipe()
	}
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, err
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if testHookRequestPty != nil {
		err = testHookRequestPty(sess)
	} else {
		// RequestPty takes (term, height=rows, width=cols).
		err = sess.RequestPty("xterm", rows, cols, modes)
	}
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, err
	}
	if testHookShell != nil {
		err = testHookShell(sess)
	} else {
		err = sess.Shell()
	}
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, err
	}
	windowChange = func(cols, rows int) error {
		if cols <= 0 || rows <= 0 {
			return nil
		}
		return sess.WindowChange(rows, cols)
	}
	cleanup = func() {
		_ = sess.Close()
		_ = client.Close()
	}
	return stdin, stdout, stderr, windowChange, cleanup, nil
}

// TerminalSize carries terminal dimensions for PTY resize (e.g. from SSH window-change).
type TerminalSize struct{ Cols, Rows int }

// StdinRecorder is called when data is sent to the target's stdin (e.g. for asciinema input events).
// May be nil.
type StdinRecorder interface {
	RecordInput(p []byte)
}

// StdinRecorderFunc adapts a function to StdinRecorder.
type StdinRecorderFunc func(p []byte)

func (f StdinRecorderFunc) RecordInput(p []byte) {
	f(p)
}

// ResizeRecorder is called when the target PTY is resized (e.g. from a
// client's window-change), so a recording can capture the resize as an
// asciicast "r" event. May be nil.
type ResizeRecorder interface {
	RecordResize(cols, rows int)
}

// ResizeRecorderFunc adapts a function to ResizeRecorder.
type ResizeRecorderFunc func(cols, rows int)

func (f ResizeRecorderFunc) RecordResize(cols, rows int) {
	f(cols, rows)
}

// AuthMethods builds SSH auth methods from password and/or PEM private key (with optional passphrase).
// Key is tried first when present. Used by bridge and by internal/sftp.
func AuthMethods(password, privateKeyPEM, keyPassphrase string) ([]ssh.AuthMethod, error) {
	var out []ssh.AuthMethod
	if privateKeyPEM != "" {
		var signer ssh.Signer
		var err error
		if keyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(privateKeyPEM), []byte(keyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(privateKeyPEM))
		}
		if err != nil {
			var passMissing *ssh.PassphraseMissingError
			if errors.As(err, &passMissing) {
				return nil, errors.New("この秘密鍵はパスフレーズで保護されています。ターゲットにパスフレーズを保存するか、接続時に入力してください")
			}
			if errors.Is(err, x509.IncorrectPasswordError) {
				return nil, errors.New("秘密鍵のパスフレーズが正しくありません。保存したパスフレーズを確認するか、接続時に再入力してください")
			}
			return nil, err
		}
		out = append(out, ssh.PublicKeys(signer))
	}
	if password != "" {
		out = append(out, ssh.Password(password))
	}
	return out, nil
}

// RunBridge connects to the target host via SSH (password and/or public key auth), opens a PTY shell,
// and bridges WebSocket messages to SSH stdin and SSH stdout/stderr to WebSocket.
// The first message from the client is not read here; the caller must pass credentials
// and consume the first message before calling RunBridge.
// If touch is non-nil, it is called on each client message and when remote stdout/stderr is received.
// If tee is non-nil, a copy of stdout and stderr is written to tee for session replay.
// If stdinRecorder is non-nil, it is called when data is written to the target stdin.
// Optional BridgeOption values configure host-key verification (see hostkey.go).
// RunBridge blocks until ctx is done or the WebSocket or SSH session closes.
func RunBridge(ctx context.Context, conn *websocket.Conn, host string, port uint16, username, password, privateKeyPEM, keyPassphrase string, touch func(), tee io.Writer, stdinRecorder StdinRecorder, opts ...BridgeOption) error {
	auth, err := AuthMethods(password, privateKeyPEM, keyPassphrase)
	if err != nil {
		return err
	}
	o := buildOptions(opts)
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(o),
		Timeout:         15 * time.Second,
	}
	config.Ciphers = ClientCiphers()

	addr := net.JoinHostPort(host, portString(port))
	stdin, stdout, stderr, windowChange, cleanup, err := sessionFactory(addr, config, 0, 0)
	if err != nil {
		return err
	}
	defer cleanup()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// WebSocket -> stdin
	wg.Add(1)
	conn.SetReadLimit(maxClientMessageBytes)
	go func() {
		defer wg.Done()
		type resizeMsg struct {
			Type string `json:"type"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				// Client closed or read error: close SSH session so stdout/stderr goroutines get EOF and RunBridge can return.
				cleanup()
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
			if touch != nil {
				touch()
			}
			// Control message: resize (do not write to stdin)
			if mt == websocket.TextMessage && windowChange != nil && len(msg) > 0 && msg[0] == '{' && strings.Contains(string(msg), `"type":"resize"`) {
				var rm resizeMsg
				if jsonErr := json.Unmarshal(msg, &rm); jsonErr == nil && rm.Type == "resize" && rm.Cols > 0 && rm.Rows > 0 {
					_ = windowChange(rm.Cols, rm.Rows)
					continue
				}
			}
			if isDataMessage(mt) {
				if stdinRecorder != nil {
					stdinRecorder.RecordInput(msg)
				}
				if _, err := stdin.Write(msg); err != nil {
					return
				}
			}
		}
	}()

	// stdout -> WebSocket (and tee for replay)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if touch != nil {
					touch()
				}
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
				if tee != nil {
					_, _ = tee.Write(buf[:n])
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// stderr -> WebSocket (and tee for replay)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				if touch != nil {
					touch()
				}
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
				if tee != nil {
					_, _ = tee.Write(buf[:n])
				}
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nil
	case <-done:
		return nil
	}
}

// RunBridgeStream connects to the target host via SSH and bridges localStdin <-> target stdin
// and target stdout/stderr -> localStdout. Used for CLI (non-WebSocket) access.
// ptyCols and ptyRows are the terminal size (from client pty-req); if <= 0, defaults (120x40) are used.
// If resizeChan is non-nil, terminal size updates (e.g. from SSH window-change) are forwarded to the target PTY.
// If touch is non-nil, it is called when data is read from localStdin or target stdout/stderr.
// If tee is non-nil, target stdout/stderr is also written to tee.
// If stdinRecorder is non-nil, it is called when data is written to the target stdin.
func RunBridgeStream(ctx context.Context, localStdin io.Reader, localStdout io.Writer, host string, port uint16, username, password, privateKeyPEM, keyPassphrase string, ptyCols, ptyRows int, resizeChan <-chan TerminalSize, touch func(), tee io.Writer, stdinRecorder StdinRecorder, opts ...BridgeOption) error {
	auth, err := AuthMethods(password, privateKeyPEM, keyPassphrase)
	if err != nil {
		return err
	}
	o := buildOptions(opts)
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(o),
		Timeout:         15 * time.Second,
	}
	config.Ciphers = ClientCiphers()
	addr := net.JoinHostPort(host, portString(port))
	stdin, stdout, stderr, windowChange, cleanup, err := sessionFactory(addr, config, ptyCols, ptyRows)
	if err != nil {
		return err
	}
	defer cleanup()

	if resizeChan != nil && windowChange != nil {
		go func() {
			for sz := range resizeChan {
				if sz.Cols > 0 && sz.Rows > 0 {
					_ = windowChange(sz.Cols, sz.Rows)
				}
			}
		}()
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	// localStdin -> target stdin
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := localStdin.Read(buf)
			if n > 0 {
				if touch != nil {
					touch()
				}
				if stdinRecorder != nil {
					stdinRecorder.RecordInput(buf[:n])
				}
				if _, err := stdin.Write(buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	// target stdout -> localStdout (and tee)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if touch != nil {
					touch()
				}
				if _, wErr := localStdout.Write(buf[:n]); wErr != nil {
					return
				}
				if tee != nil {
					_, _ = tee.Write(buf[:n])
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// target stderr -> localStdout (and tee)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				if touch != nil {
					touch()
				}
				if _, wErr := localStdout.Write(buf[:n]); wErr != nil {
					return
				}
				if tee != nil {
					_, _ = tee.Write(buf[:n])
				}
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nil
	case <-done:
		return nil
	}
}

// attachableWriter is the internal interface for writing to an attached client (WebSocket or CLI stream).
type attachableWriter interface {
	WriteBinary([]byte) error
	WriteText([]byte) error
	Close() error
}

// wsWriterAdapter adapts *websocket.Conn to attachableWriter for RunBridgeDetachable.
type wsWriterAdapter struct{ *websocket.Conn }

func (w *wsWriterAdapter) WriteBinary(p []byte) error {
	return w.WriteMessage(websocket.BinaryMessage, p)
}

func (w *wsWriterAdapter) WriteText(p []byte) error {
	return w.WriteMessage(websocket.TextMessage, p)
}

// StreamAttach is used to attach an SSH channel (CLI) to an existing session. Create from sshd and send via AttachCh.
// StartRead is called with the stdin channel and an onClose callback; the implementation should read from the CLI and
// send to the channel, and call onClose when the read loop exits (e.g. channel closed).
type StreamAttach struct {
	Write     func([]byte) error
	StartRead func(stdinCh chan<- []byte, onClose func())
	CloseFn   func() error
}

func (s *StreamAttach) WriteBinary(p []byte) error { return s.Write(p) }
func (s *StreamAttach) WriteText(p []byte) error   { return s.Write(p) }
func (s *StreamAttach) Close() error {
	if s.CloseFn != nil {
		return s.CloseFn()
	}
	return nil
}

// BridgeController is the subset of the running detachable bridge
// that the HTTP layer needs to drive the writer / viewer hand-off. It
// is published via [BridgeControlSink] so callers can promote a user
// to writer or demote everyone without holding a direct reference to
// the bridge struct.
type BridgeController interface {
	// SetWriter promotes attached clients owned by userID to writer
	// and demotes the rest. An empty userID demotes every client.
	SetWriter(userID string)
	// DetachUser closes every attached client owned by userID.
	DetachUser(userID string)
}

// BridgeControlSink receives the controller exactly once when the
// bridge is fully initialised. May be nil for callers that do not
// need run-time writer changes (CLI, tests).
type BridgeControlSink interface {
	Register(controller BridgeController)
}

// RunBridgeDetachable runs an SSH bridge that keeps running when the client disconnects.
// Output is written to output (e.g. session RingBuffer) for replay. AttachCh receives new
// client connections to attach; initialConn is the first client (may be nil), either *websocket.Conn or *StreamAttach.
// Touch is called on client or remote I/O. If tee is non-nil, a copy of stdout/stderr is written to tee (e.g. asciinema file).
// If stdinRecorder is non-nil, it is called when data is written to the target stdin. The bridge exits when ctx is done or SSH session closes.
// initialCols and initialRows are the terminal size for the PTY (e.g. from client); 0 lets the factory use defaults.
func RunBridgeDetachable(ctx context.Context, endMsg string, host string, port uint16, username, password, privateKeyPEM, keyPassphrase string, output *session.RingBuffer, attachCh <-chan session.AttachReq, initialConn interface{}, touch func(), tee io.Writer, stdinRecorder StdinRecorder, initialCols, initialRows int, externalResize <-chan TerminalSize, opts ...BridgeOption) error {
	auth, err := AuthMethods(password, privateKeyPEM, keyPassphrase)
	if err != nil {
		return err
	}
	o := buildOptions(opts)
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(o),
		Timeout:         15 * time.Second,
	}
	config.Ciphers = ClientCiphers()
	addr := net.JoinHostPort(host, portString(port))
	stdin, stdout, stderr, windowChange, cleanup, err := sessionFactory(addr, config, initialCols, initialRows)
	if err != nil {
		return proxyerrors.WrapTCPDialError("SSH", err)
	}
	// Ensure cleanup happens on ctx cancel too, otherwise stdout/stderr reads can block forever
	// and the detachable session never stops (leaving "active sessions" behind).
	var cleanupOnce sync.Once
	doCleanup := func() { cleanupOnce.Do(func() { cleanup() }) }
	defer doCleanup()
	go func() {
		<-ctx.Done()
		doCleanup()
	}()

	bridge := newSSHDetachableBridge(ctx, endMsg, stdin, output, windowChange, touch, tee, stdinRecorder, attachCh, externalResize, o.resizeRecorder)
	if o.controlSink != nil {
		o.controlSink.Register(bridge)
	}
	bridge.startPumps(stdout, stderr)
	return bridge.run(initialConn)
}

// isDataMessage reports whether the WebSocket message type should be written to SSH stdin.
func isDataMessage(mt int) bool {
	return mt == websocket.TextMessage || mt == websocket.BinaryMessage
}

func portString(port uint16) string {
	// Standard library net.JoinHostPort expects a string port; format manually to avoid strconv in hot path.
	if port == 0 {
		return "22"
	}
	var buf [5]byte
	i := len(buf) - 1
	p := uint16(port)
	for p >= 10 {
		buf[i] = byte('0' + p%10)
		p /= 10
		i--
	}
	buf[i] = byte('0' + p)
	return string(buf[i:])
}

// Credentials is the JSON shape of the first WebSocket message for SSH auth.
// When using stored credentials (server-side), PrivateKey and PrivateKeyPassphrase may be set from the target.
type Credentials struct {
	Username             string `json:"username"`
	Password             string `json:"password"`
	PrivateKey           string `json:"-"`           // PEM; set server-side when using stored key
	PrivateKeyPassphrase string `json:"-"`           // passphrase for encrypted PEM
	Name                 string `json:"name"`        // optional human-readable session name (for identification).
	Description          string `json:"description"` // optional free-form description.
}

// ReadCredentials reads the first WebSocket text message and parses it as Credentials.
// It sets a read deadline and returns an error if the message is missing or invalid.
func ReadCredentials(conn *websocket.Conn, timeout time.Duration) (Credentials, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	mt, msg, err := conn.ReadMessage()
	if err != nil {
		return Credentials{}, err
	}
	if mt != websocket.TextMessage {
		return Credentials{}, io.EOF
	}
	var c Credentials
	if err := json.Unmarshal(msg, &c); err != nil {
		return Credentials{}, err
	}
	if c.Username == "" {
		return Credentials{}, io.EOF
	}
	return c, nil
}
