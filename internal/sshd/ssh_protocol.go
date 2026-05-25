package sshd

import (
	"bytes"
	"encoding/binary"
	"io"

	"golang.org/x/crypto/ssh"
)

// parsePtyReqPayload extracts terminal width (cols) and height (rows)
// from an SSH pty-req payload (RFC 4254). Returns (0, 0, false) if the
// payload is too short or malformed.
func parsePtyReqPayload(payload []byte) (cols, rows int, ok bool) {
	if len(payload) < 12 {
		return 0, 0, false
	}
	r := bytes.NewReader(payload)
	var termLen uint32
	if err := binary.Read(r, binary.BigEndian, &termLen); err != nil {
		return 0, 0, false
	}
	if int(termLen) < 0 || len(payload) < 4+int(termLen)+8 {
		return 0, 0, false
	}
	if _, err := r.Seek(int64(4+termLen), io.SeekStart); err != nil {
		return 0, 0, false
	}
	var w, h uint32
	if err := binary.Read(r, binary.BigEndian, &w); err != nil {
		return 0, 0, false
	}
	if err := binary.Read(r, binary.BigEndian, &h); err != nil {
		return 0, 0, false
	}
	return int(w), int(h), true
}

// parseWindowChangePayload extracts cols and rows from an SSH
// window-change payload (RFC 4254).
func parseWindowChangePayload(payload []byte) (cols, rows int, ok bool) {
	if len(payload) < 8 {
		return 0, 0, false
	}
	r := bytes.NewReader(payload)
	var w, h uint32
	if err := binary.Read(r, binary.BigEndian, &w); err != nil {
		return 0, 0, false
	}
	if err := binary.Read(r, binary.BigEndian, &h); err != nil {
		return 0, 0, false
	}
	return int(w), int(h), true
}

// sendExitStatus sends an SSH exit-status request (RFC 4254 §6.10) so
// the remote client observes the desired exit code on normal close.
func sendExitStatus(channel ssh.Channel, code uint32) {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, code)
	_, _ = channel.SendRequest("exit-status", false, payload)
}
