package sshproxy

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nullpo7z/vantyx/internal/mock"
	"github.com/nullpo7z/vantyx/internal/session"
)

// recordingStreamAttach captures everything the bridge writes to it
// and supplies a stdin pump backed by a controllable channel. Used by
// the multi-client tests to assert fan-out and writer / viewer
// behaviour without spinning up real WebSockets.
type recordingStreamAttach struct {
	id string
	// written is filled by the bridge's broadcast goroutine while the
	// test polls it; guard it so the -race detector stays quiet.
	mu      sync.Mutex
	written bytes.Buffer
	closed  atomic.Bool
	stdinIn chan []byte
	onClose func()
	starter chan struct{}
	closeFn func() error
}

// out returns a snapshot of everything written so far.
func (s *recordingStreamAttach) out() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.written.Bytes()...)
}

func (s *recordingStreamAttach) outString() string { return string(s.out()) }

func newRecordingStreamAttach(id string) *recordingStreamAttach {
	return &recordingStreamAttach{
		id:      id,
		stdinIn: make(chan []byte, 16),
		starter: make(chan struct{}, 1),
	}
}

func (s *recordingStreamAttach) toStreamAttach() *StreamAttach {
	return &StreamAttach{
		Write: func(p []byte) error {
			s.mu.Lock()
			s.written.Write(p)
			s.mu.Unlock()
			return nil
		},
		StartRead: func(stdinChOut chan<- []byte, onClose func()) {
			s.onClose = onClose
			s.starter <- struct{}{}
			go func() {
				for b := range s.stdinIn {
					select {
					case stdinChOut <- b:
					default:
					}
				}
			}()
		},
		CloseFn: func() error {
			s.closed.Store(true)
			if s.closeFn != nil {
				return s.closeFn()
			}
			return nil
		},
	}
}

// TestBridgeFanOutAndWriterEnforcement covers the headline collaborative
// invariants: every attached client sees output, but only the writer's
// stdin survives the trip to the target.
func TestBridgeFanOutAndWriterEnforcement(t *testing.T) {
	server, err := mock.NewSSHEchoServer("test", "test")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()
	port := server.Port()
	if port == 0 {
		t.Fatal("port = 0")
	}

	output := session.NewRingBuffer(8192)
	attachCh := make(chan session.AttachReq, 4)

	owner := newRecordingStreamAttach("owner")
	viewer := newRecordingStreamAttach("viewer")

	bridgeErrCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		bridgeErrCh <- RunBridgeDetachable(ctx, "session_ended: SSH session closed",
			"127.0.0.1", port, "test", "test", "", "",
			output, attachCh,
			session.AttachReq{Conn: owner.toStreamAttach(), UserID: "alice", Mode: session.AttachModeWriter},
			nil, nil, nil, 0, 0, nil)
	}()

	// Wait for the owner attach to start its read loop.
	select {
	case <-owner.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("owner StartRead not invoked")
	}

	// Attach a viewer.
	attachCh <- session.AttachReq{Conn: viewer.toStreamAttach(), UserID: "bob", Mode: session.AttachModeViewer}
	select {
	case <-viewer.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("viewer StartRead not invoked")
	}

	// Owner sends a command - the echo server should reply with the
	// same bytes. Both clients must receive the output.
	owner.stdinIn <- []byte("hello-from-owner\n")
	// Viewer attempts input; bridge must drop it.
	viewer.stdinIn <- []byte("VIEWER-INPUT-MUST-BE-DROPPED\n")

	deadline := time.After(3 * time.Second)
	for !(bytes.Contains(owner.out(), []byte("hello-from-owner")) &&
		bytes.Contains(viewer.out(), []byte("hello-from-owner"))) {
		select {
		case <-deadline:
			t.Fatalf("fan-out missing: owner=%q viewer=%q", owner.outString(), viewer.outString())
		case <-time.After(50 * time.Millisecond):
		}
	}

	// The viewer's input must NOT appear in the echoed stream.
	if bytes.Contains(owner.out(), []byte("VIEWER-INPUT-MUST-BE-DROPPED")) {
		t.Fatalf("viewer input leaked to writer: %q", owner.outString())
	}
}

