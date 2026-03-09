package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/rdpproxy"
)

// handleRDPWebSocket upgrades to WebSocket and bridges the client to the target's RDP server (port 3389 over TCP).
// Query: target_id (required). Session cookie required. Target must have protocol "rdp".
func (a *App) handleRDPWebSocket(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("rdp ws bad_request user_id=%s err=target_id required", sess.UserID)
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err != nil {
		log.Printf("rdp ws not_found user_id=%s target_id=%s", sess.UserID, targetID)
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
		log.Printf("rdp ws forbidden user_id=%s target_id=%s", sess.UserID, targetID)
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	if target.Protocol != access.ProtocolRDP {
		log.Printf("rdp ws not_rdp user_id=%s target_id=%s protocol=%s", sess.UserID, targetID, target.Protocol)
		writeJSONError(w, "target is not an RDP server", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("rdp ws upgrade_failed user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
		writeJSONError(w, "failed to upgrade connection", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	targetAddr := target.Host + ":" + strconv.Itoa(int(target.Port))
	log.Printf("rdp ws start user_id=%s target_id=%s addr=%s", sess.UserID, targetID, targetAddr)
	_ = rdpproxy.Bridge(conn, targetAddr)
}

// handleRDPFile generates and serves a .rdp connection file for the target.
// Query: target_id (required). Session cookie required. Target must have protocol "rdp".
func (a *App) handleRDPFile(w http.ResponseWriter, r *http.Request) {
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
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err != nil {
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
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}

	if target.Protocol != access.ProtocolRDP {
		writeJSONError(w, "target is not an RDP server", http.StatusBadRequest)
		return
	}

	port := int(target.Port)
	if port == 0 {
		port = 3389
	}

	rdpContent := fmt.Sprintf("full address:s:%s:%d\r\n", target.Host, port)
	if target.SSHUsername != "" {
		rdpContent += fmt.Sprintf("username:s:%s\r\n", target.SSHUsername)
	}
	rdpContent += "prompt for credentials on client:i:1\r\n"
	rdpContent += "screen mode id:i:2\r\n"
	rdpContent += "desktopwidth:i:1920\r\n"
	rdpContent += "desktopheight:i:1080\r\n"
	rdpContent += "session bpp:i:32\r\n"
	rdpContent += "compression:i:1\r\n"
	rdpContent += "displayconnectionbar:i:1\r\n"
	rdpContent += "autoreconnection enabled:i:1\r\n"

	w.Header().Set("Content-Type", "application/x-rdp")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.rdp"`, target.Name))
	_, _ = w.Write([]byte(rdpContent)) // #nosec G705 -- content is built from validated target fields, not user-supplied taint
}
