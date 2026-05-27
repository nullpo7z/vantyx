package sshproxy

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
)

// clientEntry tracks a single attached client (browser or CLI).
//
// canWrite controls whether stdin coming from this client is forwarded
// to the upstream SSH session; the bridge layer enforces single-writer
// semantics so two browsers cannot fight over the same PTY. The
// userID is informational and is set from session.AttachReq.UserID
// when the http layer attaches a participant.
type clientEntry struct {
	w        attachableWriter
	canWrite bool
	userID   string
}

type sshDetachableBridge struct {
	ctx            context.Context
	endMsg         string
	stdin          io.WriteCloser
	output         *session.RingBuffer
	windowChange   func(cols, rows int) error
	touch          func()
	tee            io.Writer
	stdinRecorder  StdinRecorder
	stdinCh        chan []byte
	clientMu       sync.Mutex
	clients        map[*clientEntry]struct{}
	bridgeDone     chan struct{}
	closeStdin     func()
	attachCh       <-chan session.AttachReq
	externalResize <-chan TerminalSize
}

func newSSHDetachableBridge(
	ctx context.Context,
	endMsg string,
	stdin io.WriteCloser,
	output *session.RingBuffer,
	windowChange func(cols, rows int) error,
	touch func(),
	tee io.Writer,
	stdinRecorder StdinRecorder,
	attachCh <-chan session.AttachReq,
	externalResize <-chan TerminalSize,
) *sshDetachableBridge {
	stdinCh := make(chan []byte, 256)
	var stdinCloseOnce sync.Once
	closeStdin := func() { stdinCloseOnce.Do(func() { _ = stdin.Close() }) }
	bridgeDone := make(chan struct{})
	b := &sshDetachableBridge{
		ctx:            ctx,
		endMsg:         endMsg,
		stdin:          stdin,
		output:         output,
		windowChange:   windowChange,
		touch:          touch,
		tee:            tee,
		stdinRecorder:  stdinRecorder,
		stdinCh:        stdinCh,
		clients:        make(map[*clientEntry]struct{}),
		bridgeDone:     bridgeDone,
		closeStdin:     closeStdin,
		attachCh:       attachCh,
		externalResize: externalResize,
	}
	return b
}

func (b *sshDetachableBridge) broadcastText(p []byte) {
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

func (b *sshDetachableBridge) startPumps(stdout, stderr io.Reader) {
	go b.runStdinPump()
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() { defer wg.Done(); b.pumpReader(stdout) }()
	wg.Add(1)
	go func() { defer wg.Done(); b.pumpReader(stderr) }()
	go func() {
		wg.Wait()
		close(b.bridgeDone)
	}()
}

func (b *sshDetachableBridge) runStdinPump() {
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
				_, _ = b.stdin.Write(data)
			}
		}
	}
}

func (b *sshDetachableBridge) pumpReader(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if b.touch != nil {
				b.touch()
			}
			_, _ = b.output.Write(buf[:n])
			if b.tee != nil {
				_, _ = b.tee.Write(buf[:n])
			}
			b.broadcast(buf[:n])
		}
		if err != nil {
			b.closeStdin()
			return
		}
	}
}

