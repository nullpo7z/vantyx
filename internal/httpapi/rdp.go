package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/rdpproxy"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/vncproxy"
)

func newRDPSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

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
	SessionID  string    `json:"session_id"`
	TargetID   string    `json:"target_id"`
	TargetName string    `json:"target_name"`
	CreatedAt  time.Time `json:"created_at"`
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
	active := a.RDPVNCManager.ActiveSessionsForUser(sess.UserID)
	items := make([]RDPSessionItem, 0, len(active))
	for _, s := range active {
		targetID := s.TargetID
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
		items = append(items, RDPSessionItem{
			SessionID:  s.ID,
			TargetID:   targetID,
			TargetName: target.Name,
			CreatedAt:  s.CreatedAt,
		})
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleRDPSessionDelete terminates the given RDP (browser) session. Caller must own the session.
func (a *App) handleRDPSessionDelete(w http.ResponseWriter, r *http.Request) {
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
	if a.RDPVNCManager == nil {
		writeJSONError(w, "session not found or access denied", http.StatusNotFound)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONError(w, "session_id required", http.StatusBadRequest)
		return
	}
	s, ok := a.RDPVNCManager.GetSession(sessionID)
	if !ok || s.UserID != authSess.UserID {
		writeJSONError(w, "session not found or access denied", http.StatusNotFound)
		return
	}
	a.RDPVNCManager.RemoveSession(sessionID)
	log.Printf("rdp session stopped session_id=%s user_id=%s", sessionID, authSess.UserID)
	w.WriteHeader(http.StatusNoContent)
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
		if existingSess, ok := a.RDPVNCManager.GetSessionByKey(bridgeKey); ok && existingSess.Bridge != nil {
			ew, eh := existingSess.Bridge.Size()
			if ew == width && eh == height {
				targetAddr = fmt.Sprintf("127.0.0.1:%d", existingSess.Bridge.VNCPort())
				log.Printf("rdp browser reconnect user_id=%s target_id=%s session_id=%s vnc_port=%d", sess.UserID, targetID, existingSess.ID, existingSess.Bridge.VNCPort())
			} else {
				// Screen size changed; recreate bridge to match new window size.
				a.RDPVNCManager.RemoveSession(existingSess.ID)
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
			sid, err := newRDPSessionID()
			if err != nil {
				bridge.Stop()
				writeJSONError(w, "failed to start RDP session", http.StatusInternalServerError)
				return
			}
			a.RDPVNCManager.RegisterSession(bridgeKey, sid, sess.UserID, targetID, target.Name, width, height, bridge)
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
