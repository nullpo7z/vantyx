package rdpvnc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nullpo7z/vantyx/internal/vncproxy"
)

func (m *Manager) startActivityMonitor(sessionID string, b *Bridge) {
	if b == nil || b.VNCPort() <= 0 {
		return
	}
	addr := fmt.Sprintf("127.0.0.1:%d", b.VNCPort())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-b.Done()
		cancel()
	}()
	go func() {
		touch := func() { m.Touch(sessionID) }
		if err := vncproxy.Monitor(ctx, addr, touch); err != nil && ctx.Err() == nil {
			slog.Warn("rdpvnc: activity monitor ended", "session_id", sessionID, "addr", addr, "error", err)
		}
	}()
}
