package sshd

import (
	"context"
	"io"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// cliGroupEntry holds a group together with the terminal-capable
// (SSH / Telnet) targets the user can reach from CLI.
type cliGroupEntry struct {
	Group   *access.AccessGroup
	Targets []*access.Target
}

// isCLITerminalProtocol reports whether p is exposed in the CLI menu.
func isCLITerminalProtocol(p access.Protocol) bool {
	return p == access.ProtocolSSH || p == access.ProtocolTelnet
}

// prepareCLIFrame draws the session status bar and starts resize
// forwarding for a target session. The caller is responsible for
// calling Leave on the returned frame when the bridge ends.
func (s *Server) prepareCLIFrame(wr io.Writer, screenCols, ptyRows int, targetName string, protocol access.Protocol, showEndSessionHint bool, stopCh <-chan struct{}, resizeChan <-chan sshproxy.TerminalSize) (*cliSessionFrame, chan sshproxy.TerminalSize, error) {
	frame := newCLISessionFrame(wr, screenCols, ptyRows)
	bar := cliSessionBarState{
		TargetName:         targetName,
		Protocol:           protocol,
		Cols:               screenCols,
		ShowEndSessionHint: showEndSessionHint,
	}
	if err := frame.Enter(bar); err != nil {
		return nil, nil, err
	}
	bridgeResize := make(chan sshproxy.TerminalSize, 8)
	frame.SetOnResize(func(c, r int) {
		select {
		case bridgeResize <- sshproxy.TerminalSize{Cols: c, Rows: r}:
		default:
		}
	})
	go watchCLIResize(stopCh, resizeChan, frame)
	return frame, bridgeResize, nil
}

// loadGroupsWithTerminalTargets returns the access groups the user can
// see, each annotated with the terminal-capable targets reachable from
// the CLI. Errors fetching individual groups or targets are skipped
// silently so a single bad row never hides the entire menu.
func (s *Server) loadGroupsWithTerminalTargets(ctx context.Context, userID string) ([]cliGroupEntry, error) {
	groupIDs, err := s.groupStore.GroupIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		return nil, err
	}
	allowedTargetIDs, err := s.groupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		return nil, err
	}
	allowedSet := make(map[access.TargetID]bool)
	for _, id := range allowedTargetIDs {
		allowedSet[id] = true
	}
	var out []cliGroupEntry
	for _, gid := range groupIDs {
		g, err := s.groupStore.Get(ctx, gid)
		if err != nil {
			continue
		}
		tids, err := s.groupStore.TargetIDsForGroup(ctx, gid, nil)
		if err != nil {
			continue
		}
		var filtered []access.TargetID
		for _, tid := range tids {
			if allowedSet[tid] {
				filtered = append(filtered, tid)
			}
		}
		targets, err := s.targetStore.ListByIDs(ctx, filtered, nil)
		if err != nil {
			continue
		}
		var terminalOnly []*access.Target
		for _, t := range targets {
			if isCLITerminalProtocol(t.Protocol) {
				terminalOnly = append(terminalOnly, t)
			}
		}
		out = append(out, cliGroupEntry{Group: g, Targets: terminalOnly})
	}
	return out, nil
}
