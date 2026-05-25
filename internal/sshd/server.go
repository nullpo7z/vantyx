package sshd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
	"github.com/nullpo7z/vantyx/internal/recording"
	"github.com/nullpo7z/vantyx/internal/secret"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/telnetproxy"
)

// SessionStarter starts and touches terminal sessions (same as browser terminal).
// ListSessions is optional: if implemented, CLI can show "Resume" menu.
type SessionStarter interface {
	Start(id session.ID, opts session.StartOptions, fn func(context.Context, *session.Session)) (*session.Session, error)
	Get(id session.ID) (*session.Session, bool)
	Touch(id session.ID)
}

// SessionLister is optional and implemented by *session.Manager for CLI resume menu.
type SessionLister interface {
	ActiveIDs() []session.ID
}

// SessionStopper is optional and implemented by *session.Manager to end background bridges.
type SessionStopper interface {
	Stop(id session.ID)
}

// RecordingStore is used to persist recording metadata when CLI sessions are recorded (optional).
type RecordingStore interface {
	InsertRecording(ctx context.Context, id, userID, targetID, sessionID, channelType, filePath, startedAt, sessionName, sessionDesc string) error
	UpdateRecordingEnded(ctx context.Context, id, endedAt string) error
}

// Server is the CLI SSH gateway: users log in with Vantyx credentials, then choose a target to proxy to.
type Server struct {
	userStore      auth.UserStore
	targetStore    access.TargetStore
	groupStore     access.AccessGroupStore
	sessionManager SessionStarter
	config         *ssh.ServerConfig
	listener       net.Listener
	mu             sync.Mutex
	shutdown       bool
	recordingDir   string
	recordingStore RecordingStore
}

// Config holds dependencies for the SSH server.
type Config struct {
	UserStore      auth.UserStore
	TargetStore    access.TargetStore
	GroupStore     access.AccessGroupStore
	SessionManager SessionStarter
	// HostKey is the SSH host private key. If nil, a new 2048-bit RSA key is generated (not persisted).
	HostKey ssh.Signer
	// RecordingsDir enables asciinema recording for CLI connect sessions when set (e.g. VANTYX_RECORDINGS_DIR).
	// RecordingStore must also be set to persist metadata.
	RecordingsDir  string
	RecordingStore RecordingStore
}

// NewServer builds an SSH server that authenticates with UserStore and proxies to targets via GroupStore/TargetStore.
func NewServer(cfg Config) (*Server, error) {
	if cfg.UserStore == nil || cfg.TargetStore == nil || cfg.GroupStore == nil || cfg.SessionManager == nil {
		return nil, fmt.Errorf("sshd: UserStore, TargetStore, GroupStore and SessionManager are required")
	}
	hostKey := cfg.HostKey
	if hostKey == nil {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("sshd: generate host key: %w", err)
		}
		hostKey, err = ssh.NewSignerFromKey(key)
		if err != nil {
			return nil, fmt.Errorf("sshd: signer from key: %w", err)
		}
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			user, err := cfg.UserStore.AuthenticateByPublicKey(c.User(), key)
			if err != nil {
				return nil, err
			}
			return &ssh.Permissions{
				Extensions: map[string]string{"user_id": user.ID},
			}, nil
		},
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			user, err := cfg.UserStore.Authenticate(c.User(), string(pass))
			if err != nil {
				return nil, err
			}
			// Store user id in permissions so handler can use it
			return &ssh.Permissions{
				Extensions: map[string]string{"user_id": user.ID},
			}, nil
		},
	}
	config.AddHostKey(hostKey)
	return &Server{
		userStore:      cfg.UserStore,
		targetStore:    cfg.TargetStore,
		groupStore:     cfg.GroupStore,
		sessionManager: cfg.SessionManager,
		config:         config,
		recordingDir:   cfg.RecordingsDir,
		recordingStore: cfg.RecordingStore,
	}, nil
}

// ListenAndServe listens on addr and serves CLI SSH until Shutdown or error.
func (s *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(listener)
}

