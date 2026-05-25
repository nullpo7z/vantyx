package sshd

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// SessionStarter starts and touches terminal sessions in the same way
// as the browser terminal pipeline. [Manager] in internal/session is
// the canonical implementation; tests can supply lighter doubles.
//
// ListSessions is optional: when implemented (see [SessionLister]) the
// CLI exposes a "resume" menu.
type SessionStarter interface {
	Start(id session.ID, opts session.StartOptions, fn func(context.Context, *session.Session)) (*session.Session, error)
	Get(id session.ID) (*session.Session, bool)
	Touch(id session.ID)
}

// SessionLister is the optional interface implemented by
// [*session.Manager] that powers the CLI resume menu.
type SessionLister interface {
	ActiveIDs() []session.ID
}

// SessionStopper is the optional interface implemented by
// [*session.Manager] that lets the CLI end background bridges.
type SessionStopper interface {
	Stop(id session.ID)
}

// RecordingStore persists recording metadata when CLI sessions are
// recorded. Both methods are no-ops when recording is disabled.
type RecordingStore interface {
	InsertRecording(ctx context.Context, id, userID, targetID, sessionID, channelType, filePath, startedAt, sessionName, sessionDesc string) error
	UpdateRecordingEnded(ctx context.Context, id, endedAt string) error
}

// Server is the CLI SSH gateway: users log in with Vantyx credentials,
// then choose a target to proxy to via the text menu in [Server.runMenu].
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

// Config holds the dependencies needed to build a [Server].
type Config struct {
	UserStore      auth.UserStore
	TargetStore    access.TargetStore
	GroupStore     access.AccessGroupStore
	SessionManager SessionStarter
	// HostKey is the SSH host private key. If nil, a fresh 2048-bit RSA
	// key is generated (not persisted across restarts).
	HostKey ssh.Signer
	// RecordingsDir enables asciinema recording for CLI connect sessions
	// when set (typically from VANTYX_RECORDINGS_DIR). RecordingStore
	// must also be set to persist metadata.
	RecordingsDir  string
	RecordingStore RecordingStore
}

// NewServer builds an SSH server that authenticates with cfg.UserStore
// and proxies to targets via cfg.GroupStore / cfg.TargetStore.
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

// ListenAndServe listens on addr and serves CLI SSH until [Server.Shutdown]
// is called or a fatal error occurs.
func (s *Server) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(listener)
}

// Serve accepts connections from listener and serves CLI SSH until
// [Server.Shutdown] is called or the listener is closed. Tests may pass
// a listener created via `net.Listen("tcp", "127.0.0.1:0")`.
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

// Addr returns the listener's address while the server is serving,
// otherwise nil. Useful in tests that need the address after
// ListenAndServe has started.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

// Shutdown closes the listener so [Server.Serve] returns.
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

// handleConn handles a single accepted TCP connection: it performs the
// SSH handshake, dispatches the "session" channel, and then runs the
// CLI menu inside that channel.
func (s *Server) handleConn(nconn net.Conn) {
	defer nconn.Close()
	// Long timeout so menu and DB listing are not cut off (handshake +
	// channel open can be slow on cold caches).
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
		go s.serveChannel(ctx, channel, inReqs, userID, &sessionDone)
		break
	}
	sessionDone.Wait()
}

// serveChannel drives the request loop for a single SSH session
// channel: it dispatches pty-req / window-change / shell requests and
// hands off to [Server.runMenu] once the shell starts.
func (s *Server) serveChannel(ctx context.Context, channel ssh.Channel, inReqs <-chan *ssh.Request, userID string, sessionDone *sync.WaitGroup) {
	defer sessionDone.Done()
	defer channel.Close()
	var ptyCols, ptyRows int
	resizeChan := make(chan sshproxy.TerminalSize, 8)
	// menuReturned is non-nil once the shell has started; it is closed
	// when runMenu returns (e.g. the user typed "exit").
	var menuReturned chan struct{}
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
				handleSessionRequest(r, &ptyCols, &ptyRows, resizeChan)
			}
		} else {
			r, ok := <-inReqs
			if !ok {
				return
			}
			handleSessionRequest(r, &ptyCols, &ptyRows, resizeChan)
			if r.Type == "shell" {
				menuReturned = make(chan struct{})
				go func() {
					s.runMenu(ctx, channel, userID, ptyCols, ptyRows, resizeChan)
					close(menuReturned)
				}()
			}
		}
	}
}

// handleSessionRequest dispatches a single SSH session request. It
// updates ptyCols / ptyRows when the client sends pty-req or
// window-change, forwards window changes to resizeChan (best-effort,
// dropping the oldest entry when the channel is full), and replies
// when the client requests an acknowledgement.
func handleSessionRequest(r *ssh.Request, ptyCols, ptyRows *int, resizeChan chan sshproxy.TerminalSize) {
	accept := r.Type == "shell" || r.Type == "pty-req" || r.Type == "window-change"
	if r.Type == "pty-req" {
		if c, row, ok := parsePtyReqPayload(r.Payload); ok {
			*ptyCols, *ptyRows = c, row
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
