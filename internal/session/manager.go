package session

import (
	"context"
	"sync"
	"time"

	"github.com/nullpo7z/vantyx/internal/logging"
)

var logger = logging.WithComponent("session")

// ID represents a logical session identifier.
type ID string

// StartOptions holds metadata for a new terminal session (used for attach auth and listing).
type StartOptions struct {
	UserID      string
	TargetID    string
	TargetName  string
	Name        string // optional human-readable session name (for identification).
	Description string // optional free-form description.
}

// Manager manages long-lived sessions backed by goroutines.
type Manager struct {
	mu            sync.RWMutex
	sessions      map[ID]*Session
	now           func() time.Time
	idleWarnAfter time.Duration // 0 = idle warnings disabled
	// stopTimeout is how long Stop() waits for a session's goroutine to
	// exit after cancelling it before force-removing it anyway. A
	// struct field (not a local const) so tests can shrink it instead
	// of a multi-second real-time wait.
	stopTimeout time.Duration
}

// attachChannelBuffer is the AttachCh capacity. It is sized to absorb
// a small spike of joining viewers without blocking the HTTP layer;
// the bridge drains from the channel as fast as it can.
const attachChannelBuffer = 16

// AttachMode identifies how a newly-attached client should be wired
// into the bridge. Phase A introduces the writer / viewer split for
// multi-participant sessions; older code paths that just want a
// single attached client can leave the mode zero (writer).
type AttachMode int

const (
	// AttachModeWriter is the legacy attach mode: the new client
	// receives output and may send stdin. Sole writer at any time.
	AttachModeWriter AttachMode = 0
	// AttachModeViewer attaches a read-only client. The bridge fans
	// stdout/stderr out to the viewer, but discards anything they
	// send on the wire.
	AttachModeViewer AttachMode = 1
)

// Session is a long-lived backend session. Output holds terminal stdout/stderr for replay on resume.
// AttachCh is used to attach a new client (e.g. WebSocket); send a value to attach. Capacity is
// large enough to absorb a small burst of viewers joining a single owner.
type Session struct {
	id        ID
	createdAt time.Time
	// lastSeenMu guards lastSeen: Touch() writes it from the I/O pump
	// goroutine while LastSeen() is read concurrently from HTTP
	// handlers building session-list responses, outside any Manager
	// lock. time.Time is not safe for concurrent read/write without
	// this (a torn read can otherwise surface a garbage timestamp).
	lastSeenMu sync.RWMutex
	lastSeen   time.Time
	// keep marks a session the owner (or an admin) deliberately left
	// running, so IsIdle never flags it as abandoned.
	keep bool

	UserID      string
	TargetID    string
	TargetName  string
	Name        string // optional human-readable session name (for identification).
	Description string // optional free-form description.

	Output   *RingBuffer    // optional; set when Start creates the session for terminal replay
	AttachCh chan AttachReq // for resume: send client connection to re-attach
	cancel   context.CancelFunc
	done     chan struct{}
}

// AttachReq carries a client connection to attach to an existing
// session. Conn is intentionally interface{} so internal/session does
// not need to depend on websocket.Conn or sshproxy.StreamAttach;
// bridge implementations type-assert before use. UserID and Username
// describe the joining identity (used by the bridge layer for
// per-client metadata such as "who currently holds stdin"). Mode
// distinguishes writer from viewer; the zero value is writer for
// backwards compatibility.
type AttachReq struct {
	Conn     interface{}
	UserID   string
	Username string
	Mode     AttachMode
}

// NewManager creates a new Manager.
func NewManager() *Manager {
	return &Manager{
		sessions:    make(map[ID]*Session),
		now:         time.Now,
		stopTimeout: 8 * time.Second,
	}
}

// Start creates a new session and starts its goroutine. The callback receives the session
// so it can use sess.Output (RingBuffer) for terminal replay when present.
func (m *Manager) Start(id ID, opts StartOptions, fn func(ctx context.Context, sess *Session)) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return nil, ErrSessionExists
	}

	// #nosec G118 -- cancel is stored on Session and invoked via Manager.Stop
	ctx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		id:          id,
		createdAt:   m.now(),
		lastSeen:    m.now(),
		UserID:      opts.UserID,
		TargetID:    opts.TargetID,
		TargetName:  opts.TargetName,
		Name:        opts.Name,
		Description: opts.Description,
		Output:      NewRingBuffer(DefaultRingBufferSize),
		AttachCh:    make(chan AttachReq, attachChannelBuffer),
		cancel:      cancel,
		done:        make(chan struct{}),
	}

	m.sessions[id] = sess

	go func() {
		defer close(sess.done)
		fn(ctx, sess)

		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
	}()

	return sess, nil
}

