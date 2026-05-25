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

type sshDetachableBridge struct {
	ctx            context.Context
	stdin          io.WriteCloser
	output         *session.RingBuffer
	windowChange   func(cols, rows int) error
	touch          func()
	tee            io.Writer
	stdinRecorder  StdinRecorder
	stdinCh        chan []byte
	clientMu       sync.Mutex
	client         attachableWriter
	bridgeDone     chan struct{}
	closeStdin     func()
	attachCh       <-chan session.AttachReq
	externalResize <-chan TerminalSize
}

func newSSHDetachableBridge(
	ctx context.Context,
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
		stdin:          stdin,
		output:         output,
		windowChange:   windowChange,
		touch:          touch,
		tee:            tee,
		stdinRecorder:  stdinRecorder,
		stdinCh:        stdinCh,
		bridgeDone:     bridgeDone,
		closeStdin:     closeStdin,
		attachCh:       attachCh,
		externalResize: externalResize,
	}
	return b
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
			b.clientMu.Lock()
			c := b.client
			b.clientMu.Unlock()
			if c != nil {
				_ = c.WriteBinary(buf[:n])
			}
		}
		if err != nil {
			b.closeStdin()
			return
		}
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

func (b *sshDetachableBridge) attachWebSocket(conn *websocket.Conn) {
	b.clientMu.Lock()
	if b.client != nil {
		_ = b.client.Close()
	}
	w := &wsWriterAdapter{conn}
	b.client = w
	b.clientMu.Unlock()
	replay := b.output.Bytes()
	if len(replay) > 0 {
		_ = conn.WriteMessage(websocket.BinaryMessage, replay)
	}
	go b.readWebSocket(w, conn)
}

func (b *sshDetachableBridge) readWebSocket(adapter *wsWriterAdapter, conn *websocket.Conn) {
	defer func() {
		b.clientMu.Lock()
		if b.client == adapter {
			b.client = nil
		}
		b.clientMu.Unlock()
	}()
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
		if mt == websocket.TextMessage && b.windowChange != nil && len(msg) > 0 && msg[0] == '{' && strings.Contains(string(msg), `"type":"resize"`) {
			var rm struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(msg, &rm) == nil && rm.Type == "resize" && rm.Cols > 0 && rm.Rows > 0 {
				_ = b.windowChange(rm.Cols, rm.Rows)
				continue
			}
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

func (b *sshDetachableBridge) attachStream(sa *StreamAttach) {
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

func (b *sshDetachableBridge) doAttach(conn interface{}) {
	switch c := conn.(type) {
	case *websocket.Conn:
		b.attachWebSocket(c)
	case *StreamAttach:
		b.attachStream(c)
	}
}

//nolint:unparam // Error return preserved for signature parity with the bridge variants.
func (b *sshDetachableBridge) run(initialConn interface{}) error {
	if initialConn != nil {
		b.doAttach(initialConn)
	}
	b.runExternalResize()
	for {
		select {
		case <-b.ctx.Done():
			return nil
		case <-b.bridgeDone:
			return nil
		case req := <-b.attachCh:
			b.doAttach(req.Conn)
		}
	}
}
