package tftp

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/nullpo7z/vantyx/internal/access"
)

// controller manages the lifecycle of the TFTP server based on the number of
// enabled TFTP targets in the current process.
//
// The frontend TFTP toggle ultimately maps to /api/targets create /
// delete events. Controller bumps the reference count on those events
// and starts the server when refCount transitions 0→1 / stops it on
// the 1→0 transition.

type controller struct {
	mu       sync.Mutex
	refCount int
	server   *Server
	running  bool
}

var defaultController controller

// isEmbeddedCompanionTarget reports whether t is the auto-created TFTP row for an SSH/Telnet
// host TFTP toggle (same host as a row with TFTPEnabled). Standalone protocol=tftp servers
// registered in server management are not companions and must persist across restarts.
func isEmbeddedCompanionTarget(t *access.Target, sshTelnet []*access.Target) bool {
	if t == nil || t.Protocol != access.ProtocolTFTP || strings.TrimSpace(t.Host) == "" {
		return false
	}
	for _, x := range sshTelnet {
		if x == nil {
			continue
		}
		if x.Host != t.Host {
			continue
		}
		if (x.Protocol == access.ProtocolSSH || x.Protocol == access.ProtocolTelnet) && x.TFTPEnabled {
			return true
		}
	}
	return false
}

func listSSHTelnetTargets(ctx context.Context, store access.TargetStore) []*access.Target {
	var out []*access.Target
	for _, p := range []access.Protocol{access.ProtocolSSH, access.ProtocolTelnet} {
		list, err := store.ListByProtocol(ctx, p)
		if err != nil {
			continue
		}
		out = append(out, list...)
	}
	return out
}

// DisableAtStartup removes embedded TFTP companion targets created by the home TFTP toggle
// so the toggle shows OFF after restart. Manually registered protocol=tftp targets are kept.
func DisableAtStartup(ctx context.Context, store access.TargetStore) {
	tftpList, err := store.ListByProtocol(ctx, access.ProtocolTFTP)
	if err != nil || len(tftpList) == 0 {
		return
	}
	sshTelnet := listSSHTelnetTargets(ctx, store)
	var removed int
	for _, t := range tftpList {
		if !isEmbeddedCompanionTarget(t, sshTelnet) {
			continue
		}
		if err := store.Delete(ctx, t.ID); err != nil {
			slog.Warn("failed to remove TFTP companion at startup", "target_id", t.ID, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("TFTP companion targets removed at startup", "removed_targets", removed)
	}
}

// NotifyTargetCreated should be called after a TFTP target has been created
// successfully.
func NotifyTargetCreated(ctx context.Context, store access.TargetStore, proto access.Protocol) {
	if proto != access.ProtocolTFTP {
		return
	}
	defaultController.mu.Lock()
	defer defaultController.mu.Unlock()

	defaultController.refCount++
	if defaultController.running {
		return
	}

	addr := strings.TrimSpace(os.Getenv("VANTYX_TFTP_LISTEN"))
	if addr == "" {
		// The container runs as non-root and therefore cannot bind to UDP 69.
		// Default to 6969 inside the container; docker compose maps host 69 → container 6969.
		addr = "0.0.0.0:6969"
		slog.Info("TFTP server using default listen address (set VANTYX_TFTP_LISTEN to override)", "addr", addr)
	}
	root := strings.TrimSpace(os.Getenv("VANTYX_TFTP_ROOT"))
	if root == "" {
		root = "/app/data/tftp"
	}

	if err := os.MkdirAll(root, 0o750); err != nil {
		slog.Error("failed to create TFTP root directory", "root", root, "error", err)
		return
	}

	srv := NewServer(store, root)
	defaultController.server = srv
	defaultController.running = true

	go func() {
		slog.Info("starting TFTP server", "addr", addr, "root", root)
		if err := srv.ListenAndServe(addr); err != nil {
			slog.Error("TFTP server exited with error", "error", err)
		}
	}()
}

// StartServerIfNeeded starts the TFTP server on process startup when TFTP targets
// already exist (e.g. after restart). Call once after TargetStore is ready.
func StartServerIfNeeded(ctx context.Context, store access.TargetStore) {
	list, err := store.ListByProtocol(ctx, access.ProtocolTFTP)
	if err != nil || len(list) == 0 {
		return
	}
	defaultController.mu.Lock()
	defer defaultController.mu.Unlock()
	if defaultController.running {
		defaultController.refCount += len(list)
		return
	}
	addr := strings.TrimSpace(os.Getenv("VANTYX_TFTP_LISTEN"))
	if addr == "" {
		addr = "0.0.0.0:6969"
		slog.Info("TFTP server using default listen address (set VANTYX_TFTP_LISTEN to override)", "addr", addr)
	}
	root := strings.TrimSpace(os.Getenv("VANTYX_TFTP_ROOT"))
	if root == "" {
		root = "/app/data/tftp"
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		slog.Error("failed to create TFTP root directory", "root", root, "error", err)
		return
	}
	defaultController.refCount = len(list)
	srv := NewServer(store, root)
	defaultController.server = srv
	defaultController.running = true
	go func() {
		slog.Info("starting TFTP server", "addr", addr, "root", root)
		if err := srv.ListenAndServe(addr); err != nil {
			slog.Error("TFTP server exited with error", "error", err)
		}
	}()
}

// Current returns the process-wide embedded TFTP server instance, or nil if
// no TFTP target currently exists (server not started). Callers must not
// retain the pointer across the server's lifecycle; always re-fetch via
// Current() at the point of use, since NotifyTargetDeleted replaces it with
// nil when the last TFTP target is removed.
func Current() *Server {
	defaultController.mu.Lock()
	defer defaultController.mu.Unlock()
	if !defaultController.running {
		return nil
	}
	return defaultController.server
}

// NotifyTargetDeleted should be called after a TFTP target has been deleted
// successfully.
func NotifyTargetDeleted(proto access.Protocol) {
	if proto != access.ProtocolTFTP {
		return
	}
	defaultController.mu.Lock()
	defer defaultController.mu.Unlock()

	if defaultController.refCount > 0 {
		defaultController.refCount--
	}
	if defaultController.refCount > 0 {
		return
	}

	if defaultController.server != nil && defaultController.running {
		slog.Info("shutting down TFTP server because there are no enabled TFTP targets")
		defaultController.server.Shutdown()
	}
	defaultController.server = nil
	defaultController.running = false
}
