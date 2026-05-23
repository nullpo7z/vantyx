package telnetproxy

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
)

// RunBridgeDetachable runs a Telnet TCP bridge that keeps running when the client disconnects.
func RunBridgeDetachable(
	ctx context.Context,
	host string,
	port uint16,
	output *session.RingBuffer,
	attachCh <-chan session.AttachReq,
	initialConn interface{},
	touch func(),
	tee io.Writer,
	stdinRecorder StdinRecorder,
) error {
	addr := net.JoinHostPort(host, portString(port))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	var cleanupOnce sync.Once
	cleanup := func() { cleanupOnce.Do(func() { _ = conn.Close() }) }
	defer cleanup()
	go func() {
		<-ctx.Done()
		cleanup()
	}()

	stdinCh := make(chan []byte, 256)
	var clientMu sync.Mutex
	var client attachableWriter

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
					_, _ = conn.Write(b)
				}
			}
		}
	}()

	bridgeDone := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				filtered := filterIAC(buf[:n], func(reply []byte) error {
					_, wErr := conn.Write(reply)
					return wErr
				})
				if len(filtered) > 0 {
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
				close(bridgeDone)
				return
			}
		}
	}()

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
				// Telnet: ignore resize JSON (no NAWS in MVP).
				if mt == websocket.TextMessage && len(msg) > 0 && msg[0] == '{' && strings.Contains(string(msg), `"type":"resize"`) {
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
