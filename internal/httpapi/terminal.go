package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
	"github.com/nullpo7z/vantyx/internal/recording"
	"github.com/nullpo7z/vantyx/internal/secret"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/telnetproxy"
)

func supportsDetachableTerminal(p access.Protocol) bool {
	return p == access.ProtocolSSH || p == access.ProtocolTelnet
}

// listTerminalSessions is satisfied by *session.Manager for GET /api/terminal/sessions.
type listTerminalSessions interface {
	ActiveIDs() []session.ID
}

// terminalSessionIDGen is set in tests to force duplicate session ID and cover Start error path.
var terminalSessionIDGen func() session.ID

func newTerminalSessionID() (session.ID, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return session.ID(hex.EncodeToString(b)), nil
}

func allowedWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Some non-browser clients may omit Origin. Disallow by default (CSWSH protection).
		// Can be enabled explicitly for local development/testing.
		if strings.TrimSpace(os.Getenv("VANTYX_ALLOW_WS_NO_ORIGIN")) != "1" {
			return false
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}

	// Allow explicit allowlist (comma-separated full origins like https://example.com).
	if v := strings.TrimSpace(os.Getenv("VANTYX_WS_ALLOWED_ORIGINS")); v != "" {
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(s) == origin {
				return true
			}
		}
	}

	// Default: same-origin (scheme + host) only.
	wantScheme := "https"
	if r.TLS == nil {
		wantScheme = "http"
	}
	wantHost := r.Host
	if strings.EqualFold(u.Scheme, wantScheme) && strings.EqualFold(u.Host, wantHost) {
		return true
	}
	return false
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return allowedWebSocketOrigin(r)
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
	PrivateKeyPassphrase string `json:"private_key_passphrase"` // 保存済み秘密鍵が暗号化されている場合に接続時に入力
	Name                 string `json:"name"`
	Description          string `json:"description"`
}

// wsReadConn is the minimal interface needed from *websocket.Conn for readTerminalCredentials.
type wsReadConn interface {
	ReadMessage() (int, []byte, error)
	SetReadDeadline(time.Time) error
}

// readTerminalCredentials reads the first text message and returns credentials.
// If use_stored_credentials is true, uses target's stored SSH username/password/key; client may send password or private_key_passphrase when not stored.
func readTerminalCredentials(conn wsReadConn, target *access.Target) (sshproxy.Credentials, error) {
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
		if target.SSHUsername == "" {
			return sshproxy.Credentials{}, errNoStoredCredentials
		}
		// パスワードや秘密鍵は未保存でも可。接続時にクライアントが password または private_key_passphrase を送る。
		// 接続時に入力された値は保存済みを上書きする（誤保存の修正や、その場で正しい値を入力できるようにする）。
		password := target.SSHPassword
		if m.Password != "" {
			password = m.Password
		}
		creds := sshproxy.Credentials{
			Username:    target.SSHUsername,
			Password:    password,
			Name:        m.Name,
			Description: m.Description,
		}
		if target.Protocol != access.ProtocolTelnet {
			keyPassphrase := target.SSHPrivateKeyPassphrase
			if m.PrivateKeyPassphrase != "" {
				keyPassphrase = m.PrivateKeyPassphrase
			}
			creds.PrivateKey = target.SSHPrivateKey
			creds.PrivateKeyPassphrase = keyPassphrase
		}
		if err := credentialsDecrypted(creds); err != nil {
			return sshproxy.Credentials{}, err
		}
		return creds, nil
	}
	if m.Username == "" {
		return sshproxy.Credentials{}, errInvalidCredentials
	}
	creds := sshproxy.Credentials{
		Username:    m.Username,
		Password:    m.Password,
		Name:        m.Name,
		Description: m.Description,
	}
	// ターゲットに保存済みの秘密鍵があり、クライアントがパスフレーズを送った場合はその鍵を使う（SSH のみ）
	if target.Protocol != access.ProtocolTelnet && target.SSHPrivateKey != "" && m.PrivateKeyPassphrase != "" {
		creds.PrivateKey = target.SSHPrivateKey
		creds.PrivateKeyPassphrase = m.PrivateKeyPassphrase
	}
	if err := credentialsDecrypted(creds); err != nil {
		return sshproxy.Credentials{}, err
	}
	return creds, nil
}

