package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/vncproxy"
)

func newVNCSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleVNCWebSocket upgrades to WebSocket and bridges the client to the target's VNC server (RFB over TCP).
// Query: target_id (required). Session cookie required. Target must have protocol "vnc".
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

	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		audit("vnc_ws_bad_request", auditFields{
			"user_id": sess.UserID,
			"reason":  "target_id_required",
		})
		writeJSONErrorKey(w, r, "common.targetIDRequired", http.StatusBadRequest)
		return
	}

	userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}

	if target.Protocol != access.ProtocolVNC {
		audit("vnc_ws_not_vnc", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"protocol":  target.Protocol,
		})
		writeJSONErrorKey(w, r, "vnc.notVNC", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		audit("vnc_ws_upgrade_failed", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "common.failedUpgradeConnection", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	targetAddr := target.Host + ":" + strconv.Itoa(int(target.Port))
	sessionID, err := newVNCSessionID()
	if err != nil {
		audit("vnc_ws_start_failed", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		return
	}

	a.startVNCVideoRecording(r.Context(), sessionID, userID, targetID, target.Host, int(target.Port), target.SSHPassword)
	defer a.finishVideoRecording(sessionID)

	audit("vnc_ws_start", auditFields{
		"user_id":    userID,
		"target_id":  targetID,
		"session_id": sessionID,
		"addr":       targetAddr,
	})
	_ = vncproxy.Bridge(conn, targetAddr, nil)
}
