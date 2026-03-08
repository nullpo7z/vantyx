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
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
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
}

// Config holds dependencies for the SSH server.
type Config struct {
	UserStore      auth.UserStore
	TargetStore    access.TargetStore
	GroupStore     access.AccessGroupStore
	SessionManager SessionStarter
	// HostKey is the SSH host private key. If nil, a new 2048-bit RSA key is generated (not persisted).
	HostKey ssh.Signer
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
	}, nil
}

// ListenAndServe listens on addr and serves CLI SSH until Shutdown or error.
func (s *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	slog.Info("sshd listening", "addr", addr)
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

// cliGroupEntry is one group and its SSH targets for CLI list/connect.
type cliGroupEntry struct {
	Group   *access.AccessGroup
	Targets []*access.Target
}

// loadGroupsWithSSHTargets returns groups the user can see, each with its SSH targets (same logic as API handleGroups).
func (s *Server) loadGroupsWithSSHTargets(ctx context.Context, userID string) ([]cliGroupEntry, error) {
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
		var sshOnly []*access.Target
		for _, t := range targets {
			if t.Protocol == access.ProtocolSSH {
				sshOnly = append(sshOnly, t)
			}
		}
		if len(sshOnly) > 0 {
			out = append(out, cliGroupEntry{Group: g, Targets: sshOnly})
		}
	}
	return out, nil
}

