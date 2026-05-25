package sshd

import (
	"io"
	"sync"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/telnetproxy"
)

// cliStreamAttachOpts configures CLI attach read behaviour.
type cliStreamAttachOpts struct {
	// endOnCtrlD causes 0x04 to terminate the session (connect only).
	endOnCtrlD bool
	// detachOnCtrlBracket causes 0x1d (Ctrl+]) to return to the menu
	// without stopping the underlying bridge.
	detachOnCtrlBracket bool
}

// cliAttachOutcome records whether the user ended the session (Ctrl+D)
// or merely detached (Ctrl+]).
type cliAttachOutcome struct {
	mu    sync.Mutex
	ended bool
}

// markEnded records that the session was ended (Ctrl+D pressed).
func (o *cliAttachOutcome) markEnded() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.ended = true
	o.mu.Unlock()
}

// endedSession reports whether the user pressed Ctrl+D before detaching.
func (o *cliAttachOutcome) endedSession() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ended
}

// newCLIStreamAttach builds an attach handle for SSH or Telnet
// detachable bridges. The returned value is either an
// [*sshproxy.StreamAttach] or [*telnetproxy.StreamAttach] depending on
// protocol; callers pass it through to the corresponding bridge.
func (s *Server) newCLIStreamAttach(protocol access.Protocol, wr io.Writer, inputCh <-chan byte, stopCh <-chan struct{}, readStarted *bool, readDone chan struct{}, closeFn func() error, opts cliStreamAttachOpts, outcome *cliAttachOutcome) interface{} {
	writeFn := func(p []byte) error { _, e := wr.Write(p); return e }
	stopForControl := func(b byte) bool {
		if opts.detachOnCtrlBracket && b == 0x1d {
			return true
		}
		if opts.endOnCtrlD && b == 0x04 {
			outcome.markEnded()
			return true
		}
		return false
	}
	startRead := func(stdinCh chan<- []byte, onClose func()) {
		if readStarted != nil {
			*readStarted = true
		}
		go func() {
			defer func() {
				onClose()
				if readDone != nil {
					close(readDone)
				}
			}()
			for {
				select {
				case b, ok := <-inputCh:
					if !ok {
						return
					}
					if stopForControl(b) {
						return
					}
					buf := []byte{b}
				drain:
					for {
						select {
						case b, ok = <-inputCh:
							if !ok {
								break drain
							}
							if stopForControl(b) {
								return
							}
							buf = append(buf, b)
						default:
							break drain
						}
					}
					cp := make([]byte, len(buf))
					copy(cp, buf)
					select {
					case stdinCh <- cp:
					case <-stopCh:
						return
					}
				case <-stopCh:
					return
				}
			}
		}()
	}
	if protocol == access.ProtocolTelnet {
		return &telnetproxy.StreamAttach{Write: writeFn, StartRead: startRead, CloseFn: closeFn}
	}
	return &sshproxy.StreamAttach{Write: writeFn, StartRead: startRead, CloseFn: closeFn}
}
