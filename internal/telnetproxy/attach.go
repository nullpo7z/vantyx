package telnetproxy

import (
	"github.com/gorilla/websocket"
)

// StdinRecorder is called when data is sent to the target stdin.
type StdinRecorder interface {
	RecordInput(p []byte)
}

// StdinRecorderFunc adapts a function to StdinRecorder.
type StdinRecorderFunc func(p []byte)

func (f StdinRecorderFunc) RecordInput(p []byte) {
	f(p)
}

type attachableWriter interface {
	WriteBinary([]byte) error
	Close() error
}

type wsWriterAdapter struct{ *websocket.Conn }

func (w *wsWriterAdapter) WriteBinary(p []byte) error {
	return w.WriteMessage(websocket.BinaryMessage, p)
}

// StreamAttach attaches a CLI channel to an existing telnet session.
type StreamAttach struct {
	Write     func([]byte) error
	StartRead func(stdinCh chan<- []byte, onClose func())
	CloseFn   func() error
}

func (s *StreamAttach) WriteBinary(p []byte) error { return s.Write(p) }

func (s *StreamAttach) Close() error {
	if s.CloseFn != nil {
		return s.CloseFn()
	}
	return nil
}
