package mock

import (
	"bufio"
	"net"
	"sync"
)

// Telnet IAC and commands (RFC 854)
const (
	IAC  = 255
	WILL = 251
	WONT = 252
	DO   = 253
	DONT = 254
	SB   = 250
	SE   = 240
)

// TelnetEchoServer is a minimal Telnet server that performs basic IAC negotiation and echoes data.
type TelnetEchoServer struct {
	listener net.Listener
	mu       sync.Mutex
}

// NewTelnetEchoServer creates a new Telnet echo server (call Start to listen).
func NewTelnetEchoServer() *TelnetEchoServer {
	return &TelnetEchoServer{}
}

// Start listens on 127.0.0.1:0 and accepts connections, handling each with echo + IAC.
func (s *TelnetEchoServer) Start() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	go s.acceptLoop()
	return nil
}

func (s *TelnetEchoServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *TelnetEchoServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := conn
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if err != nil {
			return
		}
		out := s.processTelnet(buf[:n])
		if len(out) > 0 {
			_, _ = w.Write(out)
		}
	}
}

// processTelnet strips or replies to IAC sequences and returns data to echo.
func (s *TelnetEchoServer) processTelnet(b []byte) []byte {
	var out []byte
	i := 0
	for i < len(b) {
		if b[i] != IAC {
			out = append(out, b[i])
			i++
			continue
		}
		i++
		if i >= len(b) {
			break
		}
		cmd := b[i]
		i++
		switch cmd {
		case DO, DONT:
			if i < len(b) {
				// option byte
				opt := b[i]
				i++
				if cmd == DO {
					// Reply WILL or WONT for the option
					out = append(out, IAC, WONT, opt)
				}
			}
		case WILL, WONT:
			if i < len(b) {
				i++ // skip option
			}
		case SB:
			for i < len(b) && b[i] != IAC {
				i++
			}
			if i+1 < len(b) && b[i] == IAC && b[i+1] == SE {
				i += 2
			}
		default:
			// single-byte command, no reply
		}
	}
	return out
}

// Addr returns the listener address.
func (s *TelnetEchoServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Port returns the port number (0 if not started).
func (s *TelnetEchoServer) Port() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return 0
	}
	addr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return uint16(addr.Port)
}

// Close stops the listener.
func (s *TelnetEchoServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	err := s.listener.Close()
	s.listener = nil
	return err
}

// TelnetReadEcho reads until at least wantLen bytes of non-IAC data or timeout/error.
func TelnetReadEcho(conn net.Conn, wantLen int, buf []byte) ([]byte, error) {
	var out []byte
	if buf == nil {
		buf = make([]byte, 256)
	}
	for len(out) < wantLen {
		n, err := conn.Read(buf)
		if n > 0 {
			out = append(out, skipIAC(buf[:n])...)
		}
		if err != nil {
			return out, err
		}
		if len(out) >= wantLen {
			return out[:wantLen], nil
		}
	}
	return out, nil
}

func skipIAC(b []byte) []byte {
	var out []byte
	i := 0
	for i < len(b) {
		if b[i] != IAC {
			out = append(out, b[i])
			i++
			continue
		}
		i++
		if i >= len(b) {
			break
		}
		cmd := b[i]
		i++
		switch cmd {
		case DO, DONT, WILL, WONT:
			if i < len(b) {
				i++
			}
		case SB:
			for i < len(b) && b[i] != IAC {
				i++
			}
			if i+1 < len(b) && b[i] == IAC && b[i+1] == SE {
				i += 2
			}
		default:
		}
	}
	return out
}
