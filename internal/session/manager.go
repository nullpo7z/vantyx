package session

import (
	"context"
	"sync"
	"time"
)

// ID represents a logical session identifier.
type ID string

// Manager manages long-lived sessions backed by goroutines.
type Manager struct {
	mu       sync.RWMutex
	sessions map[ID]*Session
	now      func() time.Time
}

// Session is a long-lived backend session. Output holds terminal stdout/stderr for replay on resume.
type Session struct {
	id        ID
	createdAt time.Time
	lastSeen  time.Time

	Output *RingBuffer // optional; set when Start creates the session for terminal replay
	cancel context.CancelFunc
	done   chan struct{}
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
func (m *Manager) Start(id ID, fn func(ctx context.Context, sess *Session)) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return nil, ErrSessionExists
	}

	// #nosec G118 -- cancel is stored on Session and invoked via Manager.Stop
	ctx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		id:        id,
		createdAt: m.now(),
		lastSeen:  m.now(),
		Output:    NewRingBuffer(DefaultRingBufferSize),
		cancel:    cancel,
		done:      make(chan struct{}),
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

// Touch updates the lastSeen timestamp for the session.
func (m *Manager) Touch(id ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[id]; ok {
		s.lastSeen = m.now()
	}
}

// Stop cancels the session goroutine and waits for it to finish.
func (m *Manager) Stop(id ID) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return
	}

	sess.cancel()
	<-sess.done
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