// Serve accepts connections from listener and serves CLI SSH until Shutdown or listener closed.
// Used by ListenAndServe; also allows tests to pass a listener (e.g. from net.Listen("tcp", "127.0.0.1:0")).
func (s *Server) Serve(listener net.Listener) error {
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	slog.Info("sshd listening", "addr", listener.Addr().String())
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.shutdown
			s.mu.Unlock()
			if closed {
				return nil
			}
			return err
		}
		go s.handleConn(conn)
	}
}

// Addr returns the listener's address when the server is serving; nil otherwise (for tests that need the address after ListenAndServe).
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

// Shutdown closes the listener so ListenAndServe returns.
func (s *Server) Shutdown() error {
	s.mu.Lock()
	s.shutdown = true
	l := s.listener
	s.mu.Unlock()
	if l != nil {
		return l.Close()
	}
	return nil
}

func (s *Server) handleConn(nconn net.Conn) {
	defer nconn.Close()
	// Long timeout so menu and DB listing are not cut off (handshake + channel open can be slow).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sshConn, chans, reqs, err := ssh.NewServerConn(nconn, s.config)
	if err != nil {
		slog.Debug("sshd handshake failed", "remote", nconn.RemoteAddr(), "err", err)
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	userID := sshConn.Permissions.Extensions["user_id"]
	if userID == "" {
		return
	}
	var sessionDone sync.WaitGroup
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		channel, inReqs, err := ch.Accept()
		if err != nil {
			continue
		}
		sessionDone.Add(1)
		go func() {
			defer sessionDone.Done()
			defer channel.Close()
			var ptyCols, ptyRows int
			resizeChan := make(chan sshproxy.TerminalSize, 8)
			var menuReturned chan struct{} // closed when runMenu returns (e.g. user chose Exit)
			for {
				if menuReturned != nil {
					select {
					case <-menuReturned:
						close(resizeChan)
						return
					case r, ok := <-inReqs:
						if !ok {
							close(resizeChan)
							return
						}
						accept := r.Type == "shell" || r.Type == "pty-req" || r.Type == "window-change"
						if r.Type == "pty-req" {
							if c, row, ok := parsePtyReqPayload(r.Payload); ok {
								ptyCols, ptyRows = c, row
							}
						}
						if r.Type == "window-change" {
							if c, row, ok := parseWindowChangePayload(r.Payload); ok {
								select {
								case resizeChan <- sshproxy.TerminalSize{Cols: c, Rows: row}:
								default:
									select {
									case <-resizeChan:
									default:
									}
									select {
									case resizeChan <- sshproxy.TerminalSize{Cols: c, Rows: row}:
									default:
									}
								}
							}
						}
						if r.WantReply {
							_ = r.Reply(accept, nil)
						}
					}
				} else {
					r, ok := <-inReqs
					if !ok {
						return
					}
					accept := r.Type == "shell" || r.Type == "pty-req" || r.Type == "window-change"
					if r.Type == "pty-req" {
						if c, row, ok := parsePtyReqPayload(r.Payload); ok {
							ptyCols, ptyRows = c, row
						}
					}
					if r.Type == "window-change" {
						if c, row, ok := parseWindowChangePayload(r.Payload); ok {
							select {
							case resizeChan <- sshproxy.TerminalSize{Cols: c, Rows: row}:
							default:
								select {
								case <-resizeChan:
								default:
								}
								select {
								case resizeChan <- sshproxy.TerminalSize{Cols: c, Rows: row}:
								default:
								}
							}
						}
					}
					if r.WantReply {
						_ = r.Reply(accept, nil)
					}
					if r.Type == "shell" {
						menuReturned = make(chan struct{})
						go func() {
							s.runMenu(ctx, channel, userID, ptyCols, ptyRows, resizeChan)
							close(menuReturned)
						}()
					}
				}
			}
		}()
		break
	}
	sessionDone.Wait()
}

// parsePtyReqPayload extracts terminal width (cols) and height (rows) from an SSH pty-req payload (RFC 4254).
// Returns (0, 0, false) if payload is too short or invalid.
func parsePtyReqPayload(payload []byte) (cols, rows int, ok bool) {
	if len(payload) < 12 {
		return 0, 0, false
	}
	r := bytes.NewReader(payload)
	var termLen uint32
	if err := binary.Read(r, binary.BigEndian, &termLen); err != nil {
		return 0, 0, false
	}
	if int(termLen) < 0 || len(payload) < 4+int(termLen)+8 {
		return 0, 0, false
	}
	if _, err := r.Seek(int64(4+termLen), io.SeekStart); err != nil {
		return 0, 0, false
	}
	var w, h uint32
	if err := binary.Read(r, binary.BigEndian, &w); err != nil {
		return 0, 0, false
	}
	if err := binary.Read(r, binary.BigEndian, &h); err != nil {
		return 0, 0, false
	}
	return int(w), int(h), true
}

