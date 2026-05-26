package tftp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pintftp "github.com/pin/tftp/v3"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/logging"
)

var tftpLogger = logging.WithComponent("tftp")

// Server is a TFTP server that serves files from a per-target directory
// rooted at rootDir. Filenames are expected to be of the form
//
//	{target_id}/{relative_path}
//
// Access is allowed only when:
//
//   - the requested target has ProtocolTFTP;
//   - the client's IP matches the target's stored host *and* one of
//     the source IPs explicitly whitelisted via TrustedClient (set by
//     the HTTP control plane after an admin authorises a job); and
//   - for write requests (WRQ), a write window is currently open for
//     that target. Reads remain permissive when
//     VANTYX_TFTP_ALLOW_READ_WITHOUT_WINDOW=1 (default off) so legacy
//     "boot ROM" devices that pull config files can still operate.
//
// UDP source addresses are trivially spoofable (CWE-290), so IP based
// authorisation is treated as a hint and not a primary access
// control. Operators must combine it with the time-bounded window
// flow exposed by the HTTP API.
type Server struct {
	targetStore access.TargetStore
	rootDir     string
	srv         *pintftp.Server

	windowMu sync.Mutex
	windows  map[access.TargetID]tftpWindow

	readWithoutWindow bool
}

type tftpWindow struct {
	expires time.Time
	allowIP net.IP
}

// NewServer creates a new TFTP server instance using targetStore for ACL
// decisions and rootDir as the TFTP root directory.
func NewServer(targetStore access.TargetStore, rootDir string) *Server {
	s := &Server{
		targetStore:       targetStore,
		rootDir:           rootDir,
		windows:           make(map[access.TargetID]tftpWindow),
		readWithoutWindow: os.Getenv("VANTYX_TFTP_ALLOW_READ_WITHOUT_WINDOW") == "1",
	}

	readHandler := func(filename string, rf io.ReaderFrom) error {
		return s.handleRead(filename, rf)
	}
	writeHandler := func(filename string, wt io.WriterTo) error {
		return s.handleWrite(filename, wt)
	}

	s.srv = pintftp.NewServer(readHandler, writeHandler)
	return s
}

// OpenWriteWindow authorises subsequent TFTP writes from clientIP to
// land in targetID's directory for at most ttl. Existing windows are
// replaced. ttl <= 0 closes the window.
func (s *Server) OpenWriteWindow(targetID access.TargetID, clientIP net.IP, ttl time.Duration) {
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	if ttl <= 0 {
		delete(s.windows, targetID)
		return
	}
	s.windows[targetID] = tftpWindow{
		expires: time.Now().Add(ttl),
		allowIP: append(net.IP(nil), clientIP...),
	}
}

// CloseWriteWindow revokes any open write window for targetID.
func (s *Server) CloseWriteWindow(targetID access.TargetID) {
	s.OpenWriteWindow(targetID, nil, 0)
}

func (s *Server) hasOpenWindow(targetID access.TargetID, clientIP net.IP) bool {
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	w, ok := s.windows[targetID]
	if !ok {
		return false
	}
	if time.Now().After(w.expires) {
		delete(s.windows, targetID)
		return false
	}
	if w.allowIP == nil || clientIP == nil {
		return false
	}
	return w.allowIP.Equal(clientIP)
}

// ListenAndServe starts the TFTP server on the given UDP address
// (e.g. ":69"). Blocks until Shutdown is called or an error occurs.
func (s *Server) ListenAndServe(addr string) error {
	if s.srv == nil {
		return errors.New("tftp server not initialized")
	}
	return s.srv.ListenAndServe(addr)
}

// Shutdown stops the TFTP server.
func (s *Server) Shutdown() {
	if s.srv != nil {
		s.srv.Shutdown()
	}
}

func (s *Server) handleRead(filename string, rf io.ReaderFrom) error {
	ot, ok := rf.(pintftp.OutgoingTransfer)
	if !ok {
		return errors.New("tftp: outgoing transfer does not implement OutgoingTransfer")
	}
	clientAddr := ot.RemoteAddr()
	target, relPath, err := s.authorize(clientAddr.IP, filename, false)
	if err != nil {
		tftpLogger.Warn("tftp read denied",
			"client", clientAddr.IP.String(),
			"filename", filename,
			"error", err)
		return err
	}

	fullPath, err := s.safePath(target.ID, relPath)
	if err != nil {
		return err
	}

	f, err := os.Open(fullPath) // #nosec G304 -- path validated by safePath()
	if err != nil {
		return fmt.Errorf("tftp: open for read failed: %w", err)
	}
	defer f.Close()

	_, err = rf.ReadFrom(f)
	tftpLogger.Info("tftp read",
		"client", clientAddr.IP.String(),
		"target", string(target.ID),
		"filename", relPath,
		"error", err)
	return err
}

