package httpapi

import (
	"net/http"
	"strconv"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/vncproxy"
)

// handleVNCWebSocket upgrades to WebSocket and bridges the client to the target's VNC server (RFB over TCP).
// Query: target_id (required). Session cookie required. Target must have protocol "vnc".
func (a *App) handleVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		audit("vnc_ws_bad_request", auditFields{
			"user_id": sess.UserID,
			"reason":  "target_id_required",
		})
		writeJSONError(w, "target_id required", http.StatusBadRequest)
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
		writeJSONError(w, "target is not a VNC server", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		audit("vnc_ws_upgrade_failed", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		writeJSONError(w, "failed to upgrade connection", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	targetAddr := target.Host + ":" + strconv.Itoa(int(target.Port))
	audit("vnc_ws_start", auditFields{
		"user_id":   userID,
		"target_id": targetID,
		"addr":      targetAddr,
	})
	_ = vncproxy.Bridge(conn, targetAddr, nil)
}
