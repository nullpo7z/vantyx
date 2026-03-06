package httpapi

import (
	"context"
	"io"
	"log"
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
		log.Printf("terminal ws unauthorized err=no session cookie")
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		log.Printf("terminal ws unauthorized err=invalid session")
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		// #nosec G706 -- audit log; sess.UserID from session store
		log.Printf("terminal ws bad_request user_id=%s err=target_id required", sess.UserID)
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err != nil {
		// #nosec G706 -- audit log; IDs from store/query
		log.Printf("terminal ws not_found user_id=%s target_id=%s", sess.UserID, targetID)
		writeJSONError(w, "target not found", http.StatusNotFound)
		return
	}

	allowed, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allowedSet := make(map[access.TargetID]struct{})
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	if _, ok := allowedSet[access.TargetID(targetID)]; !ok {
		// #nosec G706 -- audit log; IDs from store/query
		log.Printf("terminal ws forbidden user_id=%s target_id=%s", sess.UserID, targetID)
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	if target.Protocol != access.ProtocolSSH {
		// #nosec G706 -- audit log; target from store
		log.Printf("terminal ws not_implemented user_id=%s target_id=%s protocol=%s", sess.UserID, targetID, target.Protocol)
		writeJSONError(w, "only SSH targets supported", http.StatusNotImplemented)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// #nosec G706 -- audit log; err from upgrader
		log.Printf("terminal ws upgrade_failed user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
		writeJSONError(w, "failed to upgrade connection", http.StatusBadRequest)
		return
	}

	creds, err := sshproxy.ReadCredentials(conn, 15*time.Second)
	if err != nil {
		// #nosec G706 -- audit log; err from ReadCredentials
		log.Printf("terminal ws credentials_invalid user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: invalid or missing credentials (send JSON: {\"username\":\"...\",\"password\":\"...\"})"))
		_ = conn.Close()
		return
	}
	// #nosec G706 -- audit log; creds from ReadCredentials
	log.Printf("terminal ws credentials_ok user_id=%s target_id=%s ssh_user=%q", sess.UserID, targetID, creds.Username)

	// terminalSessionIDGen is overridden in tests to trigger Start id-collision error path.
	var id session.ID
	if terminalSessionIDGen != nil {
		id = terminalSessionIDGen()
	} else {
		id = session.ID(time.Now().UTC().Format(time.RFC3339Nano))
	}

	userID := sess.UserID
	_, err = a.TerminalSessionManager.Start(id, func(ctx context.Context, termSess *session.Session) {
		defer conn.Close()
		defer log.Printf("terminal session end session_id=%s user_id=%s target_id=%s", id, userID, targetID)
		// Send an initial message so the frontend switches from credential form to terminal view.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(""))
		touch := func() { a.TerminalSessionManager.Touch(id) }
		var tee io.Writer
		if termSess.Output != nil {
			tee = termSess.Output
		}
		if bridgeErr := sshproxy.RunBridge(ctx, conn, target.Host, target.Port, creds.Username, creds.Password, touch, tee); bridgeErr != nil {
			// クライアントにエラーを表示させる（接続が閉じられました だけだと原因が分からない）
			_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+bridgeErr.Error()))
		}
	})
	if err != nil {
		// #nosec G706 -- audit log; err from Start
		log.Printf("terminal session start_failed user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
		_ = conn.Close()
		http.Error(w, "failed to start terminal session", http.StatusInternalServerError)
		return
	}
	// #nosec G706 -- audit log; target from store
	log.Printf("terminal session start session_id=%s user_id=%s target_id=%s host=%s port=%d",
		id, sess.UserID, targetID, target.Host, target.Port)
}