// Get returns the session by ID if it exists. Caller must not modify the session.
func (m *Manager) Get(id ID) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[id]
	return sess, ok
}

// ID returns the session identifier.
func (s *Session) ID() ID { return s.id }

// Done returns a channel that is closed when the session goroutine has finished.
func (s *Session) Done() <-chan struct{} { return s.done }

// CreatedAt returns when the session was created.
func (s *Session) CreatedAt() time.Time { return s.createdAt }

// LastSeen returns the last activity timestamp (updated by Touch on client or remote I/O).
func (s *Session) LastSeen() time.Time {
	s.lastSeenMu.RLock()
	defer s.lastSeenMu.RUnlock()
	return s.lastSeen
}

// Keep reports whether the session is pinned as intentionally running.
func (s *Session) Keep() bool {
	s.lastSeenMu.RLock()
	defer s.lastSeenMu.RUnlock()
	return s.keep
}

// SetIdleWarnAfter sets how long without Touch before IsIdle returns true. Zero disables idle detection.
func (m *Manager) SetIdleWarnAfter(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idleWarnAfter = d
}

// IdleWarnAfter returns the configured idle warning threshold.
func (m *Manager) IdleWarnAfter() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.idleWarnAfter
}

// IdleDuration returns time since last activity for the session.
func (m *Manager) IdleDuration(sess *Session) time.Duration {
	if sess == nil {
		return 0
	}
	return m.now().Sub(sess.LastSeen())
}

// SetKeep pins (or unpins) a session as intentionally left running.
// Returns false when the session does not exist.
func (m *Manager) SetKeep(id ID, keep bool) bool {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	s.lastSeenMu.Lock()
	s.keep = keep
	s.lastSeenMu.Unlock()
	return true
}

// IsIdle reports whether the session has exceeded the idle warning
// threshold. A session pinned via SetKeep is never reported idle: its
// owner deliberately left it running.
func (m *Manager) IsIdle(sess *Session) bool {
	if sess == nil || sess.Keep() {
		return false
	}
	m.mu.RLock()
	threshold := m.idleWarnAfter
	m.mu.RUnlock()
	if threshold <= 0 {
		return false
	}
	return m.IdleDuration(sess) >= threshold
}

// Touch updates the lastSeen timestamp for the session.
func (m *Manager) Touch(id ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[id]; ok {
		s.lastSeenMu.Lock()
		s.lastSeen = m.now()
		s.lastSeenMu.Unlock()
	}
}

// Stop cancels the session goroutine and waits for it to finish.
// If the bridge does not exit within stopTimeout, the session is removed
// from the map anyway. The bridge implementations backing real sessions
// (sshproxy/telnetproxy) already close their underlying connection as
// soon as ctx is cancelled specifically so this path isn't normally hit;
// stopTimeout firing means the goroutine's blocking call didn't react to
// that -- e.g. cleanup() itself hung -- and its resources (PTY, socket,
// stdin/stdout pumps) leak for as long as the process runs, since
// nothing else holds a reference to force them closed a second time.
// Reports false in that case so callers can audit/alert on it instead of
// this staying silent, which is the best this generic layer can do
// without every bridge plumbing through its own force-close hook.
func (m *Manager) Stop(id ID) bool {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return true
	}

	sess.cancel()
	stopTimeout := m.stopTimeout
	if stopTimeout <= 0 {
		stopTimeout = 8 * time.Second
	}
	select {
	case <-sess.done:
		return true
	case <-time.After(stopTimeout):
		logger.Warn("session goroutine did not exit within stop timeout; removing anyway, resources may leak",
			"session_id", string(id), "timeout", stopTimeout)
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return false
	}
}

// ActiveIDs returns the list of active session IDs.
func (m *Manager) ActiveIDs() []ID {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]ID, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// ActiveSessionsForUser returns active sessions owned by userID (snapshot).
func (m *Manager) ActiveSessionsForUser(userID string) []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0)
	for _, s := range m.sessions {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out
}
