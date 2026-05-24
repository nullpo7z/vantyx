package session

import (
	"context"
	"sync"
	"time"
)

// ID represents a logical session identifier.
type ID string

// StartOptions holds metadata for a new terminal session (used for attach auth and listing).
type StartOptions struct {
	UserID      string
	TargetID    string
	TargetName  string
	Name        string // セッション名（識別用）
	Description string // 説明（任意）
}

// Manager manages long-lived sessions backed by goroutines.
type Manager struct {
	mu            sync.RWMutex
	sessions      map[ID]*Session
	now           func() time.Time
	idleWarnAfter time.Duration // 0 = idle warnings disabled
}

// Session is a long-lived backend session. Output holds terminal stdout/stderr for replay on resume.
// AttachCh is used to attach a new client (e.g. WebSocket) for resume; send a value to attach, buffer size 1.
type Session struct {
	id        ID
	createdAt time.Time
	lastSeen  time.Time

	UserID      string
	TargetID    string
	TargetName  string
	Name        string // セッション名（識別用）
	Description string // 説明（任意）

	Output   *RingBuffer    // optional; set when Start creates the session for terminal replay
	AttachCh chan AttachReq // for resume: send client connection to re-attach
	cancel   context.CancelFunc
	done     chan struct{}
}

// AttachReq carries a WebSocket connection to attach to an existing session.
// The type is interface{} so that httpapi does not need to depend on websocket.Conn in session package.
// The handler should send AttachReq{Conn: conn} and the bridge loop will type-assert to *websocket.Conn.
type AttachReq struct {
	Conn interface{}
}

// NewManager creates a new Manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[ID]*Session),
		now:      time.Now,
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
		AttachCh:    make(chan AttachReq, 1),
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
func (s *Session) LastSeen() time.Time { return s.lastSeen }

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

// IsIdle reports whether the session has exceeded the idle warning threshold.
func (m *Manager) IsIdle(sess *Session) bool {
	m.mu.RLock()
	threshold := m.idleWarnAfter
	m.mu.RUnlock()
	if threshold <= 0 || sess == nil {
		return false
	}
	return m.IdleDuration(sess) >= threshold
}

// Touch updates the lastSeen timestamp for the session.
func (m *Manager) Touch(id ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[id]; ok {
		s.lastSeen = m.now()
	}
}

// Stop cancels the session goroutine and waits for it to finish.
// If the bridge does not exit within stopTimeout, the session is removed from the map anyway.
func (m *Manager) Stop(id ID) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return
	}

	sess.cancel()
	const stopTimeout = 8 * time.Second
	select {
	case <-sess.done:
	case <-time.After(stopTimeout):
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
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