// parseWindowChangePayload extracts cols and rows from an SSH window-change payload (RFC 4254).
func parseWindowChangePayload(payload []byte) (cols, rows int, ok bool) {
	if len(payload) < 8 {
		return 0, 0, false
	}
	r := bytes.NewReader(payload)
	var w, h uint32
	if err := binary.Read(r, binary.BigEndian, &w); err != nil {
		return 0, 0, false
	}
	if err := binary.Read(r, binary.BigEndian, &h); err != nil {
		return 0, 0, false
	}
	return int(w), int(h), true
}

// sendExitStatus sends SSH exit-status (RFC 4254 §6.10) so the client sees exit code 0 on normal close.
func sendExitStatus(channel ssh.Channel, code uint32) {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, code)
	_, _ = channel.SendRequest("exit-status", false, payload)
}

// cliGroupEntry is one group and its terminal targets (SSH/Telnet) for CLI list/connect.
type cliGroupEntry struct {
	Group   *access.AccessGroup
	Targets []*access.Target
}

func isCLITerminalProtocol(p access.Protocol) bool {
	return p == access.ProtocolSSH || p == access.ProtocolTelnet
}

// prepareCLIFrame draws the session status bar and starts resize forwarding for a target session.
func (s *Server) prepareCLIFrame(wr io.Writer, screenCols, ptyRows int, targetName string, protocol access.Protocol, showEndSessionHint bool, stopCh <-chan struct{}, resizeChan <-chan sshproxy.TerminalSize) (*cliSessionFrame, chan sshproxy.TerminalSize, error) {
	frame := newCLISessionFrame(wr, screenCols, ptyRows)
	bar := cliSessionBarState{
		TargetName:         targetName,
		Protocol:           protocol,
		Cols:               screenCols,
		ShowEndSessionHint: showEndSessionHint,
	}
	if err := frame.Enter(bar); err != nil {
		return nil, nil, err
	}
	bridgeResize := make(chan sshproxy.TerminalSize, 8)
	frame.SetOnResize(func(c, r int) {
		select {
		case bridgeResize <- sshproxy.TerminalSize{Cols: c, Rows: r}:
		default:
		}
	})
	go watchCLIResize(stopCh, resizeChan, frame)
	return frame, bridgeResize, nil
}

// cliStreamAttachOpts configures CLI attach read behavior.
type cliStreamAttachOpts struct {
	endOnCtrlD          bool // 0x04 ends session (connect only)
	detachOnCtrlBracket bool // 0x1d returns to menu without stopping the bridge
}

// cliAttachOutcome records whether the user ended the session (Ctrl+D) vs detached (Ctrl+]).
type cliAttachOutcome struct {
	mu    sync.Mutex
	ended bool
}

func (o *cliAttachOutcome) markEnded() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.ended = true
	o.mu.Unlock()
}

func (o *cliAttachOutcome) endedSession() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ended
}

