package httpapi

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// terminalSessionIDGen is set in tests to force duplicate session ID and cover Start error path.
var terminalSessionIDGen func() session.ID

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Origin checks will be refined when frontend is introduced.
		return true
	},
}

// terminalSessionStarter is satisfied by *session.Manager; allows tests to inject a stub that returns error from Start.
type terminalSessionStarter interface {
	Start(id session.ID, fn func(context.Context, *session.Session)) (*session.Session, error)
	Touch(id session.ID)
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

	target, err := a.TargetStore.Get(targetID)
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

	if target.Protocol != access.ProtocolSSH {
		http.Error(w, "only SSH targets supported", http.StatusNotImplemented)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "failed to upgrade connection", http.StatusBadRequest)
		return
	}

	creds, err := sshproxy.ReadCredentials(conn, 15*time.Second)
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: invalid or missing credentials (send JSON: {\"username\":\"...\",\"password\":\"...\"})"))
		_ = conn.Close()
		return
	}

	// terminalSessionIDGen is overridden in tests to trigger Start id-collision error path.
	var id session.ID
	if terminalSessionIDGen != nil {
		id = terminalSessionIDGen()
	} else {
		id = session.ID(time.Now().UTC().Format(time.RFC3339Nano))
	}

	_, err = a.TerminalSessionManager.Start(id, func(ctx context.Context, sess *session.Session) {
		defer conn.Close()
		touch := func() { a.TerminalSessionManager.Touch(id) }
		var tee io.Writer
		if sess.Output != nil {
			tee = sess.Output
		}
		_ = sshproxy.RunBridge(ctx, conn, target.Host, target.Port, creds.Username, creds.Password, touch, tee)
	})
	if err != nil {
		_ = conn.Close()
		http.Error(w, "failed to start terminal session", http.StatusInternalServerError)
		return
	}
}
