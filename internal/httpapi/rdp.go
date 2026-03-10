package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/rdpproxy"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/vncproxy"
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

// RDPSessionItem is one entry in GET /api/rdp/sessions response.
type RDPSessionItem struct {
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
}

// handleRDPSessions returns the list of active RDP (browser) sessions for the current user.
func (a *App) handleRDPSessions(w http.ResponseWriter, r *http.Request) {
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
	if a.RDPVNCManager == nil {
		writeJSON(w, map[string]interface{}{"items": []RDPSessionItem{}})
		return
	}
	ctx := r.Context()
	allowed, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowedSet := make(map[access.TargetID]struct{})
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	activeIDs := a.RDPVNCManager.ActiveTargetIDsForUser(sess.UserID)
	items := make([]RDPSessionItem, 0, len(activeIDs))
	for _, targetID := range activeIDs {
		if _, ok := allowedSet[access.TargetID(targetID)]; !ok {
			continue
		}
		target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
		if err != nil {
			continue
		}
		if target.Protocol != access.ProtocolRDP {
			continue
		}
		items = append(items, RDPSessionItem{TargetID: targetID, TargetName: target.Name})
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleRDPBrowserWebSocket launches xfreerdp→Xvfb→x11vnc, then proxies the
// resulting VNC stream over WebSocket so noVNC in the browser can display it.
func (a *App) handleRDPBrowserWebSocket(w http.ResponseWriter, r *http.Request) {
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

	widthStr := r.URL.Query().Get("w")
	heightStr := r.URL.Query().Get("h")
	width, height := 1920, 1080
	if v, e := strconv.Atoi(widthStr); e == nil && v >= 640 && v <= 3840 {
		width = v
	}
	if v, e := strconv.Atoi(heightStr); e == nil && v >= 480 && v <= 2160 {
		height = v
	}

	bridgeKey := fmt.Sprintf("%s:%s", sess.UserID, targetID)
	var bridge *rdpvnc.Bridge
	var targetAddr string
	if a.RDPVNCManager != nil {
		if existing, ok := a.RDPVNCManager.GetBridge(bridgeKey); ok {
			ew, eh := existing.Size()
			if ew == width && eh == height {
				targetAddr = fmt.Sprintf("127.0.0.1:%d", existing.VNCPort())
				log.Printf("rdp browser reconnect user_id=%s target_id=%s vnc_port=%d", sess.UserID, targetID, existing.VNCPort())
			} else {
				// Screen size changed; recreate bridge to match new window size.
				a.RDPVNCManager.Remove(bridgeKey)
			}
		}
	}
	if targetAddr == "" {
		// Do not accept credentials via URL query (leaks via logs/history/referrers).
		// Use stored credentials from target.
		rdpUser := target.SSHUsername
		rdpPass := target.SSHPassword
		var err error
		bridge, err = rdpvnc.Start(ctx, target.Host, int(target.Port), rdpUser, rdpPass, width, height)
		if err != nil {
			log.Printf("rdp browser bridge_failed user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
			writeJSONError(w, "failed to start RDP bridge: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if a.RDPVNCManager != nil {
			a.RDPVNCManager.Register(bridgeKey, bridge)
		}
		targetAddr = fmt.Sprintf("127.0.0.1:%d", bridge.VNCPort())
		log.Printf("rdp browser start user_id=%s target_id=%s vnc_port=%d", sess.UserID, targetID, bridge.VNCPort())
	}

	wsConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("rdp browser upgrade_failed user_id=%s target_id=%s err=%v", sess.UserID, targetID, err)
		if bridge != nil {
			bridge.Stop()
		}
		return
	}
	defer wsConn.Close()

	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		_ = vncproxy.Bridge(wsConn, targetAddr)
	}()

	if bridge != nil {
		bridgeDone := bridge.Done()
		select {
		case <-bridgeDone:
			_ = wsConn.Close()
		case <-proxyDone:
		}
	} else {
		<-proxyDone
	}
	// Do not stop the bridge on client disconnect so the session stays for reconnect.
}
