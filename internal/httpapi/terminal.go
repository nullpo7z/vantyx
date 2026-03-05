package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/session"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Origin checks will be refined when frontend is introduced.
		return true
	},
}

// handleSSHWebSocket upgrades the connection and starts a goroutine-backed terminal session.
// Requires query parameter target_id; the user must have access to that target.
// For Phase 2, the session still behaves as an echo server; actual SSH bridging follows.
func (a *App) handleSSHWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		http.Error(w, "target_id required", http.StatusBadRequest)
		return
	}

	_, err = a.TargetStore.Get(targetID)
	if err != nil {
		http.Error(w, "target not found", http.StatusNotFound)
		return
	}

	allowed := a.AccessGroupStore.TargetIDsForUser(sess.UserID)
	allowedSet := make(map[string]struct{})
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	if _, ok := allowedSet[targetID]; !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade connection", http.StatusBadRequest)
		return
	}

	id := session.ID(time.Now().UTC().Format(time.RFC3339Nano))

	_, err = a.TerminalSessionManager.Start(id, func(ctx context.Context) {
		defer conn.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				mt, msg, readErr := conn.ReadMessage()
				if readErr != nil {
					return
				}
				a.TerminalSessionManager.Touch(id)
				if writeErr := conn.WriteMessage(mt, msg); writeErr != nil {
					return
				}
			}
		}
	})
	if err != nil {
		_ = conn.Close()
		http.Error(w, "failed to start terminal session", http.StatusInternalServerError)
		return
	}
}
