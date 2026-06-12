package vncproxy

import (
	"context"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
)

type vncClientEntry struct {
	w        *websocket.Conn
	tcp      net.Conn
	canWrite bool
	userID   string
}

type detachableVNCBBridge struct {
	ctx        context.Context
	targetAddr string
	touch      func()
	clientMu   sync.Mutex
	clients    map[*vncClientEntry]struct{}
	bridgeDone chan struct{}
	attachCh   <-chan session.AttachReq
}

func newDetachableVNCBBridge(ctx context.Context, targetAddr string, touch func(), attachCh <-chan session.AttachReq) *detachableVNCBBridge {
	return &detachableVNCBBridge{
		ctx:        ctx,
		targetAddr: targetAddr,
		touch:      touch,
		clients:    make(map[*vncClientEntry]struct{}),
		bridgeDone: make(chan struct{}),
		attachCh:   attachCh,
	}
}

// BridgeController exposes runtime writer / detach hooks for sharing.
type BridgeController interface {
	SetWriter(userID string)
	DetachUser(userID string)
}

// BridgeControlSink receives the controller when the bridge starts.
type BridgeControlSink interface {
	Register(controller BridgeController)
}

// SetWriter promotes clients owned by userID to writer; others become viewers.
func (b *detachableVNCBBridge) SetWriter(userID string) {
	b.clientMu.Lock()
	defer b.clientMu.Unlock()
	for c := range b.clients {
		c.canWrite = userID != "" && c.userID == userID
	}
}

// DetachUser closes every client connection owned by userID.
func (b *detachableVNCBBridge) DetachUser(userID string) {
	if userID == "" {
		return
	}
	b.clientMu.Lock()
	toClose := make([]*vncClientEntry, 0)
	for c := range b.clients {
		if c.userID == userID {
			toClose = append(toClose, c)
		}
	}
	for _, c := range toClose {
		delete(b.clients, c)
	}
	b.clientMu.Unlock()
	for _, c := range toClose {
		b.closeEntry(c)
	}
}

func (b *detachableVNCBBridge) closeEntry(entry *vncClientEntry) {
	if entry.tcp != nil {
		_ = entry.tcp.Close()
	}
	if entry.w != nil {
		_ = entry.w.Close()
	}
}

func (b *detachableVNCBBridge) detachEntry(entry *vncClientEntry) {
	b.clientMu.Lock()
	delete(b.clients, entry)
	b.clientMu.Unlock()
	b.closeEntry(entry)
}

func (b *detachableVNCBBridge) attachWebSocket(wsConn *websocket.Conn, mode session.AttachMode, userID string) {
	tcpConn, err := net.DialTimeout("tcp", b.targetAddr, 15*time.Second)
	if err != nil {
		_ = wsConn.Close()
		return
	}
	entry := &vncClientEntry{
		w:        wsConn,
		tcp:      tcpConn,
		canWrite: mode != session.AttachModeViewer,
		userID:   userID,
	}
	b.clientMu.Lock()
	b.clients[entry] = struct{}{}
	b.clientMu.Unlock()

	wsConn.SetReadLimit(maxClientMessageBytes)
	go b.pumpTCPToWS(entry)
	go b.pumpWSToTCP(entry)
}

func (b *detachableVNCBBridge) pumpTCPToWS(entry *vncClientEntry) {
	defer b.detachEntry(entry)
	buf := make([]byte, 32*1024)
	for {
		n, err := entry.tcp.Read(buf)
		if n > 0 {
			if b.touch != nil {
				b.touch()
			}
			if err := entry.w.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (b *detachableVNCBBridge) pumpWSToTCP(entry *vncClientEntry) {
	for {
		mt, r, err := entry.w.NextReader()
		if err != nil {
			return
		}
		if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
			continue
		}
		b.clientMu.Lock()
		canWrite := entry.canWrite
		b.clientMu.Unlock()
		if !canWrite {
			_, _ = io.Copy(io.Discard, r)
			continue
		}
		if b.touch != nil {
			b.touch()
		}
		if _, err := io.Copy(entry.tcp, r); err != nil {
			return
		}
	}
}

func (b *detachableVNCBBridge) doAttach(req session.AttachReq) {
	conn, ok := req.Conn.(*websocket.Conn)
	if !ok || conn == nil {
		return
	}
	b.attachWebSocket(conn, req.Mode, req.UserID)
}

// RunBridgeDetachable runs a multi-client VNC bridge. Each viewer gets
// an independent upstream TCP connection (requires a shared-capable VNC
// server). initialConn may be *websocket.Conn or session.AttachReq.
func RunBridgeDetachable(ctx context.Context, targetAddr string, attachCh <-chan session.AttachReq, initialConn interface{}, touch func(), sink BridgeControlSink) error {
	b := newDetachableVNCBBridge(ctx, targetAddr, touch, attachCh)
	if sink != nil {
		sink.Register(b)
	}
	if initialConn != nil {
		if req, ok := initialConn.(session.AttachReq); ok {
			b.doAttach(req)
		} else if conn, ok := initialConn.(*websocket.Conn); ok {
			b.doAttach(session.AttachReq{Conn: conn, Mode: session.AttachModeWriter})
		}
	}
	for {
		select {
		case <-ctx.Done():
			b.clientMu.Lock()
			for c := range b.clients {
				b.closeEntry(c)
			}
			b.clients = make(map[*vncClientEntry]struct{})
			b.clientMu.Unlock()
			return nil
		case req := <-attachCh:
			b.doAttach(req)
		}
	}
}
