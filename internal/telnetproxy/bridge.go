package telnetproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
)

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// RunBridgeDetachable runs a Telnet TCP bridge that keeps running when the client disconnects.
func RunBridgeDetachable(
	ctx context.Context,
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

	termSize := newTerminalSize(initialCols, initialRows)
	replyTo := func(b []byte) error {
		if _, err := conn.Write(b); err != nil {
			return err
		}
		if len(b) == 3 && b[0] == iac && b[1] == will && b[2] == optNAWS {
			cols, rows := termSize.get()
			_, err := conn.Write(encodeNAWS(cols, rows))
			return err
		}
		return nil
	}
	_ = sendClientNegotiation(conn)
	loginAuto := NewLoginAutomater(username, password)
	if loginAuto != nil && telnetWakeOnConnect() {
		_, _ = conn.Write([]byte{'\r'})
	}

	stdinCh := make(chan []byte, 256)
	var clientMu sync.Mutex
	var client attachableWriter

	tryResize := func(cols, rows int) {
		if termSize.set(cols, rows) {
			c, r := termSize.get()
			_, _ = conn.Write(encodeNAWS(c, r))
		}
	}

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
					_, _ = conn.Write(normalizeTelnetInput(b))
				}
			}
		}
	}()

	var iacProc iacStream
	go func() {
		defer signalBridgeDone()
		buf := make([]byte, 4096)
		for {
			if ctx.Err() != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, err := conn.Read(buf)
			if n > 0 {
				filtered := iacProc.Filter(buf[:n], replyTo)
				if len(filtered) > 0 {
					if loginAuto != nil {
						loginAuto.OnOutput(filtered, func(b []byte) error {
							_, wErr := conn.Write(b)
							return wErr
						})
					}
					_, _ = output.Write(filtered)
					if tee != nil {
						_, _ = tee.Write(filtered)
					}
					clientMu.Lock()
					c := client
					clientMu.Unlock()
					if c != nil {
						_ = c.WriteBinary(filtered)
					}
				}
			}
			if err != nil {
				if isReadTimeout(err) {
					continue
				}
				return
			}
		}
	}()

	handleResizeMessage := func(msg []byte) bool {
		if len(msg) == 0 || msg[0] != '{' || !strings.Contains(string(msg), `"type":"resize"`) {
			return false
		}
		var rm resizeMsg
		if json.Unmarshal(msg, &rm) != nil || rm.Type != "resize" || rm.Cols <= 0 || rm.Rows <= 0 {
			return false
		}
		tryResize(rm.Cols, rm.Rows)
		return true
	}

	attachWebSocket := func(wsConn *websocket.Conn) {
		clientMu.Lock()
		if client != nil {
			_ = client.Close()
		}
		w := &wsWriterAdapter{wsConn}
		client = w
		clientMu.Unlock()
		replay := output.Bytes()
		if len(replay) > 0 {
			_ = wsConn.WriteMessage(websocket.BinaryMessage, replay)
		}
		cols, rows := termSize.get()
		_, _ = conn.Write(encodeNAWS(cols, rows))
		go func(adapter *wsWriterAdapter) {
			defer func() {
				clientMu.Lock()
				if client == adapter {
					client = nil
				}
				clientMu.Unlock()
			}()
			for {
				mt, msg, err := wsConn.ReadMessage()
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
				if mt == websocket.TextMessage && handleResizeMessage(msg) {
					continue
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
