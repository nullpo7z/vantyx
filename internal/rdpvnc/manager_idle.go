package rdpvnc

import "time"

// LastSeen returns the last activity timestamp (updated by Touch on VNC I/O).
func (s *Session) LastSeen() time.Time {
	if s.lastSeenMu == nil {
		return s.lastSeen
	}
	s.lastSeenMu.RLock()
	defer s.lastSeenMu.RUnlock()
	return s.lastSeen
}

// SetIdleWarnAfter sets how long without Touch before IsIdle returns true. Zero disables idle detection.
func (m *Manager) SetIdleWarnAfter(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idleWarnAfter = d
}

// IdleWarnAfter returns the configured idle warning threshold.
func (m *Manager) IdleWarnAfter() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idleWarnAfter
}

// IdleDuration returns time since last activity for the session.
func (m *Manager) IdleDuration(sess *Session) time.Duration {
	if sess == nil {
		return 0
	}
	m.mu.Lock()
	nowFn := m.now
	m.mu.Unlock()
	return nowFn().Sub(sess.LastSeen())
}

// SetKeep pins (or unpins) a session as intentionally left running.
// Returns false when the session is unknown.
func (m *Manager) SetKeep(sessionID string, keep bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessionsByID[sessionID]; !ok {
		return false
	}
	if keep {
		m.keep[sessionID] = true
	} else {
		delete(m.keep, sessionID)
	}
	return true
}

// IsKept reports whether the session is pinned.
func (m *Manager) IsKept(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keep[sessionID]
}

// IsIdle reports whether the session has exceeded the idle warning
// threshold. A pinned session (SetKeep) is never reported idle.
func (m *Manager) IsIdle(sess *Session) bool {
	if sess == nil {
		return false
	}
	m.mu.Lock()
	threshold := m.idleWarnAfter
	pinned := m.keep[sess.ID]
	m.mu.Unlock()
	if threshold <= 0 || pinned {
		return false
	}
	return m.IdleDuration(sess) >= threshold
}

// SetNowForTest overrides the clock used by the manager (tests only).
func (m *Manager) SetNowForTest(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn == nil {
		m.now = time.Now
		return
	}
	m.now = fn
}

// Touch updates the lastSeen timestamp for the session.
func (m *Manager) Touch(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessionsByID[sessionID]; ok {
		now := m.now()
		if s.lastSeenMu != nil {
			s.lastSeenMu.Lock()
			s.lastSeen = now
			s.lastSeenMu.Unlock()
		} else {
			s.lastSeen = now
		}
	}
}