// newCLIStreamAttach builds an attach handle for SSH or Telnet detachable bridges.
func (s *Server) newCLIStreamAttach(protocol access.Protocol, wr io.Writer, inputCh <-chan byte, stopCh <-chan struct{}, readStarted *bool, readDone chan struct{}, closeFn func() error, opts cliStreamAttachOpts, outcome *cliAttachOutcome) interface{} {
	writeFn := func(p []byte) error { _, e := wr.Write(p); return e }
	stopForControl := func(b byte) bool {
		if opts.detachOnCtrlBracket && b == 0x1d {
			return true
		}
		if opts.endOnCtrlD && b == 0x04 {
			outcome.markEnded()
			return true
		}
		return false
	}
	startRead := func(stdinCh chan<- []byte, onClose func()) {
		if readStarted != nil {
			*readStarted = true
		}
		go func() {
			defer func() {
				onClose()
				if readDone != nil {
					close(readDone)
				}
			}()
			for {
				select {
				case b, ok := <-inputCh:
					if !ok {
						return
					}
					if stopForControl(b) {
						return
					}
					buf := []byte{b}
				drain:
					for {
						select {
						case b, ok = <-inputCh:
							if !ok {
								break drain
							}
							if stopForControl(b) {
								return
							}
							buf = append(buf, b)
						default:
							break drain
						}
					}
					cp := make([]byte, len(buf))
					copy(cp, buf)
					select {
					case stdinCh <- cp:
					case <-stopCh:
						return
					}
				case <-stopCh:
					return
				}
			}
		}()
	}
	if protocol == access.ProtocolTelnet {
		return &telnetproxy.StreamAttach{Write: writeFn, StartRead: startRead, CloseFn: closeFn}
	}
	return &sshproxy.StreamAttach{Write: writeFn, StartRead: startRead, CloseFn: closeFn}
}

// loadGroupsWithTerminalTargets returns groups the user can see, each with SSH/Telnet targets.
func (s *Server) loadGroupsWithTerminalTargets(ctx context.Context, userID string) ([]cliGroupEntry, error) {
	groupIDs, err := s.groupStore.GroupIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		return nil, err
	}
	allowedTargetIDs, err := s.groupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		return nil, err
	}
	allowedSet := make(map[access.TargetID]bool)
	for _, id := range allowedTargetIDs {
		allowedSet[id] = true
	}
	var out []cliGroupEntry
	for _, gid := range groupIDs {
		g, err := s.groupStore.Get(ctx, gid)
		if err != nil {
			continue
		}
		tids, err := s.groupStore.TargetIDsForGroup(ctx, gid, nil)
		if err != nil {
			continue
		}
		var filtered []access.TargetID
		for _, tid := range tids {
			if allowedSet[tid] {
				filtered = append(filtered, tid)
			}
		}
		targets, err := s.targetStore.ListByIDs(ctx, filtered, nil)
		if err != nil {
			continue
		}
		var terminalOnly []*access.Target
		for _, t := range targets {
			if isCLITerminalProtocol(t.Protocol) {
				terminalOnly = append(terminalOnly, t)
			}
		}
		out = append(out, cliGroupEntry{Group: g, Targets: terminalOnly})
	}
	return out, nil
}

