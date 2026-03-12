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
// フロントエンドからの TFTP トグルは最終的に /api/targets の
// Create/Delete に集約される想定なので、そのイベントに応じて
// refCount を増減させ、0→1 で起動・1→0 で Shutdown する。

type controller struct {
	mu       sync.Mutex
	refCount int
	server   *Server
	running  bool
}

var defaultController controller

// DisableAtStartup removes all TFTP targets on process startup so that the home
// screen shows the TFTP toggle OFF and the TFTP server does not run until the user enables it.
func DisableAtStartup(ctx context.Context, store access.TargetStore) {
	list, err := store.ListByProtocol(ctx, access.ProtocolTFTP)
	if err != nil || len(list) == 0 {
		return
	}
	for _, t := range list {
		_ = store.Delete(ctx, t.ID)
	}
	slog.Info("TFTP disabled at startup", "removed_targets", len(list))
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
		// コンテナで nonroot のため 69 は使えない。6969 をデフォルトにし、docker で 69:6969/udp でマップする。
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

