package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// listTerminalSessions is satisfied by *session.Manager for GET /api/terminal/sessions.
type listTerminalSessions interface {
	ActiveIDs() []session.ID
}

// terminalSessionIDGen is set in tests to force duplicate session ID and cover Start error path.
var terminalSessionIDGen func() session.ID

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Origin checks will be refined when frontend is introduced.
		return true
	},
}

// terminalSessionStarter is satisfied by *session.Manager; allows tests to inject a stub.
type terminalSessionStarter interface {
	Start(id session.ID, opts session.StartOptions, fn func(context.Context, *session.Session)) (*session.Session, error)
	Get(id session.ID) (*session.Session, bool)
	Touch(id session.ID)
	Stop(id session.ID)
}

// wsAuthMessage is the first WebSocket message for new SSH connection (credentials or use_stored_credentials).
type wsAuthMessage struct {
	UseStoredCredentials bool   `json:"use_stored_credentials"`
	Username             string `json:"username"`
	Password             string `json:"password"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
}

// readTerminalCredentials reads the first text message and returns credentials.
// If use_stored_credentials is true, uses target's stored SSH username/password (no client secret).
func readTerminalCredentials(conn *websocket.Conn, target *access.Target) (sshproxy.Credentials, error) {
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	mt, msg, err := conn.ReadMessage()
	if err != nil {
		return sshproxy.Credentials{}, err
	}
	if mt != websocket.TextMessage {
		return sshproxy.Credentials{}, errInvalidCredentials
	}
	var m wsAuthMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		return sshproxy.Credentials{}, errInvalidCredentials
	}
	if m.UseStoredCredentials {
		if target.SSHUsername == "" || target.SSHPassword == "" {
			return sshproxy.Credentials{}, errNoStoredCredentials
		}
		return sshproxy.Credentials{
			Username:    target.SSHUsername,
			Password:    target.SSHPassword,
			Name:        m.Name,
			Description: m.Description,
		}, nil
	}
	if m.Username == "" {
		return sshproxy.Credentials{}, errInvalidCredentials
	}
	return sshproxy.Credentials{
		Username:    m.Username,
		Password:    m.Password,
		Name:        m.Name,
		Description: m.Description,
	}, nil
}

var (
	errInvalidCredentials  = errors.New("invalid or missing credentials (send JSON: {\"username\":\"...\",\"password\":\"...\"} or {\"use_stored_credentials\":true})")
	errNoStoredCredentials  = errors.New("stored credentials not configured for this target")
)

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

	// Resume (attach) to existing session: session_id in query, no target_id or credentials.
	if sessionIDParam := r.URL.Query().Get("session_id"); sessionIDParam != "" {
		termSess, ok := a.TerminalSessionManager.Get(session.ID(sessionIDParam))
		if !ok || termSess.UserID != sess.UserID {
			writeJSONError(w, "session not found or access denied", http.StatusNotFound)
			return
		}
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			// #nosec G706 -- audit log; sessionIDParam from query, err from upgrader
			log.Printf("terminal ws upgrade_failed attach session_id=%s err=%v", sessionIDParam, err)
			writeJSONError(w, "failed to upgrade connection", http.StatusBadRequest)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(""))
		select {
		case termSess.AttachCh <- session.AttachReq{Conn: conn}:
			// #nosec G706 -- audit log; IDs from session store
			log.Printf("terminal session attach session_id=%s user_id=%s", sessionIDParam, sess.UserID)
		default:
			_ = conn.WriteMessage(websocket.TextMessage, []byte("error: session attach slot busy"))
			_ = conn.Close()
		}
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

	creds, err := readTerminalCredentials(conn, target)
	if err != nil {
		// #nosec G706 -- audit log; err from readTerminalCredentials
		log.Printf("terminal ws credentials_invalid user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		_ = conn.Close()
		return
	}
	// #nosec G706 -- audit log; creds from readTerminalCredentials
	log.Printf("terminal ws credentials_ok user_id=%s target_id=%s ssh_user=%q", sess.UserID, targetID, creds.Username)

	// terminalSessionIDGen is overridden in tests to trigger Start id-collision error path.
	var id session.ID
	if terminalSessionIDGen != nil {
		id = terminalSessionIDGen()
	} else {
		id = session.ID(time.Now().UTC().Format(time.RFC3339Nano))
	}

	userID := sess.UserID
	opts := session.StartOptions{
		UserID:      userID,
		TargetID:    targetID,
		TargetName:  target.Name,
		Name:        creds.Name,
		Description: creds.Description,
	}
	_, err = a.TerminalSessionManager.Start(id, opts, func(ctx context.Context, termSess *session.Session) {
		runDetachableBridge(ctx, termSess, a.TerminalSessionManager, id, conn, target, creds)
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

// TerminalSessionItem is one entry in GET /api/terminal/sessions response.
type TerminalSessionItem struct {
	SessionID   string    `json:"session_id"`
	TargetID    string    `json:"target_id"`
	TargetName  string    `json:"target_name"`
	Name        string    `json:"name,omitempty"`        // セッション名（識別用）
	Description string    `json:"description,omitempty"` // 説明
	CreatedAt   time.Time `json:"created_at"`
}

// handleTerminalSessions returns the list of active terminal sessions for the current user.
func (a *App) handleTerminalSessions(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	authSess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	lister, ok := a.TerminalSessionManager.(listTerminalSessions)
	if !ok {
		writeJSON(w, map[string]interface{}{"items": []TerminalSessionItem{}})
		return
	}
	ids := lister.ActiveIDs()
	items := make([]TerminalSessionItem, 0, len(ids))
	for _, id := range ids {
		sess, ok := a.TerminalSessionManager.Get(id)
		if !ok || sess.UserID != authSess.UserID {
			continue
		}
		items = append(items, TerminalSessionItem{
			SessionID:   string(sess.ID()),
			TargetID:    sess.TargetID,
			TargetName:  sess.TargetName,
			Name:        sess.Name,
			Description: sess.Description,
			CreatedAt:   sess.CreatedAt(),
		})
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleTerminalSessionDelete terminates the given terminal session. Caller must own the session.
func (a *App) handleTerminalSessionDelete(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	authSess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONError(w, "session_id required", http.StatusBadRequest)
		return
	}
	id := session.ID(sessionID)
	termSess, ok := a.TerminalSessionManager.Get(id)
	if !ok || termSess.UserID != authSess.UserID {
		writeJSONError(w, "session not found or access denied", http.StatusNotFound)
		return
	}
	a.TerminalSessionManager.Stop(id)
	// #nosec G706 -- audit log; sessionID from URL, authSess from store
	log.Printf("terminal session stopped session_id=%s user_id=%s", sessionID, authSess.UserID)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// runDetachableBridge starts the SSH bridge and keeps it running when the client detaches.
// Initial conn is attached first; further attaches (resume) come via termSess.AttachCh.
func runDetachableBridge(ctx context.Context, termSess *session.Session, manager terminalSessionStarter, id session.ID, conn *websocket.Conn, target *access.Target, creds sshproxy.Credentials) {
	_ = conn.WriteMessage(websocket.TextMessage, []byte(""))
	// Send session_id so the client can reconnect (resume) without credentials.
	if b, err := json.Marshal(struct{ SessionID string `json:"session_id"` }{SessionID: string(id)}); err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}
	touch := func() { manager.Touch(id) }
	if err := sshproxy.RunBridgeDetachable(ctx, target.Host, target.Port, creds.Username, creds.Password, termSess.Output, termSess.AttachCh, conn, touch); err != nil {
		log.Printf("terminal bridge ended session_id=%s err=%v", id, err)
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
	} else {
		log.Printf("terminal bridge ended session_id=%s (SSH session closed)", id)
		_ = conn.WriteMessage(websocket.TextMessage, []byte("session_ended: SSH session closed"))
	}
	_ = conn.Close()
	// サーバー側でセッションが終了したため、バックエンドのセッションも破棄（再接続不可にする）
	manager.Stop(id)
}