func (s *Server) runMenu(ctx context.Context, channel ssh.Channel, userID string, ptyCols, ptyRows int, resizeChan <-chan sshproxy.TerminalSize) {
	defer sendExitStatus(channel, 0)
	rd := bufio.NewReader(channel)
	wr := channel
	prompt := func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(wr, format, args...)
	}
	// CLI commands for Tab completion
	cliCommands := []string{"list", "ls", "cd", "pwd", "connect", "resume", "sessions", "help", "exit", "quit"}

	readLine := func(echo bool, promptForRedraw string) (string, error) {
		var line []byte
		eraseChar := func() {
			_, _ = wr.Write([]byte{0x08, ' ', 0x08}) // backspace, space, backspace
		}
		for {
			b, err := rd.ReadByte()
			if err != nil {
				if len(line) > 0 {
					return strings.TrimSpace(string(line)), nil
				}
				return "", err
			}
			if b == '\n' || b == '\r' {
				if echo {
					_, _ = wr.Write([]byte{'\r', '\n'})
				}
				return strings.TrimSpace(string(line)), nil
			}
			// Backspace (0x7f) or BS (0x08)
			if (b == 0x7f || b == 0x08) && len(line) > 0 {
				line = line[:len(line)-1]
				if echo {
					eraseChar()
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
				// Normalize: trim spaces and control chars so "sess\r" still matches "sessions"
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
				// Tab in username/password field: ignore
				continue
			}
			// Swallow ANSI/escape sequences so they don't end up in the line (fixes "need to type twice" with some clients)
			if b == 0x1b {
				for i := 0; i < 24; i++ {
					next, err := rd.ReadByte()
					if err != nil {
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

	// lastList: result of last "list" for connect indices (1-based)
	var lastList []cliGroupEntry
	// currentGroupIndex: 1-based group index; 0 = root (no group selected)
	var currentGroupIndex int

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

	getPrompt := func() string {
		if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
			return "vantyx:/" + lastList[currentGroupIndex-1].Group.Name + "> "
		}
		return "vantyx:/> "
	}

	printHelp := func() {
		prompt("Commands:\r\n")
		prompt("  ls               List names only (at root: groups; in group: servers)\r\n")
		prompt("  list             Show groups and servers with indices and active sessions\r\n")
		prompt("  cd [n]           Move to group n (1-based); cd .. or cd 0 = root\r\n")
		prompt("  pwd              Show current group\r\n")
		prompt("  connect <t>      Connect to server index t in current group (after cd)\r\n")
		prompt("  connect <g> <t>  Connect to group g, server t (when at root)\r\n")
		prompt("  resume [n]      List active sessions; resume n to attach\r\n")
		prompt("  sessions         List active sessions for current group (or all if root)\r\n")
		prompt("  help             Show this help\r\n")
		prompt("  exit, quit       Disconnect\r\n\r\n")
	}

	prompt("\r\n=== Vantyx CLI ===\r\n")
	printHelp()

	for {
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
			printHelp()
			continue
		case "pwd":
			if currentGroupIndex < 1 || currentGroupIndex > len(lastList) {
				prompt("(root)\r\n")
			} else {
				prompt("%s\r\n", lastList[currentGroupIndex-1].Group.Name)
			}
			continue
		case "cd":
			if len(lastList) == 0 {
				ctxList, cancel := context.WithTimeout(ctx, 2*time.Minute)
				entries, err := s.loadGroupsWithSSHTargets(ctxList, userID)
				cancel()
				if err != nil {
					prompt("Error: %v\r\n", err)
					continue
				}
				lastList = entries
			}
			if len(args) == 0 {
				if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
					prompt("%s\r\n", lastList[currentGroupIndex-1].Group.Name)
				} else {
					prompt("(root)\r\n")
				}
				continue
			}
			if args[0] == ".." || args[0] == "0" {
				currentGroupIndex = 0
				prompt("(root)\r\n")
				continue
			}
			n, _ := strconv.Atoi(args[0])
			if n < 1 || n > len(lastList) {
				prompt("Invalid group index. Use 1-%d (run 'list' to see indices).\r\n", len(lastList))
				continue
			}
			currentGroupIndex = n
			prompt("%s\r\n", lastList[currentGroupIndex-1].Group.Name)
			continue
		case "ls":
			ctxList, cancel := context.WithTimeout(ctx, 2*time.Minute)
			entries, err := s.loadGroupsWithSSHTargets(ctxList, userID)
			cancel()
			if err != nil {
				prompt("Error: %v\r\n", err)
				continue
			}
			lastList = entries
			if len(entries) == 0 {
				prompt("\r\n")
				continue
			}
			if currentGroupIndex >= 1 && currentGroupIndex <= len(entries) {
				for i, t := range entries[currentGroupIndex-1].Targets {
					prompt("host %d %s\r\n", i+1, t.Name)
				}
			} else {
				for i, e := range entries {
					prompt("group %d %s\r\n", i+1, e.Group.Name)
				}
			}
			continue
		case "list":
			ctxList, cancel := context.WithTimeout(ctx, 2*time.Minute)
			entries, err := s.loadGroupsWithSSHTargets(ctxList, userID)
			cancel()
			if err != nil {
				prompt("Error: %v\r\n", err)
				continue
			}
			lastList = entries
			if len(entries) == 0 {
				prompt("No groups or servers assigned.\r\n")
				continue
			}
			for gi, e := range entries {
				cur := ""
				if gi+1 == currentGroupIndex {
					cur = " *"
				}
				prompt("  [%d] %s%s\r\n", gi+1, e.Group.Name, cur)
				for ti, t := range e.Targets {
					prompt("    [%d] %s (%s:%d)\r\n", ti+1, t.Name, t.Host, t.Port)
				}
				tidSet := make(map[string]bool)
				for _, t := range e.Targets {
					tidSet[string(t.ID)] = true
				}
				activeSess := activeSessionsForTargetIDs(tidSet)
				for i, sess := range activeSess {
					name := sess.Name
					if name == "" {
						name = "(no name)"
					}
					desc := sess.Description
					if desc != "" {
						prompt("    Active [%d] %s - %s (%s)\r\n", i+1, name, desc, sess.TargetName)
					} else {
						prompt("    Active [%d] %s (%s)\r\n", i+1, name, sess.TargetName)
					}
				}
			}
			if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
				prompt("\r\nCurrent group: %s. Use connect <server index> to connect.\r\n\r\n", lastList[currentGroupIndex-1].Group.Name)
			} else {
				prompt("\r\nUse cd <group> then connect <server>, or connect <group> <server>.\r\n\r\n")
			}
			continue
		case "sessions":
			var activeSessions []*session.Session
			if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
				tidSet := make(map[string]bool)
				for _, t := range lastList[currentGroupIndex-1].Targets {
					tidSet[string(t.ID)] = true
				}
				activeSessions = activeSessionsForTargetIDs(tidSet)
			} else {
				if lister, ok := s.sessionManager.(SessionLister); ok {
					for _, id := range lister.ActiveIDs() {
						if sess, ok := s.sessionManager.Get(id); ok && sess.UserID == userID {
							activeSessions = append(activeSessions, sess)
						}
					}
				}
			}
			if len(activeSessions) == 0 {
				prompt("No active sessions.\r\n")
				continue
			}
			for i, sess := range activeSessions {
				name := sess.Name
				if name == "" {
					name = "(no name)"
				}
				if sess.Description != "" {
					prompt("  [%d] %s - %s | %s (%s)\r\n", i+1, name, sess.Description, sess.TargetName, sess.TargetID)
				} else {
					prompt("  [%d] %s | %s (%s)\r\n", i+1, name, sess.TargetName, sess.TargetID)
				}
			}
			prompt("Use resume <n> to attach.\r\n\r\n")
			continue
		case "resume":
			var activeSessions []*session.Session
			if currentGroupIndex >= 1 && currentGroupIndex <= len(lastList) {
				tidSet := make(map[string]bool)
				for _, t := range lastList[currentGroupIndex-1].Targets {
					tidSet[string(t.ID)] = true
				}
				activeSessions = activeSessionsForTargetIDs(tidSet)
			} else {
				if lister, ok := s.sessionManager.(SessionLister); ok {
					for _, id := range lister.ActiveIDs() {
						if sess, ok := s.sessionManager.Get(id); ok && sess.UserID == userID {
							activeSessions = append(activeSessions, sess)
						}
					}
				}
			}
			if len(activeSessions) == 0 {
				prompt("No active sessions.\r\n")
				continue
			}
			if len(args) == 0 {
				for i, sess := range activeSessions {
					name := sess.Name
					if name == "" {
						name = "(no name)"
					}
					if sess.Description != "" {
						prompt("  [%d] %s - %s | %s\r\n", i+1, name, sess.Description, sess.TargetName)
					} else {
						prompt("  [%d] %s | %s\r\n", i+1, name, sess.TargetName)
					}
				}
				prompt("Use resume <n> to attach (e.g. resume 1).\r\n\r\n")
				continue
			}
			n, _ := strconv.Atoi(args[0])
			if n < 1 || n > len(activeSessions) {
				prompt("Invalid index. Use 1-%d.\r\n", len(activeSessions))
				continue
			}
			termSess := activeSessions[n-1]
			done := make(chan struct{})
			streamAttach := &sshproxy.StreamAttach{
				Write: func(p []byte) error { _, err := wr.Write(p); return err },
				StartRead: func(stdinCh chan<- []byte, onClose func()) {
					go func() {
						defer func() { onClose(); close(done) }()
						buf := make([]byte, 4096)
						for {
							n, err := rd.Read(buf)
							if n > 0 {
								select {
								case stdinCh <- buf[:n]:
								case <-ctx.Done():
									return
								}
							}
							if err != nil {
								return
							}
						}
					}()
				},
				CloseFn: func() error { return channel.Close() },
			}
			select {
			case termSess.AttachCh <- session.AttachReq{Conn: streamAttach}:
				prompt("\r\nAttached. Disconnect with Ctrl+C or close the connection.\r\n\r\n")
				<-done
			default:
				prompt("Session attach slot busy. Try again.\r\n")
			}
			continue
		case "connect":
			if len(lastList) == 0 {
				ctxList, cancel := context.WithTimeout(ctx, 2*time.Minute)
				entries, err := s.loadGroupsWithSSHTargets(ctxList, userID)
				cancel()
				if err != nil {
					prompt("Error: %v\r\n", err)
					continue
				}
				lastList = entries
			}
			if len(lastList) == 0 {
				prompt("No groups or servers assigned.\r\n")
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
					prompt("Usage: connect <server index> (e.g. connect 1)\r\n")
				} else {
					prompt("Usage: connect <group> <server> or cd <group> then connect <server>\r\n")
				}
				continue
			}
			if gi < 1 || gi > len(lastList) {
				prompt("Invalid group index. Use 1-%d (run 'list' to see indices).\r\n", len(lastList))
				continue
			}
			e := &lastList[gi-1]
			if ti < 1 || ti > len(e.Targets) {
				prompt("Invalid server index. Group has servers 1-%d.\r\n", len(e.Targets))
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
					prompt("Username required.\r\n")
					continue
				}
				prompt("Target password: ")
				targetPass, err = readLine(false, "")
				if err != nil {
					return
				}
				prompt("\r\nConnecting to %s...\r\n", target.Name)
			}

			sessionID := session.ID(time.Now().UTC().Format(time.RFC3339Nano))
			bridgeDone := make(chan struct{})
			var bridgeErr error
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
				streamAttach := &sshproxy.StreamAttach{
					Write: func(p []byte) error { _, e := wr.Write(p); return e },
					StartRead: func(stdinCh chan<- []byte, onClose func()) {
						go func() {
							defer onClose()
							buf := make([]byte, 4096)
							for {
								n, err := rd.Read(buf)
								if n > 0 {
									select {
									case stdinCh <- buf[:n]:
									case <-bridgeCtx.Done():
										return
									}
								}
								if err != nil {
									return
								}
							}
						}()
					},
					CloseFn: func() error { return channel.Close() },
				}
				bridgeErr = sshproxy.RunBridgeDetachable(bridgeCtx, target.Host, target.Port, targetUser, targetPass, sess.Output, sess.AttachCh, streamAttach, touch, nil, nil, 0, 0)
			})
			if err != nil {
				prompt("Session start failed: %v\r\n", err)
				continue
			}
			<-bridgeDone
			if bridgeErr != nil {
				prompt("\r\nDisconnected: %v\r\n", bridgeErr)
			} else {
				prompt("\r\nDisconnected from %s.\r\n", target.Name)
			}
			continue
		default:
			prompt("Unknown command '%s'. Type 'help' for commands.\r\n", cmd)
		}
	}
}
