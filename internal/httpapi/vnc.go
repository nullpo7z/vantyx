package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
)

func newVNCSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleVNCWebSocket bridges noVNC to a target VNC server or an existing shared session.
// Query: target_id (new session) or session_id (attach; mode=writer|viewer).
func (a *App) handleVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	userID := sess.UserID

	sessionIDParam := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionIDParam != "" {
		a.handleVNCAttach(w, r, userID, sessionIDParam)
		return
	}

	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		writeJSONErrorKey(w, r, "common.targetIDRequired", http.StatusBadRequest)
		return
	}
	_, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}
	if target.Protocol != access.ProtocolVNC {
		writeJSONErrorKey(w, r, "vnc.notVNC", http.StatusBadRequest)
		return
	}
	if a.VNCSessionManager == nil {
		writeJSONErrorKey(w, r, "vnc.unavailable", http.StatusServiceUnavailable)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		writeJSONErrorKey(w, r, "common.failedUpgradeConnection", http.StatusBadRequest)
		return
	}

	idStr, err := newVNCSessionID()
	if err != nil {
		_ = conn.Close()
		return
	}
	id := session.ID(idStr)
	targetAddr := target.Host + ":" + strconv.Itoa(int(target.Port))

	a.startVNCVideoRecording(r.Context(), idStr, userID, targetID, target.Host, int(target.Port), target.SSHPassword)

	_, err = a.VNCSessionManager.Start(id, session.StartOptions{
		UserID: userID, TargetID: targetID, TargetName: target.Name,
	}, func(ctx context.Context, vncSess *session.Session) {
		defer a.finishVideoRecording(idStr)
		ownerAttach := session.AttachReq{Conn: conn, UserID: userID, Mode: session.AttachModeWriter}
		a.runDetachableVNCBridge(ctx, vncSess, id, targetAddr, ownerAttach)
		if a.SessionEventBroker != nil {
			a.SessionEventBroker.Broadcast()
		}
	})
	if err != nil {
		_ = conn.Close()
		a.finishVideoRecording(idStr)
		http.Error(w, "failed to start vnc session", http.StatusInternalServerError)
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"session_meta","session_id":"`+idStr+`"}`))
}

func (a *App) handleVNCAttach(w http.ResponseWriter, r *http.Request, userID, sessionIDParam string) {
	if a.VNCSessionManager == nil {
		writeJSONErrorKey(w, r, "vnc.unavailable", http.StatusServiceUnavailable)
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	viewer := mode == "viewer"
	vncSess, ok := a.VNCSessionManager.Get(session.ID(sessionIDParam))
	if !ok {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	room, _ := a.SharingRegistry.Get(sessionIDParam)
	if viewer {
		if room == nil || !room.IsParticipant(userID) {
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return
		}
	} else if vncSess.UserID != userID && (room == nil || room.WriterID() != userID) {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	if ok, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(vncSess.TargetID)); err != nil {
		writeInternalError(w, err)
		return
	} else if !ok {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		writeJSONErrorKey(w, r, "common.failedUpgradeConnection", http.StatusBadRequest)
		return
	}
	attachMode := session.AttachModeWriter
	if viewer {
		attachMode = session.AttachModeViewer
	}
	username := a.usernameFor(r.Context(), userID)
	select {
	case vncSess.AttachCh <- session.AttachReq{Conn: conn, UserID: userID, Username: username, Mode: attachMode}:
		if !viewer && a.SharingBridges != nil {
			if controller, ok := a.SharingBridges.Get(vncSess.ID()); ok {
				controller.SetWriter(roomWriterID(room))
			}
		}
	default:
		_ = conn.Close()
		writeJSONErrorKey(w, r, "sessions.attachBusy", http.StatusServiceUnavailable)
	}
}