func (s *Server) handleWrite(filename string, wt io.WriterTo) error {
	it, ok := wt.(pintftp.IncomingTransfer)
	if !ok {
		return errors.New("tftp: incoming transfer does not implement IncomingTransfer")
	}
	clientAddr := it.RemoteAddr()
	target, relPath, err := s.authorize(clientAddr.IP, filename, true)
	if err != nil {
		tftpLogger.Warn("tftp write denied",
			"client", clientAddr.IP.String(),
			"filename", filename,
			"error", err)
		return err
	}

	fullPath, err := s.safePath(target.ID, relPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return fmt.Errorf("tftp: mkdir failed: %w", err)
	}
	f, err := os.Create(fullPath) // #nosec G304 -- path validated by safePath()
	if err != nil {
		return fmt.Errorf("tftp: create failed: %w", err)
	}
	defer f.Close()

	_, err = wt.WriteTo(f)
	tftpLogger.Info("tftp write",
		"client", clientAddr.IP.String(),
		"target", string(target.ID),
		"filename", relPath,
		"error", err)
	return err
}

// authorize parses filename into targetID/relPath (or just filename when a single TFTP target matches client IP),
// loads the target, checks protocol and applies the per-target write
// window. UDP source addresses are spoofable so write requests
// (isWrite=true) require an admin-opened time window for clientIP;
// reads optionally still require a window (see Server doc).
func (s *Server) authorize(clientIP net.IP, filename string, isWrite bool) (*access.Target, string, error) {
	if s.targetStore == nil {
		return nil, "", errors.New("tftp: target store not configured")
	}
	trimmed := strings.TrimLeft(filename, "/")
	parts := strings.SplitN(trimmed, "/", 2)

	ctx := context.Background()

	// When the requested filename does not start with "target_id/...":
	// fall back to a single TFTP target whose stored host matches the
	// client's IP. This lets simple TFTP clients (network gear) Get / Put
	// without knowing the internal target identifier.
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		list, err := s.targetStore.ListByProtocol(ctx, access.ProtocolTFTP)
		if err != nil {
			return nil, "", fmt.Errorf("tftp: list targets: %w", err)
		}
		var match *access.Target
		for _, t := range list {
			tip := net.ParseIP(t.Host)
			if tip == nil || clientIP == nil || !clientIP.Equal(tip) {
				continue
			}
			if match != nil {
				return nil, "", errors.New("tftp: multiple TFTP targets match client IP; use target_id/filename")
			}
			match = t
		}
		if match == nil {
			return nil, "", errors.New("tftp: no TFTP target for this client IP; use target_id/filename or add a TFTP target with Host = client IP")
		}
		if trimmed == "" {
			return nil, "", errors.New("tftp: filename must be target_id/relative_path or a non-empty filename")
		}
		if err := s.enforceWindow(match.ID, clientIP, isWrite); err != nil {
			return nil, "", err
		}
		return match, trimmed, nil
	}

	targetID := access.TargetID(parts[0])
	relPath := parts[1]

	target, err := s.targetStore.Get(ctx, targetID)
	if err != nil {
		return nil, "", fmt.Errorf("tftp: target not found: %w", err)
	}
	if target.Protocol != access.ProtocolTFTP {
		return nil, "", errors.New("tftp: target protocol is not tftp")
	}

	targetIP := net.ParseIP(target.Host)
	if targetIP == nil {
		return nil, "", errors.New("tftp: target host is not an IP address")
	}
	if clientIP == nil || !clientIP.Equal(targetIP) {
		return nil, "", errors.New("tftp: access denied for this IP")
	}
	if err := s.enforceWindow(target.ID, clientIP, isWrite); err != nil {
		return nil, "", err
	}

	return target, relPath, nil
}

// enforceWindow rejects a request when no admin-opened TFTP window
// authorises it. Writes always require a window; reads require one
// unless VANTYX_TFTP_ALLOW_READ_WITHOUT_WINDOW=1 (default off).
//
// UDP source addresses are spoofable, so the window flow is what
// actually protects the rootDir; the IP match is a sanity check.
func (s *Server) enforceWindow(targetID access.TargetID, clientIP net.IP, isWrite bool) error {
	if !isWrite && s.readWithoutWindow {
		return nil
	}
	if s.hasOpenWindow(targetID, clientIP) {
		return nil
	}
	if isWrite {
		return fmt.Errorf("tftp: write denied; no active write window for target %s (open one via the HTTP API)", targetID)
	}
	return fmt.Errorf("tftp: read denied; open a window via the HTTP API or set VANTYX_TFTP_ALLOW_READ_WITHOUT_WINDOW=1 for legacy clients")
}

// safePath builds a filesystem path under the per-target directory while
// preventing directory traversal.
func (s *Server) safePath(targetID access.TargetID, relPath string) (string, error) {
	base := filepath.Clean(filepath.Join(s.rootDir, string(targetID)))
	full := filepath.Clean(filepath.Join(base, relPath))
	if !strings.HasPrefix(full, base+string(os.PathSeparator)) && full != base {
		return "", errors.New("tftp: invalid path")
	}
	return full, nil
}
