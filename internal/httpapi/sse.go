package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nullpo7z/vantyx/internal/filetransfer"
)

// SessionEventBroker broadcasts session lifecycle events (terminal/RDP created or removed)
// to SSE clients so the frontend can update active session counts without polling.
//
// Each subscriber is associated with a userID so the broker can also
// fan out collaborative-session events (participant_joined,
// write_token_transferred, ...) only to clients that should care
// about them. Routing is done via the optional ToUsers field on
// [sessionEventEnvelope]; an empty list reverts to global broadcast
// for backwards compatibility.
type SessionEventBroker struct {
	mu      sync.RWMutex
	clients map[chan []byte]string // channel -> userID ("" for legacy callers)
}

// NewSessionEventBroker creates a new broker. Call Run() to start the fan-out goroutine.
func NewSessionEventBroker() *SessionEventBroker {
	return &SessionEventBroker{clients: make(map[chan []byte]string)}
}

// Subscribe adds a client channel for the legacy global broadcast
// stream. Existing tests rely on this signature.
func (b *SessionEventBroker) Subscribe() chan []byte {
	return b.SubscribeFor("")
}

// SubscribeFor adds a client channel scoped to userID. Empty userID
// means "no filtering"; the channel receives every event.
func (b *SessionEventBroker) SubscribeFor(userID string) chan []byte {
	ch := make(chan []byte, 64)
	b.mu.Lock()
	b.clients[ch] = userID
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

// PublishToUsers sends payload only to subscribers whose userID
// appears in users. The payload is delivered as the JSON body of an
// SSE message frame; the broker does no further marshalling. Callers
// generally use [sharing.Event] before invoking this.
func (b *SessionEventBroker) PublishToUsers(payload []byte, users ...string) {
	if len(users) == 0 || len(payload) == 0 {
		return
	}
	wanted := make(map[string]struct{}, len(users))
	for _, u := range users {
		if u != "" {
			wanted[u] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch, uid := range b.clients {
		if uid == "" {
			continue
		}
		if _, ok := wanted[uid]; !ok {
			continue
		}
		select {
		case ch <- payload:
		default:
		}
	}
}

// handleSessionEvents streams session lifecycle events over SSE. Requires auth.
// Frontend connects with EventSource; backend calls SessionEventBroker.Broadcast() when
// terminal or RDP sessions are created or removed.
func (a *App) handleSessionEvents(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.SessionEventBroker == nil {
		writeJSONErrorKey(w, r, "sessions.eventsUnavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch := a.SessionEventBroker.SubscribeFor(userID)
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

// fileTransferProgressThrottle limits how often progress-only updates are forwarded
// to subscribers (per-job). Terminal state changes always pass through immediately.
const fileTransferProgressThrottle = 100 * time.Millisecond

// FileTransferEventBroker streams per-user job updates over SSE.
// Each client channel is associated with a single userID and receives only that user's events.
type FileTransferEventBroker struct {
	mu      sync.RWMutex
	clients map[chan []byte]string

	// per-job progress throttle: jobID -> last emit time
	tmu        sync.Mutex
	lastEmit   map[string]time.Time
	lastStates map[string]string
}

// NewFileTransferEventBroker creates a new broker.
func NewFileTransferEventBroker() *FileTransferEventBroker {
	return &FileTransferEventBroker{
		clients:    make(map[chan []byte]string),
		lastEmit:   make(map[string]time.Time),
		lastStates: make(map[string]string),
	}
}

// Subscribe registers a subscriber for events belonging to userID.
func (b *FileTransferEventBroker) Subscribe(userID string) chan []byte {
	ch := make(chan []byte, 32)
	b.mu.Lock()
	b.clients[ch] = userID
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the channel and closes it.
func (b *FileTransferEventBroker) Unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// Publish accepts a job snapshot and forwards it to subscribers belonging to userID,
// throttling pure-progress updates per job.
func (b *FileTransferEventBroker) Publish(snap filetransfer.JobSnapshot, userID string) {
	if !b.shouldEmit(snap) {
		return
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch, uid := range b.clients {
		if uid != userID {
			continue
		}
		select {
		case ch <- payload:
		default:
			// subscriber is slow; drop this event rather than block
		}
	}
}

func (b *FileTransferEventBroker) shouldEmit(snap filetransfer.JobSnapshot) bool {
	b.tmu.Lock()
	defer b.tmu.Unlock()
	prev := b.lastStates[snap.ID]
	if prev != snap.State {
		b.lastStates[snap.ID] = snap.State
		b.lastEmit[snap.ID] = time.Now()
		return true
	}
	last, ok := b.lastEmit[snap.ID]
	now := time.Now()
	if !ok || now.Sub(last) >= fileTransferProgressThrottle {
		b.lastEmit[snap.ID] = now
		return true
	}
	return false
}

// handleFileTransferEvents streams the current user's transfer updates over SSE.
func (a *App) handleFileTransferEvents(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.FileTransferEventBroker == nil {
		writeJSONErrorKey(w, r, "transfers.eventsUnavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch := a.FileTransferEventBroker.Subscribe(userID)
	defer a.FileTransferEventBroker.Unsubscribe(ch)

	if a.FileTransferManager != nil {
		for _, j := range a.FileTransferManager.ListByUser(userID) {
			snap := j.Snapshot()
			payload, err := json.Marshal(snap)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
				return
			}
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
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
