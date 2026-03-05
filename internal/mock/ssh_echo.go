package mock

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SSHEchoServer is a minimal SSH server that accepts one connection and echoes stdin to stdout.
type SSHEchoServer struct {
	User     string
	Password string

	listener net.Listener
	config   *ssh.ServerConfig
	mu       sync.Mutex
}

// NewSSHEchoServer creates an SSH server config that accepts the given user/password and echoes session data.
func NewSSHEchoServer(user, password string) (*SSHEchoServer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, err
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)

	return &SSHEchoServer{
		User:     user,
		Password: password,
		config:   config,
	}, nil
}

// Start listens on 127.0.0.1:0 and accepts one connection, handling it in a goroutine.
func (s *SSHEchoServer) Start() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	go s.acceptOne()
	return nil
}

func (s *SSHEchoServer) acceptOne() {
	nconn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer nconn.Close()

	_, chans, reqs, err := ssh.NewServerConn(nconn, s.config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, inReqs, _ := newCh.Accept()
		go func() {
			for r := range inReqs {
				if r.WantReply {
					_ = r.Reply(true, nil)
				}
			}
		}()
		go func() {
			defer ch.Close()
			buf := make([]byte, 4096)
			for {
				n, err := ch.Read(buf)
				if n > 0 {
					_, _ = ch.Write(buf[:n])
				}
				if err != nil {
					return
				}
			}
		}()
	}
}

// Addr returns the listener address (e.g. "127.0.0.1:12345").
func (s *SSHEchoServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Port returns the port number (0 if not started).
func (s *SSHEchoServer) Port() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return 0
	}
	addr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	p := addr.Port
	if p < 0 || p > 65535 {
		return 0
	}
	return uint16(p)
}

// Close stops the listener.
func (s *SSHEchoServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	err := s.listener.Close()
	s.listener = nil
	return err
}
