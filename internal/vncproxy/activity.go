package vncproxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	rfbVersion12 = "RFB 003.008\n"
	secTypeNone  = 1
)

// Monitor connects as a minimal RFB client and calls touch on server messages until ctx is done.
// Used to detect framebuffer updates while the browser WebSocket is detached.
func Monitor(ctx context.Context, targetAddr string, touch func()) error {
	if touch == nil {
		touch = func() {}
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	fbW, fbH, err := rfbHandshake(conn)
	if err != nil {
		return fmt.Errorf("rfb handshake: %w", err)
	}
	touch()

	reqTicker := time.NewTicker(500 * time.Millisecond)
	defer reqTicker.Stop()

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			if ctx.Err() != nil {
				readDone <- ctx.Err()
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, err := conn.Read(buf)
			if n > 0 {
				touch()
			}
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				readDone <- err
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readDone:
			if err != nil && err != io.EOF {
				return err
			}
			return nil
		case <-reqTicker.C:
			if err := sendFramebufferUpdateRequest(conn, fbW, fbH); err != nil {
				return err
			}
		}
	}
}

func rfbHandshake(conn net.Conn) (uint16, uint16, error) {
	var serverVer [12]byte
	if _, err := io.ReadFull(conn, serverVer[:]); err != nil {
		return 0, 0, err
	}
	if _, err := conn.Write([]byte(rfbVersion12)); err != nil {
		return 0, 0, err
	}

	var numSec uint8
	if err := binary.Read(conn, binary.BigEndian, &numSec); err != nil {
		return 0, 0, err
	}
	if numSec == 0 {
		return 0, 0, fmt.Errorf("no security types offered")
	}
	secTypes := make([]byte, numSec)
	if _, err := io.ReadFull(conn, secTypes); err != nil {
		return 0, 0, err
	}
	chosen := byte(secTypeNone)
	found := false
	for _, t := range secTypes {
		if t == secTypeNone {
			found = true
			break
		}
	}
	if !found {
		chosen = secTypes[0]
	}
	if _, err := conn.Write([]byte{chosen}); err != nil {
		return 0, 0, err
	}

	var secResult uint32
	if err := binary.Read(conn, binary.BigEndian, &secResult); err != nil {
		return 0, 0, err
	}
	if secResult != 0 {
		var reasonLen uint32
		if err := binary.Read(conn, binary.BigEndian, &reasonLen); err != nil {
			return 0, 0, fmt.Errorf("security handshake failed (code %d)", secResult)
		}
		reason := make([]byte, reasonLen)
		if _, err := io.ReadFull(conn, reason); err != nil {
			return 0, 0, fmt.Errorf("security handshake failed (code %d)", secResult)
		}
		return 0, 0, fmt.Errorf("security handshake failed: %s", string(reason))
	}

	// ClientInit: shared desktop
	if _, err := conn.Write([]byte{1}); err != nil {
		return 0, 0, err
	}

	// ServerInit: width, height, pixel format, name
	var serverInit [24]byte
	if _, err := io.ReadFull(conn, serverInit[:]); err != nil {
		return 0, 0, err
	}
	fbW := binary.BigEndian.Uint16(serverInit[0:2])
	fbH := binary.BigEndian.Uint16(serverInit[2:4])
	nameLen := binary.BigEndian.Uint32(serverInit[20:24])
	if nameLen > 0 {
		discard := make([]byte, nameLen)
		if _, err := io.ReadFull(conn, discard); err != nil {
			return 0, 0, err
		}
	}

	if err := sendFramebufferUpdateRequest(conn, fbW, fbH); err != nil {
		return 0, 0, err
	}
	return fbW, fbH, nil
}

func sendFramebufferUpdateRequest(conn net.Conn, w, h uint16) error {
	// FramebufferUpdateRequest: type=3, incremental=1, x,y,w,h (uint16 each)
	req := make([]byte, 10)
	req[0] = 3
	req[1] = 1
	binary.BigEndian.PutUint16(req[6:], w)
	binary.BigEndian.PutUint16(req[8:], h)
	_, err := conn.Write(req)
	return err
}
