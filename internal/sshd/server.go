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
	targetStore   access.TargetStore
	groupStore    access.AccessGroupStore
	sessionManager SessionStarter
	config        *ssh.ServerConfig
	listener      net.Listener
	mu            sync.Mutex
	shutdown      bool
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
	r.Seek(int64(4+termLen), io.SeekStart)
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

func (s *Server) runMenu(ctx context.Context, channel ssh.Channel, userID string, ptyCols, ptyRows int, resizeChan <-chan sshproxy.TerminalSize) {
	rd := bufio.NewReader(channel)
	wr := channel
	prompt := func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(wr, format, args...)
	}
	// readLine reads until \n or \r. When echo is true, echoes each character so the user sees input (for clients that do not local-echo).
	readLine := func(echo bool) (string, error) {
		var line []byte
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
			if echo {
				_, _ = wr.Write([]byte{b})
			}
			line = append(line, b)
		}
	}

	for {
		ctxList, cancelList := context.WithTimeout(ctx, 30*time.Second)
		targetIDs, err := s.groupStore.TargetIDsForUser(ctxList, access.UserID(userID), nil)
		if err != nil {
			cancelList()
			prompt("Error listing targets: %v\r\n", err)
			continue
		}
		if len(targetIDs) == 0 {
			cancelList()
			prompt("No targets assigned. Contact your administrator.\r\n")
			return
		}
		targets, err := s.targetStore.ListByIDs(ctxList, targetIDs, nil)
		cancelList()
		if err != nil || len(targets) == 0 {
			prompt("No targets available.\r\n")
			continue
		}
		var sshTargets []*access.Target
		for _, t := range targets {
			if t.Protocol == access.ProtocolSSH {
				sshTargets = append(sshTargets, t)
			}
		}
		if len(sshTargets) == 0 {
			prompt("No SSH targets available.\r\n")
			continue
		}

		prompt("\r\n=== Vantyx CLI ===\r\n")
		var activeSessions []*session.Session
		if lister, ok := s.sessionManager.(SessionLister); ok {
			for _, id := range lister.ActiveIDs() {
				if sess, ok := s.sessionManager.Get(id); ok && sess.UserID == userID {
					activeSessions = append(activeSessions, sess)
				}
			}
		}
		if len(activeSessions) > 0 {
			prompt("  R. Resume existing session\r\n")
		}
		for i, t := range sshTargets {
			prompt("  %d. %s (%s:%d)\r\n", i+1, t.Name, t.Host, t.Port)
		}
		prompt("  0. Exit\r\n\r\n")
		prompt("Select target (0-%d%s): ", len(sshTargets), map[bool]string{true: " or R", false: ""}[len(activeSessions) > 0])
		choice, err := readLine(true)
		if err != nil {
			return
		}
		choiceUpper := strings.ToUpper(strings.TrimSpace(choice))
		if choiceUpper == "R" && len(activeSessions) > 0 {
			prompt("\r\nResume session:\r\n")
			for i, sess := range activeSessions {
				prompt("  %d. %s (%s)\r\n", i+1, sess.TargetName, sess.TargetID)
			}
			prompt("  0. Cancel\r\n\r\n")
			prompt("Select (0-%d): ", len(activeSessions))
			resumeChoice, err := readLine(true)
			if err != nil {
				return
			}
			rn, _ := strconv.Atoi(resumeChoice)
			if rn < 1 || rn > len(activeSessions) {
				continue
			}
			termSess := activeSessions[rn-1]
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
		}
		n, _ := strconv.Atoi(choice)
		if n == 0 {
			prompt("Bye.\r\n")
			return
		}
		if n < 1 || n > len(sshTargets) {
			prompt("Invalid choice.\r\n")
			continue
		}
		target := sshTargets[n-1]

		prompt("Target username for %s: ", target.Name)
		targetUser, err := readLine(true)
		if err != nil {
			return
		}
		if targetUser == "" {
			prompt("Username required.\r\n")
			continue
		}
		prompt("Target password: ")
		targetPass, err := readLine(false)
		if err != nil {
			return
		}
		prompt("\r\nConnecting to %s...\r\n", target.Name)

		sessionID := session.ID(time.Now().UTC().Format(time.RFC3339Nano))
		bridgeDone := make(chan struct{})
		var bridgeErr error
		opts := session.StartOptions{
			UserID:     userID,
			TargetID:   string(target.ID),
			TargetName: target.Name,
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
			bridgeErr = sshproxy.RunBridgeDetachable(bridgeCtx, target.Host, target.Port, targetUser, targetPass, sess.Output, sess.AttachCh, streamAttach, touch)
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
	}
}