// broadcast fans the same payload out to every attached client. A
// failed write detaches the offending client so a stuck viewer cannot
// pin the bridge.
func (b *sshDetachableBridge) broadcast(p []byte) {
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

// SetWriter sets the user ID that may write to stdin. All attached
// clients with a matching userID become writers, every other client
// is downgraded to read-only. The bridge guarantees at most one
// active writer regardless of how many clients the same user has
// open. An empty userID demotes every client to viewer.
func (b *sshDetachableBridge) SetWriter(userID string) {
	b.clientMu.Lock()
	defer b.clientMu.Unlock()
	for c := range b.clients {
		c.canWrite = userID != "" && c.userID == userID
	}
}

func (b *sshDetachableBridge) runExternalResize() {
	if b.externalResize == nil || b.windowChange == nil {
		return
	}
	go func() {
		for {
			select {
			case <-b.ctx.Done():
				return
			case sz, ok := <-b.externalResize:
				if !ok {
					return
				}
				if sz.Cols > 0 && sz.Rows > 0 {
					_ = b.windowChange(sz.Cols, sz.Rows)
				}
			}
		}
	}()
}

func (b *sshDetachableBridge) attachWebSocket(conn *websocket.Conn, mode session.AttachMode, userID string) {
	w := &wsWriterAdapter{conn}
	entry := &clientEntry{w: w, canWrite: mode != session.AttachModeViewer, userID: userID}
	b.clientMu.Lock()
	b.clients[entry] = struct{}{}
	b.clientMu.Unlock()
	replay := b.output.Bytes()
	if len(replay) > 0 {
		if err := conn.WriteMessage(websocket.BinaryMessage, replay); err != nil {
			b.detachEntry(entry)
			return
		}
	}
	go b.readWebSocket(entry, conn)
}

func (b *sshDetachableBridge) detachEntry(entry *clientEntry) {
	b.clientMu.Lock()
	if _, ok := b.clients[entry]; ok {
		delete(b.clients, entry)
	}
	b.clientMu.Unlock()
	_ = entry.w.Close()
}

func (b *sshDetachableBridge) readWebSocket(entry *clientEntry, conn *websocket.Conn) {
	defer b.detachEntry(entry)
	for {
		mt, msg, err := conn.ReadMessage()
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
		// Resize messages are honoured for every attached client (so
		// the writer's PTY can match the writer's window even when
		// they reattach), but only the writer can drive stdin.
		if mt == websocket.TextMessage && b.windowChange != nil && len(msg) > 0 && msg[0] == '{' && strings.Contains(string(msg), `"type":"resize"`) {
			var rm struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(msg, &rm) == nil && rm.Type == "resize" && rm.Cols > 0 && rm.Rows > 0 {
				b.clientMu.Lock()
				canWrite := entry.canWrite
				b.clientMu.Unlock()
				if canWrite {
					_ = b.windowChange(rm.Cols, rm.Rows)
				}
				continue
			}
		}
		if !isDataMessage(mt) {
			continue
		}
		b.clientMu.Lock()
		canWrite := entry.canWrite
		b.clientMu.Unlock()
		if !canWrite {
			// Drop input from viewers entirely; never reaches stdin.
			continue
		}
		select {
		case b.stdinCh <- msg:
		case <-b.ctx.Done():
			return
		}
	}
}

func (b *sshDetachableBridge) attachStream(sa *StreamAttach, mode session.AttachMode, userID string) {
	entry := &clientEntry{w: sa, canWrite: mode != session.AttachModeViewer, userID: userID}
	b.clientMu.Lock()
	b.clients[entry] = struct{}{}
	b.clientMu.Unlock()
	replay := b.output.Bytes()
	if len(replay) > 0 {
		_ = sa.WriteBinary(replay)
	}
	// Use a per-client buffer so writer / viewer status can flip at
	// runtime via SetWriter. The forwarder goroutine consults
	// entry.canWrite for every chunk; this keeps the StreamAttach
	// API stable while still letting the bridge enforce the
	// single-writer invariant.
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

func (b *sshDetachableBridge) doAttach(req session.AttachReq) {
	switch c := req.Conn.(type) {
	case *websocket.Conn:
		b.attachWebSocket(c, req.Mode, req.UserID)
	case *StreamAttach:
		b.attachStream(c, req.Mode, req.UserID)
	}
}

//nolint:unparam // Error return preserved for signature parity with the bridge variants.
func (b *sshDetachableBridge) run(initialConn interface{}) error {
	if initialConn != nil {
		// Callers may pass either a raw *websocket.Conn / *StreamAttach
		// (legacy "single writer" behaviour) or a session.AttachReq
		// carrying user metadata for the new collaborative attach flow.
		if req, ok := initialConn.(session.AttachReq); ok {
			b.doAttach(req)
		} else {
			b.doAttach(session.AttachReq{Conn: initialConn, Mode: session.AttachModeWriter})
		}
	}
	b.runExternalResize()
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
		case req := <-b.attachCh:
			b.doAttach(req)
		}
	}
}
