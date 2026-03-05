package sshproxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// RunBridge connects to the target host via SSH with password auth, opens a PTY shell,
// and bridges WebSocket messages to SSH stdin and SSH stdout/stderr to WebSocket.
// The first message from the client is not read here; the caller must pass credentials
// and consume the first message before calling RunBridge.
// If touch is non-nil, it is called on each client message (e.g. for session keepalive).
// RunBridge blocks until ctx is done or the WebSocket or SSH session closes.
func RunBridge(ctx context.Context, conn *websocket.Conn, host string, port uint16, username, password string, touch func()) error {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		// #nosec G106 -- Phase 2: accept any host key; verify in Phase 3
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := net.JoinHostPort(host, portString(port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		return err
	}
	if err := session.Shell(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	// WebSocket -> stdin
	wg.Add(1)
	go func() {
		defer wg.Done()
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
			if mt == websocket.TextMessage || mt == websocket.BinaryMessage {
				if _, err := stdin.Write(msg); err != nil {
					return
				}
			}
		}
	}()

	// stdout -> WebSocket
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
			}
			if err != nil {
				return
			}
		}
	}()

	// stderr -> WebSocket
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
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
