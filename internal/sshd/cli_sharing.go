package sshd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sharing"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/telnetproxy"
)

type sharingBridgeRegistry interface {
	Register(id session.ID, ctrl sharing.BridgeControl)
	Get(id session.ID) (sharing.BridgeControl, bool)
}

type sshBridgeSink struct {
	id  session.ID
	reg sharingBridgeRegistry
}

func (s sshBridgeSink) Register(c sshproxy.BridgeController) {
	if s.reg != nil {
		s.reg.Register(s.id, c)
	}
}

type telnetBridgeSinkCLI struct {
	id  session.ID
	reg sharingBridgeRegistry
}

func (s telnetBridgeSinkCLI) Register(c telnetproxy.BridgeController) {
	if s.reg != nil {
		s.reg.Register(s.id, c)
	}
}

func (s *Server) sharingService() *sharing.Service {
	return &sharing.Service{
		Store:    s.sharingStore,
		Registry: s.sharingRegistry,
		Access:   s.userCanAccessTarget,
		Bridges: func(sessionID string) (sharing.BridgeControl, bool) {
			if s.sharingBridges == nil {
				return nil, false
			}
			return s.sharingBridges.Get(session.ID(sessionID))
		},
	}
}

// userCanAccessTarget reports whether userID may reach targetID via their
// group/tag ACL grants. It mirrors the HTTP layer's check of the same
// name so the CLI collaborative-join path enforces the identical target
// access control (a valid invitation token is never sufficient on its
// own). Fails closed when the group store is unavailable.
func (s *Server) userCanAccessTarget(ctx context.Context, userID, targetID string) (bool, error) {
	if s.groupStore == nil {
		return false, nil
	}
	ids, err := s.groupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if string(id) == targetID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) listJoinableSessions(userID string) []*session.Session {
	if s.sharingRegistry == nil || s.sessionManager == nil {
		return nil
	}
	var out []*session.Session
	for _, sid := range s.sharingRegistry.RoomsForUser(userID) {
		termSess, ok := s.sessionManager.Get(session.ID(sid))
		if !ok {
			continue
		}
		if termSess.UserID == userID {
			continue
		}
		room, ok := s.sharingRegistry.Get(sid)
		if !ok || !room.IsParticipant(userID) {
			continue
		}
		out = append(out, termSess)
	}
	return out
}

func (s *Server) handleJoinCommand(wr io.Writer, inputCh <-chan byte, args []string, userID string, readLine func(bool, string) (string, error), prompt func(string, ...interface{})) []string {
	var status []string
	add := func(msg string) { status = append(status, msg) }
	if s.sharingStore == nil || s.sharingRegistry == nil {
		add("Collaborative sessions are not available.")
		return status
	}
	joinable := s.listJoinableSessions(userID)
	if len(args) == 0 && len(joinable) == 0 {
		prompt("Invitation token: ")
		tok, err := readLine(true, "Invitation token: ")
		if err != nil {
			add(fmt.Sprintf("Error: %v", err))
			return status
		}
		tok = strings.TrimSpace(tok)
		if tok == "" {
			add("Token required.")
			return status
		}
		return s.joinWithToken(wr, inputCh, userID, tok, readLine, prompt, add)
	}
	if len(args) == 0 {
		for i, ts := range joinable {
			name := ts.TargetName
			if name == "" {
				name = ts.TargetID
			}
			add(fmt.Sprintf("  %d) %s (session %s)", i+1, name, ts.ID()))
		}
		add("Use watch <n> to attach view-only.")
		return status
	}
	add("Use watch <n> to attach to a session you already joined.")
	return status
}

func (s *Server) joinWithToken(wr io.Writer, inputCh <-chan byte, userID, token string, readLine func(bool, string) (string, error), prompt func(string, ...interface{}), add func(string)) []string {
	inv, err := s.sharingStore.GetByTokenHash(context.Background(), sharing.HashToken(token))
	if err != nil {
		add("Invitation not found or expired.")
		return nil
	}
	_, ok := s.sessionManager.Get(session.ID(inv.SessionID))
	if !ok {
		add("Session not found.")
		return nil
	}
	username := userID
	if u, err := s.userStore.GetByID(userID); err == nil && u != nil && u.Username != "" {
		username = u.Username
	}
	svc := s.sharingService()
	if _, err := svc.JoinRoom(context.Background(), inv, userID, username, time.Now().UTC()); err != nil {
		add(fmt.Sprintf("Join failed: %v", err))
		return nil
	}
	add(fmt.Sprintf("Joined session %s. Use watch to attach.", inv.SessionID))
	return nil
}

func (s *Server) handleWatchCommand(wr io.Writer, inputCh <-chan byte, args []string, userID string, screenCols, screenRows int, resizeChan <-chan sshproxy.TerminalSize) []string {
	var status []string
	add := func(msg string) { status = append(status, msg) }
	joinable := s.listJoinableSessions(userID)
	if len(joinable) == 0 {
		add("No shared sessions to watch. Use join with an invitation token first.")
		return status
	}
	if len(args) == 0 {
		for i, ts := range joinable {
			name := ts.TargetName
			if name == "" {
				name = ts.TargetID
			}
			add(fmt.Sprintf("  %d) %s", i+1, name))
		}
		add("Use watch <n> to attach view-only.")
		return status
	}
	n, _ := strconv.Atoi(args[0])
	if n < 1 || n > len(joinable) {
		add(fmt.Sprintf("Invalid index. Use 1-%d.", len(joinable)))
		return status
	}
	termSess := joinable[n-1]
	// Defense-in-depth: re-check target ACL at attach time, mirroring the
	// HTTP viewer-attach path. Room participation is stored in-memory, so
	// a user who has lost target access since joining must not be able to
	// (re-)attach to the live session.
	if ok, err := s.userCanAccessTarget(context.Background(), userID, termSess.TargetID); err != nil {
		add(fmt.Sprintf("Error: %v", err))
		return status
	} else if !ok {
		add("You no longer have access to this target.")
		return status
	}
	proto := access.ProtocolSSH
	if t, err := s.targetStore.Get(context.Background(), access.TargetID(termSess.TargetID)); err == nil {
		proto = t.Protocol
	}
	targetName := termSess.TargetName
	if targetName == "" {
		targetName = termSess.TargetID
	}
	watchStopCh := make(chan struct{})
	watchDone := make(chan struct{})
	var readStarted bool
	frame, bridgeResize, err := s.prepareCLIFrame(wr, screenCols, screenRows, targetName+" (view-only)", proto, false, watchStopCh, resizeChan)
	if err != nil {
		add(fmt.Sprintf("Error: %v", err))
		return status
	}
	defer func() { _ = frame.Leave() }()
	add("View-only mode: input is disabled. Press Ctrl+] to return to the menu.")
	streamAttach := s.newCLIStreamAttach(proto, frame.SessionWriter(), inputCh, watchStopCh, &readStarted, watchDone, nil, cliStreamAttachOpts{detachOnCtrlBracket: true}, nil)
	select {
	case termSess.AttachCh <- session.AttachReq{Conn: streamAttach, UserID: userID, Mode: session.AttachModeViewer}:
		select {
		case bridgeResize <- sshproxy.TerminalSize{Cols: frame.SessionCols(), Rows: frame.SessionRows()}:
		default:
		}
		select {
		case <-watchDone:
		case <-termSess.Done():
			close(watchStopCh)
			if readStarted {
				<-watchDone
			}
		}
	default:
		add("Session attach slot busy. Try again.")
	}
	return status
}
