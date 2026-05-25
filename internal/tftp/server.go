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

	pintftp "github.com/pin/tftp/v3"

	"github.com/nullpo7z/vantyx/internal/access"
)

// Server is a TFTP server that serves files from a per-target directory
// rooted at rootDir. Filenames are expected to be of the form
//
//	{target_id}/{relative_path}
//
// and access is allowed only when the remote IP matches the target's Host
// and the target has ProtocolTFTP.
type Server struct {
	targetStore access.TargetStore
	rootDir     string
	srv         *pintftp.Server
}

// NewServer creates a new TFTP server instance using targetStore for ACL
// decisions and rootDir as the TFTP root directory.
func NewServer(targetStore access.TargetStore, rootDir string) *Server {
	s := &Server{
		targetStore: targetStore,
		rootDir:     rootDir,
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
	target, relPath, err := s.authorize(clientAddr.IP, filename)
	if err != nil {
		return err
	}

	fullPath, err := s.safePath(target.ID, relPath)
	if err != nil {
		return err
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("tftp: open for read failed: %w", err)
	}
	defer f.Close()

	_, err = rf.ReadFrom(f)
	return err
}

func (s *Server) handleWrite(filename string, wt io.WriterTo) error {
	it, ok := wt.(pintftp.IncomingTransfer)
	if !ok {
		return errors.New("tftp: incoming transfer does not implement IncomingTransfer")
	}
	clientAddr := it.RemoteAddr()
	target, relPath, err := s.authorize(clientAddr.IP, filename)
	if err != nil {
		return err
	}

	fullPath, err := s.safePath(target.ID, relPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return fmt.Errorf("tftp: mkdir failed: %w", err)
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("tftp: create failed: %w", err)
	}
	defer f.Close()

	_, err = wt.WriteTo(f)
	return err
}

// authorize parses filename into targetID/relPath (or just filename when a single TFTP target matches client IP),
// loads the target, checks protocol and that the client's IP matches target.Host.
func (s *Server) authorize(clientIP net.IP, filename string) (*access.Target, string, error) {
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

	return target, relPath, nil
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
