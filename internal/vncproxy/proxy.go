package vncproxy

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Bridge connects a noVNC WebSocket client to a VNC server at targetAddr.
// It proxies raw bytes both ways until either side closes. RFB protocol is unchanged.
// If touch is non-nil, it is called on client and server I/O (e.g. for session last_seen).
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

	// WebSocket -> TCP: read WS messages, write to VNC server
	go func() {
		defer closeBoth()
		for {
			mt, data, err := wsConn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Printf("vncproxy: ws read err: %v", err)
				}
				return
			}
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			if touch != nil {
				touch()
			}
			if _, err := tcpConn.Write(data); err != nil {
				log.Printf("vncproxy: tcp write err: %v", err)
				return
			}
		}
	}()

	// TCP -> WebSocket: read from VNC server, write as binary WS frames
	buf := make([]byte, 32*1024)
	for {
		n, err := tcpConn.Read(buf)
		if n > 0 {
			if touch != nil {
				touch()
			}
			if err := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Printf("vncproxy: ws write err: %v", err)
				break
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("vncproxy: tcp read err: %v", err)
			}
			break
		}
	}
	closeBoth()
	return nil
}
