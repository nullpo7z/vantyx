package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// SessionEventBroker broadcasts session lifecycle events (terminal/RDP created or removed)
// to SSE clients so the frontend can update active session counts without polling.
type SessionEventBroker struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

// NewSessionEventBroker creates a new broker. Call Run() to start the fan-out goroutine.
func NewSessionEventBroker() *SessionEventBroker {
	return &SessionEventBroker{clients: make(map[chan []byte]struct{})}
}

// Subscribe adds a client channel and returns it. The caller must call Unsubscribe when done.
func (b *SessionEventBroker) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the client channel and closes it.
func (b *SessionEventBroker) Unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// Broadcast sends a session_change event to all subscribed clients (non-blocking).
func (b *SessionEventBroker) Broadcast() {
	msg, _ := json.Marshal(map[string]string{"type": "session_change"})
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
			// client slow; skip this event
		}
	}
}

// handleSessionEvents streams session lifecycle events over SSE. Requires auth.
// Frontend connects with EventSource; backend calls SessionEventBroker.Broadcast() when
// terminal or RDP sessions are created or removed.
func (a *App) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if a.SessionEventBroker == nil {
		writeJSONError(w, "session events not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch := a.SessionEventBroker.Subscribe()
	defer a.SessionEventBroker.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write([]byte("data: " + string(msg) + "\n\n")); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}
