package telnetproxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
)

type detachableBridge struct {
	ctx              context.Context
	conn             net.Conn
	output           *session.RingBuffer
	termSize         *terminalSize
	loginAuto        *LoginAutomater
	touch            func()
	tee              io.Writer
	stdinRecorder    StdinRecorder
	stdinCh          chan []byte
	clientMu         sync.Mutex
	client           attachableWriter
	bridgeDone       chan struct{}
	signalBridgeDone func()
	cleanup          func()
	iacProc          iacStream
}

func newDetachableBridge(
	ctx context.Context,
	conn net.Conn,
	output *session.RingBuffer,
	username, password string,
	touch func(),
	tee io.Writer,
	stdinRecorder StdinRecorder,
	initialCols, initialRows int,
	signalBridgeDone func(),
	cleanup func(),
) *detachableBridge {
	termSize := newTerminalSize(initialCols, initialRows)
	loginAuto := NewLoginAutomater(username, password)
	if loginAuto != nil && telnetWakeOnConnect() {
		_, _ = conn.Write([]byte{'\r'})
	}
	return &detachableBridge{
		ctx:              ctx,
		conn:             conn,
		output:           output,
		termSize:         termSize,
		loginAuto:        loginAuto,
		touch:            touch,
		tee:              tee,
		stdinRecorder:    stdinRecorder,
		stdinCh:          make(chan []byte, 256),
		bridgeDone:       make(chan struct{}),
		signalBridgeDone: signalBridgeDone,
		cleanup:          cleanup,
	}
}

func (b *detachableBridge) replyTo(iacReply []byte) error {
	if _, err := b.conn.Write(iacReply); err != nil {
		return err
	}
	if len(iacReply) == 3 && iacReply[0] == iac && iacReply[1] == will && iacReply[2] == optNAWS {
		cols, rows := b.termSize.get()
		_, err := b.conn.Write(encodeNAWS(cols, rows))
		return err
	}
	return nil
}

func (b *detachableBridge) tryResize(cols, rows int) {
	if b.termSize.set(cols, rows) {
		c, r := b.termSize.get()
		_, _ = b.conn.Write(encodeNAWS(c, r))
	}
}

func (b *detachableBridge) runStdinPump() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case data, ok := <-b.stdinCh:
			if !ok {
				return
			}
			if len(data) > 0 {
				if b.stdinRecorder != nil {
					b.stdinRecorder.RecordInput(data)
				}
				_, _ = b.conn.Write(normalizeTelnetInput(data))
			}
		}
	}
}

func (b *detachableBridge) runOutputPump() {
	defer b.signalBridgeDone()
	buf := make([]byte, 4096)
	for {
		if b.ctx.Err() != nil {
			return
		}
		_ = b.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := b.conn.Read(buf)
		if n > 0 {
			filtered := b.iacProc.Filter(buf[:n], b.replyTo)
			if len(filtered) > 0 {
				if b.loginAuto != nil {
					loginAuto := b.loginAuto
					loginAuto.OnOutput(filtered, func(reply []byte) error {
						_, wErr := b.conn.Write(reply)
						return wErr
					})
				}
				_, _ = b.output.Write(filtered)
				if b.tee != nil {
					_, _ = b.tee.Write(filtered)
				}
				b.clientMu.Lock()
				c := b.client
				b.clientMu.Unlock()
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
}

func (b *detachableBridge) handleResizeMessage(msg []byte) bool {
	if len(msg) == 0 || msg[0] != '{' || !strings.Contains(string(msg), `"type":"resize"`) {
		return false
	}
	var rm resizeMsg
	if json.Unmarshal(msg, &rm) != nil || rm.Type != "resize" || rm.Cols <= 0 || rm.Rows <= 0 {
		return false
	}
	b.tryResize(rm.Cols, rm.Rows)
	return true
}

func (b *detachableBridge) attachWebSocket(wsConn *websocket.Conn) {
	b.clientMu.Lock()
	if b.client != nil {
		_ = b.client.Close()
	}
	w := &wsWriterAdapter{wsConn}
	b.client = w
	b.clientMu.Unlock()
	replay := b.output.Bytes()
	if len(replay) > 0 {
		_ = wsConn.WriteMessage(websocket.BinaryMessage, replay)
	}
	cols, rows := b.termSize.get()
	_, _ = b.conn.Write(encodeNAWS(cols, rows))
	go b.readWebSocket(w, wsConn)
}

func (b *detachableBridge) readWebSocket(adapter *wsWriterAdapter, wsConn *websocket.Conn) {
	defer func() {
		b.clientMu.Lock()
		if b.client == adapter {
			b.client = nil
		}
		b.clientMu.Unlock()
	}()
	for {
		mt, msg, err := wsConn.ReadMessage()
		if err != nil {
			return
		}
		select {
		case <-b.ctx.Done():
			return
		default:
		}
		if b.touch != nil {
			b.touch()
		}
		if mt == websocket.TextMessage && b.handleResizeMessage(msg) {
			continue
		}
		if isDataMessage(mt) {
			select {
			case b.stdinCh <- msg:
			case <-b.ctx.Done():
				return
			}
		}
	}
}

func (b *detachableBridge) attachStream(sa *StreamAttach) {
	b.clientMu.Lock()
	if b.client != nil {
		_ = b.client.Close()
	}
	b.client = sa
	b.clientMu.Unlock()
	replay := b.output.Bytes()
	if len(replay) > 0 {
		_ = sa.WriteBinary(replay)
	}
	sa.StartRead(b.stdinCh, func() {
		b.clientMu.Lock()
		if c, ok := b.client.(*StreamAttach); ok && c == sa {
			b.client = nil
		}
		b.clientMu.Unlock()
	})
}

func (b *detachableBridge) doAttach(conn interface{}) {
	switch c := conn.(type) {
	case *websocket.Conn:
		b.attachWebSocket(c)
	case *StreamAttach:
		b.attachStream(c)
	}
}

func (b *detachableBridge) runAttachLoop(initialConn interface{}, attachCh <-chan session.AttachReq) error {
	if initialConn != nil {
		b.doAttach(initialConn)
	}
	for {
		select {
		case <-b.ctx.Done():
			return nil
		case <-b.bridgeDone:
			return nil
		case req := <-attachCh:
			b.doAttach(req.Conn)
		}
	}
}
