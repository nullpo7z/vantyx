package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/rdpproxy"
	"github.com/nullpo7z/vantyx/internal/rdpvnc"
	"github.com/nullpo7z/vantyx/internal/session"
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
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	_, err = a.SessionStore.Get(cookie.Value)
	if err != nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}

	targetID := r.URL.Query().Get("target_id")
	userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}

	if target.Protocol != access.ProtocolRDP {
		audit("rdp_ws_not_rdp", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"protocol":  target.Protocol,
		})
		writeJSONErrorKey(w, r, "rdp.notRDP", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		audit("rdp_ws_upgrade_failed", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "common.failedUpgradeConnection", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	targetAddr := target.Host + ":" + strconv.Itoa(int(target.Port))
	audit("rdp_ws_start", auditFields{
		"user_id":   userID,
		"target_id": targetID,
		"addr":      targetAddr,
	})
	_ = rdpproxy.Bridge(conn, targetAddr)
}

// RDPSessionItem is one entry in GET /api/rdp/sessions response.
type RDPSessionItem struct {
	SessionID   string    `json:"session_id"`
	TargetID    string    `json:"target_id"`
	TargetName  string    `json:"target_name"`
	TargetPath  string    `json:"target_path,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
	Idle        bool      `json:"idle"`
	IdleSeconds int       `json:"idle_seconds,omitempty"`
}

func rdpSessionItemFrom(s rdpvnc.Session, mgr *rdpvnc.Manager) RDPSessionItem {
	item := RDPSessionItem{
		SessionID:  s.ID,
		TargetID:   s.TargetID,
		TargetName: s.TargetName,
		CreatedAt:  s.CreatedAt,
		LastSeen:   s.LastSeen(),
	}
	if mgr != nil && mgr.IsIdle(&s) {
		item.Idle = true
		item.IdleSeconds = int(mgr.IdleDuration(&s).Seconds())
	}
	return item
}

// handleRDPSessions returns the list of active RDP (browser) sessions for the current user.
func (a *App) handleRDPSessions(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.RDPVNCManager == nil {
		writeJSON(w, map[string]interface{}{"items": []RDPSessionItem{}})
		return
	}
	ctx := r.Context()
	allowed, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowedSet := make(map[access.TargetID]struct{})
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	active := a.RDPVNCManager.ActiveSessionsForUser(userID)
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
		item := rdpSessionItemFrom(s, a.RDPVNCManager)
		item.TargetName = target.Name
		item.TargetPath = target.Path
		items = append(items, item)
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleRDPSessionDelete terminates the given RDP (browser) session. Caller must own the session.
func (a *App) handleRDPSessionDelete(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	if a.RDPVNCManager == nil {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		writeJSONErrorKey(w, r, "sessions.idRequired", http.StatusBadRequest)
		return
	}
	s, ok := a.RDPVNCManager.GetSession(sessionID)
	if !ok || s.UserID != userID {
		writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
		return
	}
	canAccess, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(s.TargetID))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canAccess {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	a.finishVideoRecording(sessionID)
	a.RDPVNCManager.RemoveSession(sessionID)
	if a.SharingRegistry != nil {
		a.SharingRegistry.Remove(sessionID)
	}
	audit("rdp_session_stop", auditFields{
		"session_id": sessionID,
		"user_id":    userID,
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRDPBrowserWebSocket launches xfreerdp→Xvfb→x11vnc, then proxies the
// resulting VNC stream over WebSocket so noVNC in the browser can display it.
func (a *App) handleRDPBrowserWebSocket(w http.ResponseWriter, r *http.Request) {
	targetID := strings.TrimSpace(r.URL.Query().Get("target_id"))
	sessionIDParam := strings.TrimSpace(r.URL.Query().Get("session_id"))
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	viewer := mode == "viewer"

	// Shared-session attach: resolve target from the existing browser RDP session.
	if sessionIDParam != "" && a.RDPVNCManager != nil {
		if sharedSess, ok := a.RDPVNCManager.GetSession(sessionIDParam); ok && sharedSess.Bridge != nil {
			if targetID == "" {
				targetID = sharedSess.TargetID
			} else if targetID != sharedSess.TargetID {
				writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
				return
			}
			userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
			if !ok {
				return
			}
			if target.Protocol != access.ProtocolRDP {
				writeJSONErrorKey(w, r, "rdp.notRDP", http.StatusBadRequest)
				return
			}
			if viewer {
				room, _ := a.SharingRegistry.Get(sharedSess.ID)
				if room == nil || !room.IsParticipant(userID) {
					writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
					return
				}
			} else if sharedSess.UserID != userID {
				writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
				return
			}
			a.startRDPBridgeProxyIfNeeded(sharedSess)
			wsConn, err := wsUpgrader.Upgrade(w, r, nil)
			if err != nil {
				writeJSONErrorKey(w, r, "rdp.startFailed", http.StatusInternalServerError)
				return
			}
			attachMode := session.AttachModeWriter
			if viewer {
				attachMode = session.AttachModeViewer
			}
			if sharedSess.AttachCh != nil {
				select {
				case sharedSess.AttachCh <- session.AttachReq{
					Conn:     wsConn,
					UserID:   userID,
					Username: a.usernameFor(r.Context(), userID),
					Mode:     attachMode,
				}:
				default:
					_ = wsConn.Close()
				}
			} else {
				_ = wsConn.Close()
			}
			return
		}
	}

	userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}

	if target.Protocol != access.ProtocolRDP {
		writeJSONErrorKey(w, r, "rdp.notRDP", http.StatusBadRequest)
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

	bridgeKey := fmt.Sprintf("%s:%s", userID, targetID)
	var bridge *rdpvnc.Bridge
	var targetAddr string
	var sessionID string
	if a.RDPVNCManager != nil {
		if existingSess, ok := a.RDPVNCManager.GetSessionByKey(bridgeKey); ok && existingSess.Bridge != nil {
			ew, eh := existingSess.Bridge.Size()
			if ew == width && eh == height {
				sessionID = existingSess.ID
				targetAddr = fmt.Sprintf("127.0.0.1:%d", existingSess.Bridge.VNCPort())
				a.startRDPBridgeProxyIfNeeded(existingSess)
				audit("rdp_browser_reconnect", auditFields{
					"user_id":    userID,
					"target_id":  targetID,
					"session_id": existingSess.ID,
					"vnc_port":   existingSess.Bridge.VNCPort(),
				})
			} else {
				// Screen size changed; recreate bridge to match new window size.
				a.RDPVNCManager.RemoveSession(existingSess.ID)
				if a.SessionEventBroker != nil {
					a.SessionEventBroker.Broadcast()
				}
			}
		}
	}
	if targetAddr == "" {
		// Do not accept credentials via URL query (leaks via logs/history/referrers).
		// Use stored credentials from target.
		rdpUser := target.SSHUsername
		rdpPass := target.SSHPassword
		var err error
		// Use a detached background context for the bridge so that it
		// survives HTTP handler return and client disconnect; lifecycle
		// is instead tied to the RDP process and explicit session delete.
		bridge, err = rdpvnc.Start(context.Background(), target.Host, int(target.Port), rdpUser, rdpPass, width, height)
		if err != nil {
			audit("rdp_browser_bridge_failed", auditFields{
				"user_id":   userID,
				"target_id": targetID,
				"error":     err.Error(),
			})
			writeJSONErrorKey(w, r, "rdp.bridgeFailed", http.StatusInternalServerError)
			return
		}
		if a.RDPVNCManager != nil {
			sid, err := newRDPSessionID()
			if err != nil {
				bridge.Stop()
				writeJSONErrorKey(w, r, "rdp.startFailed", http.StatusInternalServerError)
				return
			}
			sess := a.RDPVNCManager.RegisterSession(bridgeKey, sid, userID, targetID, target.Name, width, height, bridge)
			sessionID = sess.ID
			a.startRDPBridgeProxyIfNeeded(sess)
			a.startRDPVideoRecording(r.Context(), sessionID, userID, targetID, bridge.Display(), width, height)
			go func(id string, done <-chan struct{}) {
				<-done
				a.finishVideoRecording(id)
			}(sessionID, bridge.Done())
			if a.SessionEventBroker != nil {
				a.SessionEventBroker.Broadcast()
			}
		}
		targetAddr = fmt.Sprintf("127.0.0.1:%d", bridge.VNCPort())
		audit("rdp_browser_start", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"vnc_port":  bridge.VNCPort(),
		})
	}

	wsConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		audit("rdp_browser_upgrade_failed", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		if bridge != nil {
			bridge.Stop()
		}
		return
	}

	attachMode := session.AttachModeWriter
	if viewer {
		attachMode = session.AttachModeViewer
	}
	if sessionID == "" && a.RDPVNCManager != nil {
		if existingSess, ok := a.RDPVNCManager.GetSessionByKey(bridgeKey); ok {
			sessionID = existingSess.ID
		}
	}
	if viewer && sessionID != "" {
		room, _ := a.SharingRegistry.Get(sessionID)
		if room == nil || !room.IsParticipant(userID) {
			_ = wsConn.Close()
			writeJSONErrorKey(w, r, "sessions.notFoundOrAccessDenied", http.StatusNotFound)
			return
		}
	}
	var rdpSess *rdpvnc.Session
	if a.RDPVNCManager != nil && sessionID != "" {
		rdpSess, _ = a.RDPVNCManager.GetSession(sessionID)
	}
	if rdpSess != nil && rdpSess.AttachCh != nil {
		select {
		case rdpSess.AttachCh <- session.AttachReq{Conn: wsConn, UserID: userID, Username: a.usernameFor(r.Context(), userID), Mode: attachMode}:
			if !viewer && a.SharingBridges != nil {
				if controller, ok := a.SharingBridges.Get(session.ID(sessionID)); ok {
					if room, ok := a.SharingRegistry.Get(sessionID); ok {
						controller.SetWriter(roomWriterID(room))
					}
				}
			}
		default:
			_ = wsConn.Close()
		}
		return
	}
	defer wsConn.Close()

	var touch func()
	if a.RDPVNCManager != nil && sessionID != "" {
		sid := sessionID
		touch = func() { a.RDPVNCManager.Touch(sid) }
	}

	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		_ = vncproxy.Bridge(wsConn, targetAddr, touch)
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