// credentialsDecrypted returns an error if PrivateKey or PrivateKeyPassphrase is still ciphertext (decryption failed at rest).
func credentialsDecrypted(creds sshproxy.Credentials) error {
	if creds.PrivateKey != "" && strings.HasPrefix(creds.PrivateKey, secret.CiphertextVersionPrefix) {
		return errCredentialsNotDecrypted
	}
	if creds.PrivateKeyPassphrase != "" && strings.HasPrefix(creds.PrivateKeyPassphrase, secret.CiphertextVersionPrefix) {
		return errCredentialsNotDecrypted
	}
	return nil
}

var (
	errInvalidCredentials      = errors.New("invalid or missing credentials (send JSON: {\"username\":\"...\",\"password\":\"...\"} or {\"use_stored_credentials\":true})")
	errNoStoredCredentials     = errors.New("stored credentials not configured for this target")
	errCredentialsNotDecrypted = errors.New("保存された認証情報の復号に失敗しています。VANTYX_ENCRYPTION_KEY を確認してください")
)

// handleSSHWebSocket upgrades the connection and starts a goroutine-backed terminal session.
// Requires query parameter target_id; the user must have access to that target.
// For Phase 2, the session still behaves as an echo server; actual SSH bridging follows.
func (a *App) handleSSHWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("vantyx_session")
	if err != nil || cookie.Value == "" {
		audit("terminal_ws_unauthorized", auditFields{
			"reason": "no_session_cookie",
		})
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(cookie.Value)
	if err != nil {
		audit("terminal_ws_unauthorized", auditFields{
			"reason": "invalid_session",
		})
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
		canAccess, err := a.userCanAccessTarget(r.Context(), sess.UserID, access.TargetID(termSess.TargetID))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if !canAccess {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			audit("terminal_ws_upgrade_failed_attach", auditFields{
				"session_id": sessionIDParam,
				"error":      err.Error(),
			})
			writeJSONError(w, "failed to upgrade connection", http.StatusBadRequest)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(""))
		select {
		case termSess.AttachCh <- session.AttachReq{Conn: conn}:
			audit("terminal_session_attach", auditFields{
				"session_id": sessionIDParam,
				"user_id":    sess.UserID,
			})
		default:
			_ = conn.WriteMessage(websocket.TextMessage, []byte("error: session attach slot busy"))
			_ = conn.Close()
		}
		return
	}

	targetID := r.URL.Query().Get("target_id")
	userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}

	if !supportsDetachableTerminal(target.Protocol) {
		audit("terminal_ws_not_implemented", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"protocol":  target.Protocol,
		})
		writeJSONError(w, "only SSH and Telnet targets supported", http.StatusNotImplemented)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		audit("terminal_ws_upgrade_failed", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		writeJSONError(w, "failed to upgrade connection", http.StatusBadRequest)
		return
	}

	creds, err := readTerminalCredentials(conn, target)
	if err != nil {
		audit("terminal_ws_credentials_invalid", auditFields{
			"user_id":   userID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		_ = conn.Close()
		return
	}
	audit("terminal_ws_credentials_ok", auditFields{
		"user_id":   userID,
		"target_id": targetID,
		"ssh_user":  creds.Username,
	})

	// terminalSessionIDGen is overridden in tests to trigger Start id-collision error path.
	var id session.ID
	if terminalSessionIDGen != nil {
		id = terminalSessionIDGen()
	} else {
		var err error
		id, err = newTerminalSessionID()
		if err != nil {
			audit("terminal_session_idgen_failed", auditFields{
				"user_id":   sess.UserID,
				"target_id": targetID,
				"error":     err.Error(),
			})
			_ = conn.Close()
			http.Error(w, "failed to start terminal session", http.StatusInternalServerError)
			return
		}
	}

	opts := session.StartOptions{
		UserID:      userID,
		TargetID:    targetID,
		TargetName:  target.Name,
		Name:        creds.Name,
		Description: creds.Description,
	}
	q := r.URL.Query()
	cols, rows := 80, 24
	if c := q.Get("cols"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 && n <= 512 {
			cols = n
		}
	}
	if rv := q.Get("rows"); rv != "" {
		if n, err := strconv.Atoi(rv); err == nil && n > 0 && n <= 256 {
			rows = n
		}
	}
	_, err = a.TerminalSessionManager.Start(id, opts, func(ctx context.Context, termSess *session.Session) {
		a.runDetachableBridge(ctx, termSess, a.TerminalSessionManager, id, conn, target, creds, cols, rows)
		if a.SessionEventBroker != nil {
			a.SessionEventBroker.Broadcast()
		}
	})
	if err != nil {
		audit("terminal_session_start_failed", auditFields{
			"user_id":   sess.UserID,
			"target_id": targetID,
			"error":     err.Error(),
		})
		_ = conn.Close()
		http.Error(w, "failed to start terminal session", http.StatusInternalServerError)
		return
	}
	audit("terminal_session_start", auditFields{
		"session_id": id,
		"user_id":    sess.UserID,
		"target_id":  targetID,
		"host":       target.Host,
		"port":       target.Port,
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
}

// TerminalSessionItem is one entry in GET /api/terminal/sessions response.
type TerminalSessionItem struct {
	SessionID   string    `json:"session_id"`
	TargetID    string    `json:"target_id"`
	TargetName  string    `json:"target_name"`
	TargetPath  string    `json:"target_path,omitempty"` // ターゲットの階層パス（例: prod/network）
	Protocol    string    `json:"protocol"`              // ターゲットのプロトコル（ssh / telnet 等）
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
	Idle        bool      `json:"idle"`
	IdleSeconds int       `json:"idle_seconds,omitempty"`
}

// terminalIdleWarnAfter returns the configured idle warning threshold from the terminal session manager.
func (a *App) terminalIdleWarnAfter() time.Duration {
	if m, ok := a.TerminalSessionManager.(*session.Manager); ok {
		return m.IdleWarnAfter()
	}
	return 0
}

func terminalSessionItemFrom(sess *session.Session, mgr *session.Manager, protocol access.Protocol, targetPath string) TerminalSessionItem {
	item := TerminalSessionItem{
		SessionID:   string(sess.ID()),
		TargetID:    sess.TargetID,
		TargetName:  sess.TargetName,
		TargetPath:  targetPath,
		Protocol:    string(protocol),
		Name:        sess.Name,
		Description: sess.Description,
		CreatedAt:   sess.CreatedAt(),
		LastSeen:    sess.LastSeen(),
	}
	if mgr != nil && mgr.IsIdle(sess) {
		item.Idle = true
		item.IdleSeconds = int(mgr.IdleDuration(sess).Seconds())
	}
	return item
}

// handleTerminalSessions returns the list of active terminal sessions for the current user.
func (a *App) handleTerminalSessions(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	lister, ok := a.TerminalSessionManager.(listTerminalSessions)
	if !ok {
		writeJSON(w, map[string]interface{}{"items": []TerminalSessionItem{}})
		return
	}
	ctx := r.Context()
	allowedSet, err := a.allowedTargetIDSet(ctx, userID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var mgr *session.Manager
	if m, ok := a.TerminalSessionManager.(*session.Manager); ok {
		mgr = m
	}
	ids := lister.ActiveIDs()
	items := make([]TerminalSessionItem, 0, len(ids))
	for _, id := range ids {
		sess, ok := a.TerminalSessionManager.Get(id)
		if !ok || sess.UserID != userID {
			continue
		}
		if _, ok := allowedSet[access.TargetID(sess.TargetID)]; !ok {
			continue
		}
		protocol := access.ProtocolSSH
		targetPath := ""
		if target, err := a.TargetStore.Get(ctx, access.TargetID(sess.TargetID)); err == nil && target != nil {
			protocol = target.Protocol
			targetPath = target.Path
		}
		items = append(items, terminalSessionItemFrom(sess, mgr, protocol, targetPath))
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleTerminalSessionDelete terminates the given terminal session. Caller must own the session.
func (a *App) handleTerminalSessionDelete(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
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
	if !ok || termSess.UserID != userID {
		writeJSONError(w, "session not found or access denied", http.StatusNotFound)
		return
	}
	canAccess, err := a.userCanAccessTarget(r.Context(), userID, access.TargetID(termSess.TargetID))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canAccess {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return
	}
	a.TerminalSessionManager.Stop(id)
	audit("terminal_session_stop", auditFields{
		"session_id": sessionID,
		"user_id":    userID,
	})
	if a.SessionEventBroker != nil {
		a.SessionEventBroker.Broadcast()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListRecordings returns recordings for the current user (metadata only, no file_path).
func (a *App) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if a.DB == nil {
		writeJSON(w, map[string]interface{}{"items": []interface{}{}})
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	targetID := q.Get("target_id")
	query := `SELECT id, user_id, target_id, session_id, channel_type, started_at, ended_at, COALESCE(session_name, ''), COALESCE(session_description, '') FROM recordings WHERE user_id = ?`
	args := []interface{}{userID}
	if targetID != "" {
		query += ` AND target_id = ?`
		args = append(args, targetID)
	}
	query += ` ORDER BY started_at DESC LIMIT 200`
	rows, err := a.DB.QueryContext(ctx, query, args...)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	var items []map[string]interface{}
	for rows.Next() {
		var id, userID, tID, sessID, channelType, startedAt, sessName, sessDesc string
		var endedAt sql.NullString
		if err := rows.Scan(&id, &userID, &tID, &sessID, &channelType, &startedAt, &endedAt, &sessName, &sessDesc); err != nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"id":                  id,
			"user_id":             userID,
			"target_id":           tID,
			"session_id":          sessID,
			"channel_type":        channelType,
			"started_at":          startedAt,
			"ended_at":            endedAt.String,
			"session_name":        sessName,
			"session_description": sessDesc,
		})
	}
	writeJSON(w, map[string]interface{}{"items": items})
}

// handleGetRecordingFile serves the recording file. Query format=cast|gif|webm (default cast).
// cast = asciinema .cast; gif/webm require agg (and ffmpeg for webm) to be installed, else 503.
func (a *App) handleGetRecordingFile(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rawID := chi.URLParam(r, "recording_id")
	if rawID == "" {
		writeJSONError(w, "recording_id required", http.StatusBadRequest)
		return
	}
	recordingID := rawID
	if decoded, e := url.PathUnescape(rawID); e == nil {
		recordingID = decoded
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "cast"
	}
	switch format {
	case "cast", "gif", "webm":
	default:
		writeJSONError(w, "format must be cast, gif, or webm", http.StatusBadRequest)
		return
	}
	if a.DB == nil {
		writeJSONError(w, "recordings not available", http.StatusServiceUnavailable)
		return
	}
	var (
		filePath string
		err      error
	)
	err = a.DB.QueryRowContext(r.Context(), `SELECT file_path FROM recordings WHERE id = ? AND user_id = ?`, recordingID, userID).Scan(&filePath)
	if err == sql.ErrNoRows {
		writeJSONError(w, "recording not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR")
	if recordingDir == "" {
		writeJSONError(w, "recordings not configured", http.StatusServiceUnavailable)
		return
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}
	absDir, err := filepath.Abs(recordingDir)
	if err != nil {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}
	tryOpen := func(path string) (*os.File, error) {
		return os.Open(path) // #nosec G304 -- path validated above (no .. or prefix)
	}
	sanitizeBasename := func(name string) string {
		const ext = ".cast"
		if !strings.HasSuffix(name, ext) {
			s := strings.ReplaceAll(name, ":", "-")
			return strings.ReplaceAll(s, ".", "-")
		}
		prefix := name[:len(name)-len(ext)]
		safe := strings.ReplaceAll(prefix, ":", "-")
		safe = strings.ReplaceAll(safe, ".", "-")
		return safe + ext
	}
	castPath := filePath
	f, err := tryOpen(filePath)
	if err != nil {
		dir, base := filepath.Dir(filePath), filepath.Base(filePath)
		safeBase := sanitizeBasename(base)
		if safeBase != base {
			castPath = filepath.Join(dir, safeBase)
			f, err = tryOpen(castPath)
		}
	}
	if err != nil {
		writeJSONError(w, "recording file not found", http.StatusNotFound)
		return
	}
	if format == "cast" {
		defer f.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(castPath))
		_, _ = io.Copy(w, f)
		return
	}
	f.Close()
	// GIF or WebM: convert with agg (and ffmpeg for webm)
	outPath, contentType, disposition, err := convertCastToVideo(castPath, format)
	if err != nil {
		audit("recording_convert_failed", auditFields{
			"id":     recordingID,
			"format": format,
			"error":  err.Error(),
		})
		writeJSONError(w, "video export unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer os.Remove(outPath)
	out, err := os.Open(outPath) // #nosec G304 -- path from convertCastToVideo (temp file we created)
	if err != nil {
		writeJSONError(w, "failed to read converted file", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	_, _ = io.Copy(w, out)
}

// convertCastToVideo converts a .cast file to gif or webm using agg (and ffmpeg for webm).
// Returns (outPath, contentType, contentDisposition, error). Caller must os.Remove(outPath).
func convertCastToVideo(castPath, format string) (string, string, string, error) {
	aggPath, err := exec.LookPath("agg")
	if err != nil {
		return "", "", "", errors.New("agg not found in PATH (install asciinema-agg for GIF/WebM export)")
	}
	dir := filepath.Dir(castPath)
	gifFile, err := os.CreateTemp(dir, "rec-*.gif")
	if err != nil {
		return "", "", "", err
	}
	gifPath := gifFile.Name()
	gifFile.Close()
	defer func() {
		if format == "webm" {
			_ = os.Remove(gifPath)
		}
	}()
	cmd := exec.Command(aggPath, castPath, gifPath) // #nosec G204 -- paths from validated castPath and temp file
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(gifPath)
		return "", "", "", errors.New(strings.TrimSpace(string(out)) + ": " + runErr.Error())
	}
	if format == "gif" {
		return gifPath, "image/gif", `attachment; filename="recording.gif"`, nil
	}
	// WebM: ffmpeg -i gifPath -c:v libvpx-vp9 -pix_fmt yuv420p -an -b:v 0 -crf 30 out.webm
	// Use yuv420p (not yuva420p); terminal GIFs from agg are opaque. -vf scale ensures even dimensions.
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", "", "", errors.New("ffmpeg not found in PATH (required for WebM export)")
	}
	webmFile, err := os.CreateTemp(dir, "rec-*.webm")
	if err != nil {
		return "", "", "", err
	}
	webmPath := webmFile.Name()
	webmFile.Close()
	cmd = exec.Command(ffmpegPath, "-y", "-i", gifPath,
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-c:v", "libvpx-vp9", "-pix_fmt", "yuv420p", "-an", "-b:v", "0", "-crf", "30",
		webmPath) // #nosec G204
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(webmPath)
		return "", "", "", errors.New("ffmpeg: " + strings.TrimSpace(string(out)))
	}
	return webmPath, "video/webm", `attachment; filename="recording.webm"`, nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// runDetachableBridge starts the SSH bridge and keeps it running when the client detaches.
// Initial conn is attached first; further attaches (resume) come via termSess.AttachCh.
// When VANTYX_RECORDINGS_DIR is set, asciinema-format recording is written and metadata stored in recordings table.
// cols and rows are the initial terminal size (from client) for PTY and recording; 0 uses bridge default.
func (a *App) runDetachableBridge(ctx context.Context, termSess *session.Session, manager terminalSessionStarter, id session.ID, conn *websocket.Conn, target *access.Target, creds sshproxy.Credentials, cols, rows int) {
	_ = conn.WriteMessage(websocket.TextMessage, []byte(""))
	// Send session_id so the client can reconnect (resume) without credentials.
	if b, err := json.Marshal(struct {
		SessionID string `json:"session_id"`
	}{SessionID: string(id)}); err == nil {
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}
	touch := func() { manager.Touch(id) }

	var tee io.Writer
	var stdinRecorder sshproxy.StdinRecorder
	var recordingCloser func()
	if recordingDir := os.Getenv("VANTYX_RECORDINGS_DIR"); recordingDir != "" {
		_ = os.MkdirAll(recordingDir, 0750) // #nosec G703 -- path from env, dir is admin-configured
		// セッションIDは RFC3339Nano でコロンを含むため、ファイル名として使う場合はサニタイズ（Windows 等で不可の文字を置換）
		safeName := strings.ReplaceAll(string(id), ":", "-")
		safeName = strings.ReplaceAll(safeName, ".", "-")
		castPath := filepath.Join(recordingDir, safeName+".cast")
		f, err := os.Create(castPath) // #nosec G703 G304 -- path under recordingDir, safeName sanitized
		if err != nil {
			audit("recording_create_failed", auditFields{
				"session_id": id,
				"path":       castPath,
				"error":      err.Error(),
			})
		} else {
			startedAt := time.Now().UTC()
			w, h := cols, rows
			if w <= 0 {
				w = 80
			}
			if h <= 0 {
				h = 24
			}
			asc := recording.NewAsciinemaWriter(f, w, h)
			tee = asc
			stdinRecorder = asc
			if a.DB != nil {
				sessName := termSess.Name
				sessDesc := termSess.Description
				if _, err := a.DB.ExecContext(ctx, `INSERT INTO recordings (id, user_id, target_id, session_id, channel_type, file_path, started_at, session_name, session_description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					string(id), termSess.UserID, termSess.TargetID, string(id), "browser", castPath, startedAt.Format("2006-01-02 15:04:05"), sessName, sessDesc); err != nil {
					audit("recording_insert_failed", auditFields{
						"session_id": id,
						"error":      err.Error(),
					})
				}
			}
			recordingCloser = func() {
				_ = f.Sync() // バッファをディスクにフラッシュしてから閉じる
				_ = f.Close()
				if a.DB != nil {
					_, _ = a.DB.ExecContext(context.Background(), `UPDATE recordings SET ended_at = ? WHERE id = ?`, time.Now().UTC().Format("2006-01-02 15:04:05"), string(id))
				}
			}
		}
		if recordingCloser != nil {
			defer recordingCloser()
		}
	}

	// Command log recorder: records stdin lines into command_logs for search.
	cmdRec := newCommandLogRecorder(a.CommandLogStore, string(id), termSess.UserID, termSess.TargetID)
	if cmdRec != nil {
		prev := stdinRecorder
		stdinRecorder = sshproxy.StdinRecorderFunc(func(p []byte) {
			if prev != nil {
				prev.RecordInput(p)
			}
			cmdRec.RecordInput(p)
		})
	}

	var bridgeErr error
	var endReason, endMsg string
	switch target.Protocol {
	case access.ProtocolTelnet:
		var telStdin telnetproxy.StdinRecorder
		if stdinRecorder != nil {
			telStdin = telnetproxy.StdinRecorderFunc(stdinRecorder.RecordInput)
		}
		bridgeErr = telnetproxy.RunBridgeDetachable(ctx, target.Host, target.Port, creds.Username, creds.Password, termSess.Output, termSess.AttachCh, conn, touch, tee, telStdin, cols, rows, nil)
		endReason = "telnet_session_closed"
		endMsg = "session_ended: Telnet session closed"
	default:
		bridgeErr = sshproxy.RunBridgeDetachable(ctx, target.Host, target.Port, creds.Username, creds.Password, creds.PrivateKey, creds.PrivateKeyPassphrase, termSess.Output, termSess.AttachCh, conn, touch, tee, stdinRecorder, cols, rows, nil)
		endReason = "ssh_session_closed"
		endMsg = "session_ended: SSH session closed"
	}
	if bridgeErr != nil {
		audit("terminal_bridge_end_error", auditFields{
			"session_id": id,
			"error":      proxyerrors.UnwrapForAudit(bridgeErr),
		})
		_ = conn.WriteMessage(websocket.TextMessage, []byte("error: "+proxyerrors.BridgeErrorMessage(bridgeErr)))
	} else {
		audit("terminal_bridge_end", auditFields{
			"session_id": id,
			"reason":     endReason,
		})
		_ = conn.WriteMessage(websocket.TextMessage, []byte(endMsg))
	}
	_ = conn.Close()
	// コールバックから return すると Manager の goroutine が sess.done を close し delete(m.sessions, id) するため、
	// ここで manager.Stop(id) を呼ぶとデッドロックになる（Stop は <-sess.done で待つが、return するまで done は close されない）。
	// セッション一覧からの削除はコールバック return 時に自動で行われる。
}

// InsertRecording inserts a recording row (for CLI or other non-browser channels).
// Used by sshd when RecordingsDir and RecordingStore are set. No-op if a.DB is nil.
func (a *App) InsertRecording(ctx context.Context, id, userID, targetID, sessionID, channelType, filePath, startedAt, sessionName, sessionDesc string) error {
	if a.DB == nil {
		return nil
	}
	_, err := a.DB.ExecContext(ctx, `INSERT INTO recordings (id, user_id, target_id, session_id, channel_type, file_path, started_at, session_name, session_description) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, targetID, sessionID, channelType, filePath, startedAt, sessionName, sessionDesc)
	return err
}

// UpdateRecordingEnded sets ended_at for a recording. No-op if a.DB is nil.
func (a *App) UpdateRecordingEnded(ctx context.Context, id, endedAt string) error {
	if a.DB == nil {
		return nil
	}
	_, err := a.DB.ExecContext(ctx, `UPDATE recordings SET ended_at = ? WHERE id = ?`, endedAt, id)
	return err
}
