package httpapi

import (
	"sync"

	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/telnetproxy"
	"github.com/nullpo7z/vantyx/internal/vncproxy"
)

// bridgeController is kept as an alias for local bridge structs.
type bridgeController = sharing.BridgeControl

// bridgeRegistry tracks the live bridge controllers indexed by their
// terminal session ID. Entries are added when the bridge starts (via
// the sink registered in terminal.go) and removed when the session
// goroutine exits.
type bridgeRegistry struct {
	mu      sync.RWMutex
	bridges map[session.ID]sharing.BridgeControl
}

func newBridgeRegistry() *bridgeRegistry {
	return &bridgeRegistry{bridges: make(map[session.ID]sharing.BridgeControl)}
}

// Register stores controller under id, replacing any existing entry.
func (br *bridgeRegistry) Register(id session.ID, controller sharing.BridgeControl) {
	if br == nil {
		return
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	br.bridges[id] = controller
}

// Unregister removes id, typically called from the session-end
// callback so a stale controller cannot be promoted later.
func (br *bridgeRegistry) Unregister(id session.ID) {
	if br == nil {
		return
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	delete(br.bridges, id)
}

// Get returns the controller for id, if registered.
func (br *bridgeRegistry) Get(id session.ID) (sharing.BridgeControl, bool) {
	if br == nil {
		return nil, false
	}
	br.mu.RLock()
	defer br.mu.RUnlock()
	c, ok := br.bridges[id]
	return c, ok
}

type vncBridgeSink struct {
	id session.ID
	br *bridgeRegistry
}

func (s vncBridgeSink) Register(c vncproxy.BridgeController) {
	s.br.Register(s.id, bridgeController(c))
}

// sshBridgeSink adapts a session.ID-keyed bridgeRegistry to the
// sshproxy.BridgeControlSink interface.
type sshBridgeSink struct {
	id session.ID
	br *bridgeRegistry
}

// Register implements sshproxy.BridgeControlSink.
func (s sshBridgeSink) Register(c sshproxy.BridgeController) {
	s.br.Register(s.id, bridgeController(c))
}

// telnetBridgeSink wraps the telnet controller in the same way.
type telnetBridgeSink struct {
	id session.ID
	br *bridgeRegistry
}

// Register implements telnetproxy.BridgeControlSink.
func (s telnetBridgeSink) Register(c telnetproxy.BridgeController) {
	s.br.Register(s.id, bridgeController(c))
}
