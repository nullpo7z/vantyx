package httpapi

import (
	"log"
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
		// #nosec G706 -- audit log; sess.UserID from session store
		log.Printf("vnc ws bad_request user_id=%s err=target_id required", sess.UserID)
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err != nil {
		// #nosec G706 -- audit log; IDs from store/query
		log.Printf("vnc ws not_found user_id=%s target_id=%s", sess.UserID, targetID)
		writeJSONError(w, "target not found", http.StatusNotFound)
		return
	}

	allowed, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowedSet := make(map[access.TargetID]struct{})
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	if _, ok := allowedSet[access.TargetID(targetID)]; !ok {
		// #nosec G706 -- audit log; IDs from store/query
		log.Printf("vnc ws forbidden user_id=%s target_id=%s", sess.UserID, targetID)
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	if target.Protocol != access.ProtocolVNC {
		// #nosec G706 -- audit log; target from store
		log.Printf("vnc ws not_vnc user_id=%s target_id=%s protocol=%s", sess.UserID, targetID, target.Protocol)
		writeJSONError(w, "target is not a VNC server", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// #nosec G706 -- audit log; err from upgrader
		log.Printf("vnc ws upgrade_failed user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
		writeJSONError(w, "failed to upgrade connection", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	targetAddr := target.Host + ":" + strconv.Itoa(int(target.Port))
	// #nosec G706 -- audit log; target from store
	log.Printf("vnc ws start user_id=%s target_id=%s addr=%s", sess.UserID, targetID, targetAddr)
	_ = vncproxy.Bridge(conn, targetAddr)
}
