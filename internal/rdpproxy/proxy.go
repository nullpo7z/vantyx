package rdpproxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Bridge connects a WebSocket client to an RDP server at targetAddr (typically port 3389).
// It proxies raw bytes both ways until either side closes.
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

	// WebSocket -> TCP: read WS messages, write to RDP server
	go func() {
		defer closeBoth()
		for {
			mt, data, err := wsConn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("rdpproxy: ws read err: %v", err)
				}
				return
			}
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			if _, err := tcpConn.Write(data); err != nil {
				log.Printf("rdpproxy: tcp write err: %v", err)
				return
			}
		}
	}()

	// TCP -> WebSocket: read from RDP server, write as binary WS frames
	buf := make([]byte, 32*1024)
	for {
		n, err := tcpConn.Read(buf)
		if n > 0 {
			if err := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Printf("rdpproxy: ws write err: %v", err)
				break
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("rdpproxy: tcp read err: %v", err)
			}
			break
		}
	}
	closeBoth()
	return nil
}