//nolint:gocyclo // menu has many commands and branches by design
func (s *Server) runMenu(ctx context.Context, channel ssh.Channel, userID string, ptyCols, ptyRows int, resizeChan <-chan sshproxy.TerminalSize) {
	defer sendExitStatus(channel, 0)
	rd := bufio.NewReader(channel)
	wr := channel
	prompt := func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(wr, format, args...)
	}

	// Single reader goroutine owns rd; all consumers read from inputCh.
	// This prevents races when StartRead and readLine would both call rd.Read.
	inputCh := make(chan byte, 4096)
	go func() {
		for {
			b, err := rd.ReadByte()
			if err != nil {
				close(inputCh)
				return
			}
			inputCh <- b
		}
	}()

	// CLI commands for Tab completion
	cliCommands := []string{"list", "ls", "cd", "pwd", "connect", "resume", "sessions", "help", "exit", "quit"}

	readLine := func(echo bool, promptForRedraw string) (string, error) {
		var line []byte
		eraseChar := func() {
			_, _ = wr.Write([]byte{0x08, ' ', 0x08}) // backspace, space, backspace
		}
		for {
			b, ok := <-inputCh
			if !ok {
				if len(line) > 0 {
					return strings.TrimSpace(string(line)), nil
				}
				return "", io.EOF
			}
			if b == '\n' || b == '\r' {
				if echo {
					_, _ = wr.Write([]byte{'\r', '\n'})
				}
				return strings.TrimSpace(string(line)), nil
			}
			// Backspace (0x7f) or BS (0x08). Never echo when the line is empty (would erase the prompt).
			if b == 0x7f || b == 0x08 {
				if len(line) > 0 {
					line = line[:len(line)-1]
					if echo {
						eraseChar()
					}
				}
				continue
			}
			// Tab completion (only when promptForRedraw is set, i.e. main vantyx:/ prompt)
			if b == '\t' && promptForRedraw != "" {
				start := 0
				for i := len(line) - 1; i >= 0; i-- {
					if line[i] == ' ' {
						start = i + 1
						break
					}
				}
				word := string(line[start:])
				word = strings.TrimSpace(word)
				word = strings.TrimFunc(word, func(r rune) bool { return r < 32 || r == 127 })
				var matches []string
				if word != "" {
					for _, c := range cliCommands {
						if strings.HasPrefix(c, strings.ToLower(word)) {
							matches = append(matches, c)
						}
					}
				}
				if len(matches) == 1 {
					completion := matches[0]
					line = append(line[:start], []byte(completion)...)
					if echo {
						for range word {
							eraseChar()
						}
						_, _ = wr.Write([]byte(completion))
					}
				} else if len(matches) > 1 {
					_, _ = wr.Write([]byte{'\r', '\n'})
					for _, m := range matches {
						prompt("  %s\r\n", m)
					}
					prompt("\r\n%s%s", promptForRedraw, line)
				} else if echo {
					_, _ = wr.Write([]byte{'\a'})
				}
				continue
			}
			if b == '\t' {
				continue
			}
			if b == 0x1b {
				for i := 0; i < 24; i++ {
					next, ok := <-inputCh
					if !ok {
						break
					}
					if next >= 0x40 && next <= 0x7e {
						break
					}
				}
				continue
			}
			if echo {
				_, _ = wr.Write([]byte{b})
			}
			line = append(line, b)
		}
	}

	// lastList: cached groups/targets for connect indices (1-based)
	var lastList []cliGroupEntry
	// currentGroupIndex: 1-based group index; 0 = root (no group selected)
	var currentGroupIndex int
	screenCols := ptyCols
	if screenCols < 40 {
		screenCols = 80
	}
	var pendingExtra []string
	setStatus := func(lines ...string) {
		pendingExtra = append(pendingExtra, lines...)
	}

	loadLastList := func() error {
		ctxList, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		entries, err := s.loadGroupsWithTerminalTargets(ctxList, userID)
		if err != nil {
			return err
		}
		lastList = entries
		return nil
	}

	redrawScreen := func(extraLines []string) {
		_, _ = wr.Write([]byte(cliClearScreen))
		_ = writeCLIScreen(wr, cliScreenState{
			Entries:           lastList,
			CurrentGroupIndex: currentGroupIndex,
			Cols:              screenCols,
		}, extraLines)
	}

	// activeSessionsForTargetIDs returns user's active sessions whose TargetID is in the set.
	activeSessionsForTargetIDs := func(targetIDSet map[string]bool) []*session.Session {
		var out []*session.Session
		if lister, ok := s.sessionManager.(SessionLister); ok {
			for _, id := range lister.ActiveIDs() {
				sess, ok := s.sessionManager.Get(id)
				if !ok || sess.UserID != userID {
					continue
				}
				if targetIDSet[sess.TargetID] {
					out = append(out, sess)
				}
			}
		}
		return out
	}

	activeSessionsForScope := func() []*session.Session {
		if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
			tidSet := make(map[string]bool)
			for _, t := range lastList[currentGroupIndex-1].Targets {
				tidSet[string(t.ID)] = true
			}
			return activeSessionsForTargetIDs(tidSet)
		}
		var out []*session.Session
		if lister, ok := s.sessionManager.(SessionLister); ok {
			for _, id := range lister.ActiveIDs() {
				if sess, ok := s.sessionManager.Get(id); ok && sess.UserID == userID {
					out = append(out, sess)
				}
			}
		}
		return out
	}

	var cliSessionMgr *session.Manager
	if m, ok := s.sessionManager.(*session.Manager); ok {
		cliSessionMgr = m
	}

	getPrompt := func() string {
		if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
			return "vantyx:/" + lastList[currentGroupIndex-1].Group.Name + "> "
		}
		return "vantyx:/> "
	}

	helpLines := func() []string {
		return []string{
			"=== Vantyx CLI ===",
			"The top of the screen shows PWD, Groups, and Hosts (after cd).",
			"Quick start: cd <group#>  →  connect <server#>",
			"             or connect <group#> <server#> from root",
			"",
			"Commands:",
			"  cd <n> | cd ..     Change group (updates header)",
			"  connect <n>        Connect (in group: server index n)",
			"  connect <g> <n>    Connect from root",
			"  ls                 Reload and refresh header",
			"  list               Show active sessions below header",
			"  resume [n]         Attach to background session",
			"  sessions           Same as list (active sessions)",
			"  In a session: Ctrl+] detach to menu; Ctrl+D end (connect only)",
			"  help | exit",
		}
	}

	if err := loadLastList(); err != nil {
		setStatus(fmt.Sprintf("Error: %v", err))
	}

	for {
		select {
		case sz, ok := <-resizeChan:
			if ok && sz.Cols > 0 {
				screenCols = sz.Cols
			}
		default:
		}

		if err := loadLastList(); err != nil {
			setStatus(fmt.Sprintf("Error: %v", err))
		}
		extra := pendingExtra
		pendingExtra = nil
		redrawScreen(extra)
		currentPrompt := getPrompt()
		prompt("%s", currentPrompt)
		line, err := readLine(true, currentPrompt)
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		// Trim control chars so "sessions\r" etc. is recognized
		cmd := strings.ToLower(strings.TrimSpace(parts[0]))
		cmd = strings.TrimFunc(cmd, func(r rune) bool { return r < 32 || r == 127 })
		args := parts[1:]

		switch cmd {
		case "exit", "quit":
			prompt("Bye.\r\n")
			return
		case "help":
			pendingExtra = helpLines()
			continue
		case "pwd":
			continue
		case "cd":
			if len(args) == 0 {
				continue
			}
			if args[0] == ".." || args[0] == "0" {
				currentGroupIndex = 0
				continue
			}
			n, _ := strconv.Atoi(args[0])
			if n < 1 || n > len(lastList) {
				setStatus(fmt.Sprintf("Invalid group index. Use 1-%d.", len(lastList)))
				continue
			}
			currentGroupIndex = n
			continue
		case "ls":
			setStatus("Refreshed.")
			continue
		case "list", "sessions":
			pendingExtra = formatCLIActiveSessionLines(activeSessionsForScope(), cliSessionMgr)
			continue
		case "resume":
			activeSessions := activeSessionsForScope()
			if len(activeSessions) == 0 {
				setStatus("No active sessions.")
				continue
			}
			if len(args) == 0 {
				pendingExtra = formatCLIActiveSessionLines(activeSessions, cliSessionMgr)
				pendingExtra = append(pendingExtra, "Use resume <n> to attach (e.g. resume 1).")
				continue
			}
			n, _ := strconv.Atoi(args[0])
			if n < 1 || n > len(activeSessions) {
				setStatus(fmt.Sprintf("Invalid index. Use 1-%d.", len(activeSessions)))
				continue
			}
			termSess := activeSessions[n-1]
			if cliSessionMgr != nil && cliSessionMgr.IsIdle(termSess) {
				setStatus("Warning: session has been idle for a long time (not auto-stopped).")
			}
			resumeStopCh := make(chan struct{})
			resumeDone := make(chan struct{})
			var resumeReadStarted bool
			resumeProto := access.ProtocolSSH
			if t, err := s.targetStore.Get(ctx, access.TargetID(termSess.TargetID)); err == nil {
				resumeProto = t.Protocol
			}
			targetName := termSess.TargetName
			if targetName == "" {
				targetName = "(unknown)"
			}
			frame, bridgeResize, err := s.prepareCLIFrame(wr, screenCols, ptyRows, targetName, resumeProto, false, resumeStopCh, resizeChan)
			if err != nil {
				setStatus(fmt.Sprintf("Error: %v", err))
				continue
			}
			defer func() { _ = frame.Leave() }()
			streamAttach := s.newCLIStreamAttach(resumeProto, frame.SessionWriter(), inputCh, resumeStopCh, &resumeReadStarted, resumeDone, nil, cliStreamAttachOpts{detachOnCtrlBracket: true}, nil)
			select {
			case termSess.AttachCh <- session.AttachReq{Conn: streamAttach}:
				select {
				case bridgeResize <- sshproxy.TerminalSize{Cols: frame.SessionCols(), Rows: frame.SessionRows()}:
				default:
				}
				select {
				case <-resumeDone:
				case <-termSess.Done():
					close(resumeStopCh)
					if resumeReadStarted {
						<-resumeDone
					}
				}
			default:
				setStatus("Session attach slot busy. Try again.")
			}
			continue
		case "connect":
			if len(lastList) == 0 {
				setStatus("No groups assigned.")
				continue
			}
			var gi, ti int
			if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) && len(args) == 1 {
				gi = currentGroupIndex
				ti, _ = strconv.Atoi(args[0])
			} else if len(args) == 1 && strings.Contains(args[0], ".") {
				dot := strings.Index(args[0], ".")
				gi, _ = strconv.Atoi(strings.TrimSpace(args[0][:dot]))
				ti, _ = strconv.Atoi(strings.TrimSpace(args[0][dot+1:]))
			} else if len(args) >= 2 {
				gi, _ = strconv.Atoi(args[0])
				ti, _ = strconv.Atoi(args[1])
			} else {
				if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
					setStatus("Usage: connect <server index> (e.g. connect 1)")
				} else {
					setStatus("Usage: connect <group> <server> or cd <group> then connect <server>")
				}
				continue
			}
			if gi < 1 || gi > len(lastList) {
				setStatus(fmt.Sprintf("Invalid group index. Use 1-%d.", len(lastList)))
				continue
			}
			e := &lastList[gi-1]
			if ti < 1 || ti > len(e.Targets) {
				if len(e.Targets) == 0 {
					setStatus("No SSH/Telnet servers in this group.")
				} else {
					setStatus(fmt.Sprintf("Invalid server index. Group has servers 1-%d.", len(e.Targets)))
				}
				continue
			}
			target := e.Targets[ti-1]

			// Session name and description (optional)
			prompt("Session name (optional): ")
			sessionName, _ := readLine(true, "")
			sessionName = strings.TrimSpace(sessionName)
			prompt("Description (optional): ")
			sessionDesc, _ := readLine(true, "")
			sessionDesc = strings.TrimSpace(sessionDesc)

			if target.Protocol == access.ProtocolSSH && target.SSHPrivateKey != "" && strings.HasPrefix(target.SSHPrivateKey, secret.CiphertextVersionPrefix) {
				setStatus("Saved credentials could not be decrypted. Check VANTYX_ENCRYPTION_KEY.")
				continue
			}
			var targetUser, targetPass string
			if target.SSHUsername != "" {
				targetUser = target.SSHUsername
				targetPass = target.SSHPassword
				prompt("\r\nUsing stored credentials for %s.\r\n\r\nConnecting to %s...\r\n", target.Name, target.Name)
			} else {
				prompt("\r\nTarget username for %s: ", target.Name)
				targetUser, err = readLine(true, "")
				if err != nil {
					return
				}
				if targetUser == "" {
					setStatus("Username required.")
					continue
				}
				prompt("Target password: ")
				targetPass, err = readLine(false, "")
				if err != nil {
					return
				}
				prompt("\r\nConnecting to %s...\r\n", target.Name)
			}

			connectStopCh := make(chan struct{})
			frame, bridgeResize, err := s.prepareCLIFrame(wr, screenCols, ptyRows, target.Name, target.Protocol, true, connectStopCh, resizeChan)
			if err != nil {
				setStatus(fmt.Sprintf("Error: %v", err))
				continue
			}
			defer func() { _ = frame.Leave() }()

			sessionID := session.ID(time.Now().UTC().Format(time.RFC3339Nano))
			bridgeDone := make(chan struct{})
			readDone := make(chan struct{})
			var readStarted bool
			var bridgeErr error
			var attachOutcome cliAttachOutcome
			sessionCols := frame.SessionCols()
			sessionRows := frame.SessionRows()
			opts := session.StartOptions{
				UserID:      userID,
				TargetID:    string(target.ID),
				TargetName:  target.Name,
				Name:        sessionName,
				Description: sessionDesc,
			}
			_, err = s.sessionManager.Start(sessionID, opts, func(bridgeCtx context.Context, sess *session.Session) {
				defer close(bridgeDone)
				touch := func() { s.sessionManager.Touch(sessionID) }
				streamAttach := s.newCLIStreamAttach(target.Protocol, frame.SessionWriter(), inputCh, connectStopCh, &readStarted, readDone, nil, cliStreamAttachOpts{endOnCtrlD: true, detachOnCtrlBracket: true}, &attachOutcome)
				select {
				case bridgeResize <- sshproxy.TerminalSize{Cols: sessionCols, Rows: sessionRows}:
				default:
				}
				var tee io.Writer
				var stdinRecorder sshproxy.StdinRecorder
				if s.recordingDir != "" && s.recordingStore != nil {
					_ = os.MkdirAll(s.recordingDir, 0750)
					safeName := strings.ReplaceAll(string(sessionID), ":", "-")
					safeName = strings.ReplaceAll(safeName, ".", "-")
					castPath := filepath.Join(s.recordingDir, safeName+".cast")
					f, createErr := os.Create(castPath)
					if createErr != nil {
						slog.Warn("CLI recording create failed", "session_id", sessionID, "path", castPath, "error", createErr)
					} else {
						w, h := ptyCols, ptyRows
						if w <= 0 {
							w = 80
						}
						if h <= 0 {
							h = 24
						}
						asc := recording.NewAsciinemaWriter(f, w, h)
						tee = asc
						stdinRecorder = asc
						startedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
						if insertErr := s.recordingStore.InsertRecording(bridgeCtx, string(sessionID), userID, string(target.ID), string(sessionID), "cli", castPath, startedAt, sessionName, sessionDesc); insertErr != nil {
							slog.Warn("CLI recording insert failed", "session_id", sessionID, "error", insertErr)
						}
						defer func() {
							_ = f.Sync()
							_ = f.Close()
							_ = s.recordingStore.UpdateRecordingEnded(context.Background(), string(sessionID), time.Now().UTC().Format("2006-01-02 15:04:05"))
						}()
					}
				}
				switch target.Protocol {
				case access.ProtocolTelnet:
					var telStdin telnetproxy.StdinRecorder
					if stdinRecorder != nil {
						telStdin = telnetproxy.StdinRecorderFunc(stdinRecorder.RecordInput)
					}
					bridgeErr = telnetproxy.RunBridgeDetachable(bridgeCtx, target.Host, target.Port, targetUser, targetPass, sess.Output, sess.AttachCh, streamAttach, touch, tee, telStdin, sessionCols, sessionRows, bridgeResize)
				default:
					bridgeErr = sshproxy.RunBridgeDetachable(bridgeCtx, target.Host, target.Port, targetUser, targetPass, target.SSHPrivateKey, target.SSHPrivateKeyPassphrase, sess.Output, sess.AttachCh, streamAttach, touch, tee, stdinRecorder, sessionCols, sessionRows, bridgeResize)
				}
			})
			if err != nil {
				setStatus(fmt.Sprintf("Session start failed: %v", err))
				continue
			}
			attachWait := time.Now().Add(3 * time.Second)
			for time.Now().Before(attachWait) && !readStarted {
				time.Sleep(10 * time.Millisecond)
			}
			sessionEnded := false
			clientDetached := false
			select {
			case <-readDone:
				if attachOutcome.endedSession() {
					sessionEnded = true
					if stopper, ok := s.sessionManager.(SessionStopper); ok {
						stopper.Stop(sessionID)
					}
				} else {
					clientDetached = true
				}
			case <-bridgeDone:
			}
			close(connectStopCh)
			if readStarted {
				select {
				case <-readDone:
				default:
				}
			}
			if sessionEnded {
				select {
				case <-bridgeDone:
				default:
					<-bridgeDone
				}
			} else if !clientDetached {
				select {
				case <-bridgeDone:
				default:
					<-bridgeDone
				}
			}
			switch {
			case clientDetached:
				setStatus("Detached. Session continues in background.")
			case bridgeErr != nil:
				setStatus(fmt.Sprintf("Disconnected: %s", proxyerrors.BridgeErrorMessage(bridgeErr)))
			default:
				setStatus(fmt.Sprintf("Disconnected from %s.", target.Name))
			}
			continue
		default:
			setStatus(fmt.Sprintf("Unknown command '%s'. Type 'help' for commands.", cmd))
		}
	}
}
