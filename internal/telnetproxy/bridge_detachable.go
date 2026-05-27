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

// clientEntry tracks a single attached telnet client, mirroring the
// SSH bridge's structure. canWrite controls stdin forwarding so a
// viewer's input never reaches the upstream telnet socket.
type clientEntry struct {
	w        attachableWriter
	canWrite bool
	userID   string
}

type detachableBridge struct {
	ctx              context.Context
	endMsg           string
	conn             net.Conn
	output           *session.RingBuffer
	termSize         *terminalSize
	loginAuto        *LoginAutomater
	touch            func()
	tee              io.Writer
	stdinRecorder    StdinRecorder
	stdinCh          chan []byte
	clientMu         sync.Mutex
	clients          map[*clientEntry]struct{}
	bridgeDone       chan struct{}
	signalBridgeDone func()
	cleanup          func()
	iacProc          iacStream
}

func newDetachableBridge(
	ctx context.Context,
	endMsg string,
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
		endMsg:           endMsg,
		conn:             conn,
		output:           output,
		termSize:         termSize,
		loginAuto:        loginAuto,
		touch:            touch,
		tee:              tee,
		stdinRecorder:    stdinRecorder,
		stdinCh:          make(chan []byte, 256),
		clients:          make(map[*clientEntry]struct{}),
		bridgeDone:       make(chan struct{}),
		signalBridgeDone: signalBridgeDone,
		cleanup:          cleanup,
	}
}

func (b *detachableBridge) broadcastText(p []byte) {
	if len(p) == 0 {
		return
	}
	b.clientMu.Lock()
	dead := make([]*clientEntry, 0)
	for c := range b.clients {
		if err := c.w.WriteText(p); err != nil {
			dead = append(dead, c)
		}
	}
	for _, c := range dead {
		delete(b.clients, c)
		_ = c.w.Close()
	}
	b.clientMu.Unlock()
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
				if b.touch != nil {
					b.touch()
				}
				if b.stdinRecorder != nil {
					b.stdinRecorder.RecordInput(data)
				}
				_, _ = b.conn.Write(normalizeTelnetInput(data))
			}
		}
	}
}

// broadcast sends p to every attached client, dropping any that
// produce a write error.
func (b *detachableBridge) broadcast(p []byte) {
	if len(p) == 0 {
		return
	}
	b.clientMu.Lock()
	dead := make([]*clientEntry, 0)
	for c := range b.clients {
		if err := c.w.WriteBinary(p); err != nil {
			dead = append(dead, c)
		}
	}
	for _, c := range dead {
		delete(b.clients, c)
		_ = c.w.Close()
	}
	b.clientMu.Unlock()
}

// SetWriter promotes attached clients owned by userID to writer and
// downgrades all others. An empty userID demotes every client.
func (b *detachableBridge) SetWriter(userID string) {
	b.clientMu.Lock()
	defer b.clientMu.Unlock()
	for c := range b.clients {
		c.canWrite = userID != "" && c.userID == userID
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
				if b.touch != nil {
					b.touch()
				}
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
				b.broadcast(filtered)
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

func (b *detachableBridge) attachWebSocket(wsConn *websocket.Conn, mode session.AttachMode, userID string) {
	w := &wsWriterAdapter{wsConn}
	entry := &clientEntry{w: w, canWrite: mode != session.AttachModeViewer, userID: userID}
	b.clientMu.Lock()
	b.clients[entry] = struct{}{}
	b.clientMu.Unlock()
	replay := b.output.Bytes()
	if len(replay) > 0 {
		if err := wsConn.WriteMessage(websocket.BinaryMessage, replay); err != nil {
			b.detachEntry(entry)
			return
		}
	}
	cols, rows := b.termSize.get()
	_, _ = b.conn.Write(encodeNAWS(cols, rows))
	go b.readWebSocket(entry, wsConn)
}

func (b *detachableBridge) detachEntry(entry *clientEntry) {
	b.clientMu.Lock()
	if _, ok := b.clients[entry]; ok {
		delete(b.clients, entry)
	}
	b.clientMu.Unlock()
	_ = entry.w.Close()
}

func (b *detachableBridge) readWebSocket(entry *clientEntry, wsConn *websocket.Conn) {
	defer b.detachEntry(entry)
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
		if !isDataMessage(mt) {
			continue
		}
		b.clientMu.Lock()
		canWrite := entry.canWrite
		b.clientMu.Unlock()
		if !canWrite {
			continue
		}
		select {
		case b.stdinCh <- msg:
		case <-b.ctx.Done():
			return
		}
	}
}

func (b *detachableBridge) attachStream(sa *StreamAttach, mode session.AttachMode, userID string) {
	entry := &clientEntry{w: sa, canWrite: mode != session.AttachModeViewer, userID: userID}
	b.clientMu.Lock()
	b.clients[entry] = struct{}{}
	b.clientMu.Unlock()
	replay := b.output.Bytes()
	if len(replay) > 0 {
		_ = sa.WriteBinary(replay)
	}
	// Use a per-client buffer so SetWriter can flip writer / viewer
	// status at runtime without re-attaching the underlying stream.
	perClient := make(chan []byte, 16)
	go func() {
		for data := range perClient {
			b.clientMu.Lock()
			canWrite := entry.canWrite
			b.clientMu.Unlock()
			if !canWrite {
				continue
			}
			select {
			case b.stdinCh <- data:
			case <-b.ctx.Done():
				return
			}
		}
	}()
	sa.StartRead(perClient, func() {
		close(perClient)
		b.detachEntry(entry)
	})
}

func (b *detachableBridge) doAttach(req session.AttachReq) {
	switch c := req.Conn.(type) {
	case *websocket.Conn:
		b.attachWebSocket(c, req.Mode, req.UserID)
	case *StreamAttach:
		b.attachStream(c, req.Mode, req.UserID)
	}
}

//nolint:unparam // Error return preserved for signature parity with the bridge variants.
func (b *detachableBridge) runAttachLoop(initialConn interface{}, attachCh <-chan session.AttachReq) error {
	if initialConn != nil {
		if req, ok := initialConn.(session.AttachReq); ok {
			b.doAttach(req)
		} else {
			b.doAttach(session.AttachReq{Conn: initialConn, Mode: session.AttachModeWriter})
		}
	}
	for {
		select {
		case <-b.ctx.Done():
			if b.endMsg != "" {
				b.broadcastText([]byte(b.endMsg))
			}
			return nil
		case <-b.bridgeDone:
			if b.endMsg != "" {
				b.broadcastText([]byte(b.endMsg))
			}
			return nil
		case req := <-attachCh:
			b.doAttach(req)
		}
	}
}
