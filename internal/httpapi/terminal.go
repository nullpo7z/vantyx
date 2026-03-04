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
// For Phase 2 start, this behaves as a simple echo server over WebSocket, wired through
// the generic session.Manager for lifecycle management.
func (a *App) handleSSHWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := a.SessionStore.Get(cookie.Value); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
