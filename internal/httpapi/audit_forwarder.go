package httpapi

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type auditForwarderConfig struct {
	Enabled bool   `json:"enabled"`
	Proto   string `json:"proto"`
	Addr    string `json:"addr"`
	App     string `json:"app"`
	Buffer  int    `json:"buffer"`
}

// auditForwarder asynchronously forwards audit JSONL lines to an external SIEM endpoint.
// It is best-effort: drops logs on backpressure and never blocks request handling.
type auditForwarder struct {
	network string
	addr    string
	appName string
	host    string

	ch   chan []byte
	done chan struct{}

	mu   sync.Mutex
	conn net.Conn
}

func newAuditForwarder(cfg auditForwarderConfig) *auditForwarder {
	if !cfg.Enabled {
		return nil
	}
	network := strings.TrimSpace(cfg.Proto)
	if network == "" {
		network = "udp"
	}
	addr := strings.TrimSpace(cfg.Addr)
	// Local syslog defaults.
	if addr == "" && (network == "unix" || network == "unixgram") {
		addr = "/dev/log"
	}
	if addr == "" {
		return nil
	}
	appName := strings.TrimSpace(cfg.App)
	if appName == "" {
		appName = "vantyx"
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	bufSize := cfg.Buffer
	if bufSize <= 0 {
		bufSize = 2000
	}
	if bufSize > 200000 {
		bufSize = 200000
	}
	f := &auditForwarder{
		network: network,
		addr:    addr,
		appName: appName,
		host:    host,
		ch:      make(chan []byte, bufSize),
		done:    make(chan struct{}),
	}
	go f.loop()
	return f
}

func newAuditForwarderFromEnv() *auditForwarder {
	addr := strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_ADDR"))
	network := strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_PROTO"))
	if network == "" {
		network = "udp"
	}
	// Local syslog defaults.
	if addr == "" && (network == "unix" || network == "unixgram") {
		addr = "/dev/log"
	}
	if addr == "" {
		return nil
	}
	appName := strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_APP"))
	if appName == "" {
		appName = "vantyx"
	}
	bufSize := 2000
	if s := strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_BUFFER")); s != "" {
		if n, err := strconvAtoiSafe(s); err == nil && n > 0 && n <= 200000 {
			bufSize = n
		}
	}
	return newAuditForwarder(auditForwarderConfig{
		Enabled: true,
		Proto:   network,
		Addr:    addr,
		App:     appName,
		Buffer:  bufSize,
	})
}

func (f *auditForwarder) close() {
	if f == nil {
		return
	}
	close(f.done)
	f.mu.Lock()
	if f.conn != nil {
		_ = f.conn.Close()
		f.conn = nil
	}
	f.mu.Unlock()
}

func (f *auditForwarder) sendJSONL(line []byte) {
	if f == nil || len(line) == 0 {
		return
	}
	// Non-blocking: drop if buffer is full.
	select {
	case f.ch <- append([]byte(nil), line...):
	default:
	}
}

func (f *auditForwarder) loop() {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-f.done:
			return
		case b := <-f.ch:
			f.writeOne(b)
		case <-tick.C:
			// keep-alive / reconnect opportunity for connection-based protocols
			if strings.HasPrefix(f.network, "tcp") || f.network == "unix" {
				_ = f.ensureConn()
			}
		}
	}
}

func (f *auditForwarder) ensureConn() error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn != nil {
		return nil
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	c, err := d.Dial(f.network, f.addr)
	if err != nil {
		return err
	}
	f.conn = c
	return nil
}

func (f *auditForwarder) writeOne(jsonLine []byte) {
	if f == nil {
		return
	}
	// RFC5424-ish syslog message with JSON payload in MSG.
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	msg := fmt.Sprintf("<134>1 %s %s %s - - - %s\n", ts, f.host, f.appName, strings.TrimSpace(string(jsonLine)))

	if strings.HasPrefix(f.network, "udp") {
		// UDP: dial per write (simple, avoids keeping state)
		c, err := net.DialTimeout("udp", f.addr, 2*time.Second)
		if err != nil {
			return
		}
		_, _ = c.Write([]byte(msg))
		_ = c.Close()
		return
	}

	if f.network == "unixgram" {
		c, err := net.DialTimeout("unixgram", f.addr, 2*time.Second)
		if err != nil {
			return
		}
		_, _ = c.Write([]byte(msg))
		_ = c.Close()
		return
	}

	// TCP/UNIX stream: keep connection and reconnect on error.
	if err := f.ensureConn(); err != nil {
		return
	}
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	if c == nil {
		return
	}
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		f.mu.Lock()
		if f.conn != nil {
			_ = f.conn.Close()
			f.conn = nil
		}
		f.mu.Unlock()
	}
}

func strconvAtoiSafe(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-digit")
		}
		n = n*10 + int(r-'0')
		if n > 1000000000 {
			return 0, fmt.Errorf("too large")
		}
	}
	return n, nil
}

