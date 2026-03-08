package sshproxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/session"
)

// sessionFactory opens an SSH connection and returns stdin/stdout/stderr pipes, a window-change hook, and a cleanup function.
// Used so tests can inject a fake that fails at specific steps for coverage.
var sessionFactory = defaultSessionFactory

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
// If touch is non-nil, it is called on each client message (e.g. for session keepalive).
// If tee is non-nil, a copy of stdout and stderr is written to tee for session replay.
// If stdinRecorder is non-nil, it is called when data is written to the target stdin.
// RunBridge blocks until ctx is done or the WebSocket or SSH session closes.
func RunBridge(ctx context.Context, conn *websocket.Conn, host string, port uint16, username, password, privateKeyPEM, keyPassphrase string, touch func(), tee io.Writer, stdinRecorder StdinRecorder) error {
	auth, err := AuthMethods(password, privateKeyPEM, keyPassphrase)
	if err != nil {
		return err
	}
	config := &ssh.ClientConfig{
		User: username,
		Auth: auth,
		// #nosec G106 -- Phase 2: accept any host key; verify in Phase 3
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

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
// If touch is non-nil, it is called when data is read from localStdin.
// If tee is non-nil, target stdout/stderr is also written to tee.
// If stdinRecorder is non-nil, it is called when data is written to the target stdin.
func RunBridgeStream(ctx context.Context, localStdin io.Reader, localStdout io.Writer, host string, port uint16, username, password, privateKeyPEM, keyPassphrase string, ptyCols, ptyRows int, resizeChan <-chan TerminalSize, touch func(), tee io.Writer, stdinRecorder StdinRecorder) error {
	auth, err := AuthMethods(password, privateKeyPEM, keyPassphrase)
	if err != nil {
		return err
	}
	config := &ssh.ClientConfig{
		User: username,
		Auth: auth,
		// #nosec G106 -- Phase 2: accept any host key; verify in Phase 3
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
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
	Close() error
}

// wsWriterAdapter adapts *websocket.Conn to attachableWriter for RunBridgeDetachable.
type wsWriterAdapter struct{ *websocket.Conn }

func (w *wsWriterAdapter) WriteBinary(p []byte) error {
	return w.WriteMessage(websocket.BinaryMessage, p)
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
func (s *StreamAttach) Close() error {
	if s.CloseFn != nil {
		return s.CloseFn()
	}
	return nil
}

// RunBridgeDetachable runs an SSH bridge that keeps running when the client disconnects.
// Output is written to output (e.g. session RingBuffer) for replay. AttachCh receives new
// client connections to attach; initialConn is the first client (may be nil), either *websocket.Conn or *StreamAttach.
// Touch is called on client activity. If tee is non-nil, a copy of stdout/stderr is written to tee (e.g. asciinema file).
// If stdinRecorder is non-nil, it is called when data is written to the target stdin. The bridge exits when ctx is done or SSH session closes.
// initialCols and initialRows are the terminal size for the PTY (e.g. from client); 0 lets the factory use defaults.
func RunBridgeDetachable(ctx context.Context, host string, port uint16, username, password, privateKeyPEM, keyPassphrase string, output *session.RingBuffer, attachCh <-chan session.AttachReq, initialConn interface{}, touch func(), tee io.Writer, stdinRecorder StdinRecorder, initialCols, initialRows int) error {
	auth, err := AuthMethods(password, privateKeyPEM, keyPassphrase)
	if err != nil {
		return err
	}
	config := &ssh.ClientConfig{
		User: username,
		Auth: auth,
		// #nosec G106 -- Phase 2: accept any host key; verify in Phase 3
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(host, portString(port))
	stdin, stdout, stderr, windowChange, cleanup, err := sessionFactory(addr, config, initialCols, initialRows)
	if err != nil {
		return err
	}
	defer cleanup()

	stdinCh := make(chan []byte, 256)
	var clientMu sync.Mutex
	var client attachableWriter

	// SSH stdin: consume from channel (fed by attached client)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case b, ok := <-stdinCh:
				if !ok {
					return
				}
				if len(b) > 0 {
					if stdinRecorder != nil {
						stdinRecorder.RecordInput(b)
					}
					_, _ = stdin.Write(b)
				}
			}
		}
	}()

	// SSH stdout/stderr -> output + current client; bridgeDone closed when SSH session ends.
	// When either pipe gets EOF (e.g. user ran "exit" on server), close stdin once so the session ends cleanly and the other pipe gets EOF.
	var stdinCloseOnce sync.Once
	closeStdin := func() { stdinCloseOnce.Do(func() { _ = stdin.Close() }) }
	bridgeDone := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				_, _ = output.Write(buf[:n])
				if tee != nil {
					_, _ = tee.Write(buf[:n])
				}
				clientMu.Lock()
				c := client
				clientMu.Unlock()
				if c != nil {
					_ = c.WriteBinary(buf[:n])
				}
			}
			if err != nil {
				closeStdin()
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				_, _ = output.Write(buf[:n])
				if tee != nil {
					_, _ = tee.Write(buf[:n])
				}
				clientMu.Lock()
				c := client
				clientMu.Unlock()
				if c != nil {
					_ = c.WriteBinary(buf[:n])
				}
			}
			if err != nil {
				closeStdin()
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(bridgeDone)
	}()

	type resizeMsg struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}

	attachWebSocket := func(conn *websocket.Conn) {
		clientMu.Lock()
		if client != nil {
			_ = client.Close()
		}
		w := &wsWriterAdapter{conn}
		client = w
		clientMu.Unlock()
		replay := output.Bytes()
		if len(replay) > 0 {
			_ = conn.WriteMessage(websocket.BinaryMessage, replay)
		}
		go func(adapter *wsWriterAdapter) {
			defer func() {
				clientMu.Lock()
				if client == adapter {
					client = nil
				}
				clientMu.Unlock()
			}()
			for {
				mt, msg, err := conn.ReadMessage()
				if err != nil {
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
				if mt == websocket.TextMessage && windowChange != nil && len(msg) > 0 && msg[0] == '{' && strings.Contains(string(msg), `"type":"resize"`) {
					var rm resizeMsg
					if jsonErr := json.Unmarshal(msg, &rm); jsonErr == nil && rm.Type == "resize" && rm.Cols > 0 && rm.Rows > 0 {
						_ = windowChange(rm.Cols, rm.Rows)
						continue
					}
				}
				if isDataMessage(mt) {
					select {
					case stdinCh <- msg:
					case <-ctx.Done():
						return
					}
				}
			}
		}(w)
	}

	attachStream := func(sa *StreamAttach) {
		clientMu.Lock()
		if client != nil {
			_ = client.Close()
		}
		client = sa
		clientMu.Unlock()
		replay := output.Bytes()
		if len(replay) > 0 {
			_ = sa.WriteBinary(replay)
		}
		sa.StartRead(stdinCh, func() {
			clientMu.Lock()
			if c, ok := client.(*StreamAttach); ok && c == sa {
				client = nil
			}
			clientMu.Unlock()
		})
	}

	doAttach := func(conn interface{}) {
		switch c := conn.(type) {
		case *websocket.Conn:
			attachWebSocket(c)
		case *StreamAttach:
			attachStream(c)
		}
	}

	if initialConn != nil {
		doAttach(initialConn)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-bridgeDone:
			return nil
		case req := <-attachCh:
			doAttach(req.Conn)
		}
	}
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
	Name                 string `json:"name"`        // セッション名（識別用・任意）
	Description          string `json:"description"` // 説明（任意）
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