// TestBridgeSetWriterTransfersControl checks that promoting a viewer
// via SetWriter unblocks their stdin and that the previous writer is
// downgraded.
func TestBridgeSetWriterTransfersControl(t *testing.T) {
	server, err := mock.NewSSHEchoServer("test", "test")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()

	output := session.NewRingBuffer(8192)
	attachCh := make(chan session.AttachReq, 4)

	owner := newRecordingStreamAttach("owner")
	viewer := newRecordingStreamAttach("viewer")

	type sinkResult struct {
		ctrl BridgeController
	}
	resultCh := make(chan sinkResult, 1)
	sink := bridgeControlFunc(func(c BridgeController) {
		resultCh <- sinkResult{ctrl: c}
	})

	bridgeErrCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		bridgeErrCh <- RunBridgeDetachable(ctx, "session_ended: SSH session closed",
			"127.0.0.1", server.Port(), "test", "test", "", "",
			output, attachCh,
			session.AttachReq{Conn: owner.toStreamAttach(), UserID: "alice", Mode: session.AttachModeWriter},
			nil, nil, nil, 0, 0, nil,
			WithBridgeControlSink(sink),
		)
	}()

	select {
	case <-owner.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("owner attach not started")
	}
	attachCh <- session.AttachReq{Conn: viewer.toStreamAttach(), UserID: "bob", Mode: session.AttachModeViewer}
	select {
	case <-viewer.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("viewer attach not started")
	}

	var ctrl BridgeController
	select {
	case res := <-resultCh:
		ctrl = res.ctrl
	case <-time.After(2 * time.Second):
		t.Fatal("bridge controller not registered")
	}

	// Promote bob.
	ctrl.SetWriter("bob")

	owner.stdinIn <- []byte("from-old-writer\n")
	viewer.stdinIn <- []byte("from-new-writer\n")

	deadline := time.After(3 * time.Second)
	for !bytes.Contains(viewer.out(), []byte("from-new-writer")) {
		select {
		case <-deadline:
			t.Fatalf("new writer's input did not echo back: %q", viewer.outString())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if bytes.Contains(viewer.out(), []byte("from-old-writer")) {
		t.Fatalf("demoted writer's input must be dropped: %q", viewer.outString())
	}
}

// TestBridgeSecondWriterAttachDemotesFirst covers the case that
// motivated demoteOtherWritersLocked: a second client attaches with
// Mode: AttachModeWriter (e.g. the same session resumed from the SSH
// CLI console while it's already open in a browser tab, or the
// reverse) without going through the explicit SetWriter promotion
// flow used by the sharing/invite feature. Only the most recently
// attached writer's stdin may reach the target; the first must be
// silently downgraded to read-only, not left able to write
// concurrently.
func TestBridgeSecondWriterAttachDemotesFirst(t *testing.T) {
	server, err := mock.NewSSHEchoServer("test", "test")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()

	output := session.NewRingBuffer(8192)
	attachCh := make(chan session.AttachReq, 4)

	first := newRecordingStreamAttach("first")
	second := newRecordingStreamAttach("second")

	bridgeErrCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		bridgeErrCh <- RunBridgeDetachable(ctx, "session_ended: SSH session closed",
			"127.0.0.1", server.Port(), "test", "test", "", "",
			output, attachCh,
			session.AttachReq{Conn: first.toStreamAttach(), UserID: "alice", Mode: session.AttachModeWriter},
			nil, nil, nil, 0, 0, nil)
	}()

	select {
	case <-first.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("first attach not started")
	}

	// Second client attaches as writer too -- plain reattach, not a
	// SetWriter promotion (mirrors SSH-console + browser both resuming
	// the same session).
	attachCh <- session.AttachReq{Conn: second.toStreamAttach(), UserID: "alice", Mode: session.AttachModeWriter}
	select {
	case <-second.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("second attach not started")
	}

	first.stdinIn <- []byte("from-first-writer\n")
	second.stdinIn <- []byte("from-second-writer\n")

	deadline := time.After(3 * time.Second)
	for !bytes.Contains(second.out(), []byte("from-second-writer")) {
		select {
		case <-deadline:
			t.Fatalf("second (current) writer's input did not echo back: %q", second.outString())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if bytes.Contains(second.out(), []byte("from-first-writer")) {
		t.Fatalf("first writer must be demoted once the second attaches: %q", second.outString())
	}
}

// TestBridgeDetachUser closes only the targeted user's connection.
func TestBridgeDetachUser(t *testing.T) {
	server, err := mock.NewSSHEchoServer("test", "test")
	if err != nil {
		t.Fatalf("NewSSHEchoServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Close()

	output := session.NewRingBuffer(8192)
	attachCh := make(chan session.AttachReq, 4)
	owner := newRecordingStreamAttach("owner")
	viewer := newRecordingStreamAttach("viewer")

	resultCh := make(chan BridgeController, 1)
	sink := bridgeControlFunc(func(c BridgeController) { resultCh <- c })

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = RunBridgeDetachable(ctx, "session_ended: SSH session closed",
			"127.0.0.1", server.Port(), "test", "test", "", "",
			output, attachCh,
			session.AttachReq{Conn: owner.toStreamAttach(), UserID: "alice", Mode: session.AttachModeWriter},
			nil, nil, nil, 0, 0, nil,
			WithBridgeControlSink(sink),
		)
	}()

	select {
	case <-owner.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("owner attach not started")
	}
	attachCh <- session.AttachReq{Conn: viewer.toStreamAttach(), UserID: "bob", Mode: session.AttachModeViewer}
	select {
	case <-viewer.starter:
	case <-time.After(2 * time.Second):
		t.Fatal("viewer attach not started")
	}

	var ctrl BridgeController
	select {
	case ctrl = <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("bridge controller not registered")
	}

	ctrl.DetachUser("bob")
	deadline := time.After(2 * time.Second)
	for !viewer.closed.Load() {
		select {
		case <-deadline:
			t.Fatal("viewer connection not closed after DetachUser")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if owner.closed.Load() {
		t.Fatal("owner connection must remain open")
	}
}

// bridgeControlFunc is a tiny adapter that implements the
// BridgeControlSink interface with a closure.
type bridgeControlFunc func(BridgeController)

func (f bridgeControlFunc) Register(c BridgeController) { f(c) }
