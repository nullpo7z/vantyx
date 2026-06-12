package telnetproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// BridgeController is the runtime hook that the HTTP layer uses to
// drive the collaborative-session writer / viewer hand-off. It mirrors
// [sshproxy.BridgeController] so callers can promote and demote
// participants the same way for Telnet sessions.
type BridgeController interface {
	SetWriter(userID string)
	DetachUser(userID string)
}

// BridgeControlSink receives the controller exactly once when the
// bridge is ready. May be nil.
type BridgeControlSink interface {
	Register(controller BridgeController)
}

// RunBridgeDetachable runs a Telnet TCP bridge that keeps running when the client disconnects.
// When controlSink is non-nil it receives the live BridgeController so
// the HTTP layer can hand out the write token at runtime.
func RunBridgeDetachable(
	ctx context.Context,
	endMsg string,
	host string,
	port uint16,
	username, password string,
	output *session.RingBuffer,
	attachCh <-chan session.AttachReq,
	initialConn interface{},
	touch func(),
	tee io.Writer,
	stdinRecorder StdinRecorder,
	initialCols, initialRows int,
	externalResize <-chan sshproxy.TerminalSize,
	controlSink BridgeControlSink,
) error {
	dialer := net.Dialer{Timeout: 15 * time.Second}
	addr := net.JoinHostPort(host, portString(port))
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return WrapDialError(err)
	}

	var cleanupOnce sync.Once
	cleanup := func() { cleanupOnce.Do(func() { _ = conn.Close() }) }
	defer cleanup()

	bridgeDone := make(chan struct{})
	var bridgeDoneOnce sync.Once
	signalBridgeDone := func() {
		bridgeDoneOnce.Do(func() { close(bridgeDone) })
	}

	go func() {
		<-ctx.Done()
		cleanup()
		signalBridgeDone()
	}()

	_ = sendClientNegotiation(conn)
	b := newDetachableBridge(ctx, endMsg, conn, output, username, password, touch, tee, stdinRecorder, initialCols, initialRows, signalBridgeDone, cleanup)
	b.bridgeDone = bridgeDone
	if controlSink != nil {
		controlSink.Register(b)
	}

	go b.runStdinPump()
	go b.runOutputPump()
	if externalResize != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case sz, ok := <-externalResize:
					if !ok {
						return
					}
					if sz.Cols > 0 && sz.Rows > 0 {
						b.tryResize(sz.Cols, sz.Rows)
					}
				}
			}
		}()
	}

	return b.runAttachLoop(initialConn, attachCh)
}

// normalizeTelnetInput maps LF to CR for line endings (Telnet expects CR).
func normalizeTelnetInput(b []byte) []byte {
	if !bytesContains(b, '\n') {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' && (i == 0 || b[i-1] != '\r') {
			out = append(out, '\r')
		}
		out = append(out, b[i])
	}
	return out
}

func isReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func bytesContains(b []byte, c byte) bool {
	for _, x := range b {
		if x == c {
			return true
		}
	}
	return false
}

func isDataMessage(mt int) bool {
	return mt == websocket.TextMessage || mt == websocket.BinaryMessage
}

func portString(port uint16) string {
	if port == 0 {
		return "23"
	}
	var buf [5]byte
	i := len(buf) - 1
	p := port
	for p >= 10 {
		buf[i] = byte('0' + p%10)
		p /= 10
		i--
	}
	buf[i] = byte('0' + p)
	return string(buf[i:])
}
