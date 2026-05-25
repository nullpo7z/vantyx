package rdpproxy

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/logging"
)

var logger = logging.WithComponent("rdpproxy")

// Bridge connects a WebSocket client to an RDP server at targetAddr
// (typically port 3389). It proxies raw bytes in both directions until
// either side closes the connection.
func Bridge(wsConn *websocket.Conn, targetAddr string) error {
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

	// WebSocket -> TCP: read WS messages and forward them to the RDP server.
	go func() {
		defer closeBoth()
		for {
			mt, data, err := wsConn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					logger.Warn("ws read failed", "error", err)
				}
				return
			}
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			if _, err := tcpConn.Write(data); err != nil {
				logger.Warn("tcp write failed", "error", err)
				return
			}
		}
	}()

	// TCP -> WebSocket: read from the RDP server and emit binary WS frames.
	buf := make([]byte, 32*1024)
	for {
		n, err := tcpConn.Read(buf)
		if n > 0 {
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
