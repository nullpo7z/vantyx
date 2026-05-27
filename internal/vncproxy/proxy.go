package vncproxy

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/logging"
)

var logger = logging.WithComponent("vncproxy")

// maxClientMessageBytes bounds client->server WebSocket frames to avoid
// unbounded memory growth in gorilla/websocket's ReadMessage path (CWE-770).
// VNC client traffic is typically small (input events); large framebuffer data
// flows server->client.
const maxClientMessageBytes int64 = 1 << 20 // 1 MiB

// Bridge connects a noVNC WebSocket client to a VNC server at targetAddr.
//
// It proxies raw bytes in both directions until either side closes; the
// RFB protocol payload is unchanged. When touch is non-nil it is invoked
// on every successful I/O step so the caller can update session
// last-seen timestamps for idle warnings.
func Bridge(wsConn *websocket.Conn, targetAddr string, touch func()) error {
	tcpConn, err := net.DialTimeout("tcp", targetAddr, 15*time.Second)
	if err != nil {
		return err
	}
	defer func() {
		_ = tcpConn.Close()
	}()

	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = tcpConn.Close()
			_ = wsConn.Close()
		})
	}

	// WebSocket -> TCP: read WS messages, write to the VNC server.
	wsConn.SetReadLimit(maxClientMessageBytes)
	go func() {
		defer closeBoth()
		for {
			mt, r, err := wsConn.NextReader()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					logger.Warn("ws read failed", "error", err)
				}
				return
			}
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			if touch != nil {
				touch()
			}
			if _, err := io.Copy(tcpConn, r); err != nil {
				logger.Warn("tcp write failed", "error", err)
				return
			}
		}
	}()

	// TCP -> WebSocket: read from the VNC server and forward as binary
	// frames so noVNC interprets them as raw RFB bytes.
	buf := make([]byte, 32*1024)
	for {
		n, err := tcpConn.Read(buf)
		if n > 0 {
			if touch != nil {
				touch()
			}
			if err := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				logger.Warn("ws write failed", "error", err)
				break
			}
		}
		if err != nil {
			if err != io.EOF {
				logger.Warn("tcp read failed", "error", err)
			}
			break
		}
	}
	closeBoth()
	return nil
}
