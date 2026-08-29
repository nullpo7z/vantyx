package sshd

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
	"github.com/nullpo7z/vantyx/internal/recording"
	"github.com/nullpo7z/vantyx/internal/secret"
	"github.com/nullpo7z/vantyx/internal/session"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/telnetproxy"
)

// cliCommands is the list of recognised top-level commands at the
// vantyx:/ prompt. It powers Tab completion in [runMenu].
var cliCommands = []string{"list", "ls", "cd", "pwd", "connect", "resume", "sessions", "join", "watch", "help", "exit", "quit"}

// helpLines returns the help text shown when the user types "help".
func helpLines() []string {
	return []string{
		"=== Vantyx CLI ===",
		"The top of the screen shows PWD, Groups, and Hosts (after cd).",
		"Quick start: cd <group#>  →  connect <server#>",
		"             or connect <group#> <server#> from root (top-level group only)",
		"",
		"Commands:",
		"  cd <n> | cd ..     Enter subgroup/folder or go up (cd .. / cd 0)",
		"  connect <n>        Connect (in group: server index n)",
		"  connect <g> <n>    Connect from root: g is a TOP-LEVEL group index",
		"                     only (see \"Groups\" at the vantyx:/ prompt). For a",
		"                     server nested deeper (e.g. group/sub/sub2), cd into",
		"                     each subgroup in turn, then run connect <n> there.",
		"  ls                 Reload and refresh header",
		"  list               Show active sessions below header",
		"  resume [n]         Attach to background session",
		"  join [token]       Join shared session (invitation token)",
		"  watch [n]          View-only attach to joined session",
		"  sessions           Same as list (active sessions)",
		"  In a session: Ctrl+] detach to menu; Ctrl+D end (connect only)",
		"  help | exit",
	}
}

// runMenu owns the interactive vantyx:/ prompt: target listing,
// resume, connect, navigation. Most subcommands are short, but the
// connect and resume branches embed full session lifecycles, so the
// function is intentionally long. Splitting it further would require
// passing many local variables across helpers without making it
// clearer, so the body is kept here behind exported helpers.
//
//nolint:gocyclo // menu has many commands and branches by design
func (s *Server) runMenu(ctx context.Context, channel ssh.Channel, userID string, ptyCols, ptyRows int, resizeChan <-chan sshproxy.TerminalSize) {
	defer sendExitStatus(channel, 0)
	rd := bufio.NewReader(channel)
	wr := channel
	prompt := func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(wr, format, args...)
	}

	// Single reader goroutine owns rd; all consumers read from inputCh.
	// This prevents races when StartRead and readLine would both call rd.Read.
	inputCh := make(chan byte, 4096)
	go func() {
		for {
			b, err := rd.ReadByte()
			if err != nil {
				close(inputCh)
				return
			}
			inputCh <- b
		}
	}()

	readLine := newCLILineReader(wr, inputCh)

	// allGroups caches every access group the user can reach.
	var allGroups []cliGroupEntry
	// navLoc is the current position in the group / path tree.
	var navLoc cliNavLocation
	// screenCols / screenRows track the latest known terminal size.
	// Both are updated from window-change events so target sessions
	// started later use the current size — not a stale pty-req snapshot.
	screenCols := ptyCols
	if screenCols < 40 {
		screenCols = 80
	}
	screenRows := ptyRows
	if screenRows < 1 {
		screenRows = 24
	}
	var pendingExtra []string
	setStatus := func(lines ...string) {
		pendingExtra = append(pendingExtra, lines...)
	}

	loadAllGroups := func() error {
		ctxList, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		entries, err := s.loadGroupsWithTerminalTargets(ctxList, userID)
		if err != nil {
			return err
		}
		allGroups = entries
		return nil
	}

	// activeSessionsForTargetIDs returns user's active sessions whose
	// TargetID is in the supplied set.
	activeSessionsForTargetIDs := func(targetIDSet map[string]bool) []*session.Session {
		var out []*session.Session
		if lister, ok := s.sessionManager.(SessionLister); ok {
			for _, id := range lister.ActiveIDs() {
				sess, ok := s.sessionManager.Get(id)
				if !ok || sess.UserID != userID {
					continue
				}
				if targetIDSet[sess.TargetID] {
					out = append(out, sess)
				}
			}
		}
		return out
	}

	activeSessionsForScope := func() []*session.Session {
		tidSet := targetIDsInNavScope(allGroups, navLoc)
		return activeSessionsForTargetIDs(tidSet)
	}

	var cliSessionMgr *session.Manager
	if m, ok := s.sessionManager.(*session.Manager); ok {
		cliSessionMgr = m
	}

	redrawScreen := func(extraLines []string) {
		_, _ = wr.Write([]byte(cliClearScreen))
		sessionLines := formatCLIActiveSessionLines(activeSessionsForScope(), cliSessionMgr)
		_ = writeCLIScreen(wr, cliScreenState{
			AllGroups: allGroups,
			Location:  navLoc,
			Cols:      screenCols,
		}, sessionLines, extraLines)
	}

	getPrompt := func() string {
		if navLoc.atRoot() {
			return "vantyx:/> "
		}
		return "vantyx:" + navLoc.pwd() + "> "
	}

	if err := loadAllGroups(); err != nil {
		setStatus(fmt.Sprintf("Error: %v", err))
	}

	// drainResize consumes every resize event currently waiting in
	// resizeChan, applying the latest known dimensions to screenCols /
	// screenRows. We call it at the top of the loop AND right before
	// connect / resume, because window-change events that arrive while
	// readLine is blocked on input would otherwise sit unread until the
	// next iteration — so the just-issued connect would build its frame
	// with stale dimensions.
	drainResize := func() {
		for {
			select {
			case sz, ok := <-resizeChan:
				if !ok {
					return
				}
				if sz.Cols > 0 {
					screenCols = sz.Cols
				}
				if sz.Rows > 0 {
					screenRows = sz.Rows
				}
			default:
				return
			}
		}
	}

	for {
		drainResize()

		if err := loadAllGroups(); err != nil {
			setStatus(fmt.Sprintf("Error: %v", err))
		}
		extra := pendingExtra
		pendingExtra = nil
		redrawScreen(extra)
		currentPrompt := getPrompt()
		prompt("%s", currentPrompt)
		line, err := readLine(true, currentPrompt)
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		// Trim control chars so "sessions\r" is recognised as
		// "sessions" rather than dropping into the unknown-command path.
		cmd := strings.ToLower(strings.TrimSpace(parts[0]))
		cmd = strings.TrimFunc(cmd, func(r rune) bool { return r < 32 || r == 127 })
		args := parts[1:]

		switch cmd {
		case "exit", "quit":
			prompt("Bye.\r\n")
			return
		case "help":
			pendingExtra = helpLines()
			continue
		case "pwd":
			continue
		case "cd":
			if len(args) == 0 {
				continue
			}
			if args[0] == ".." || args[0] == "0" {
				navLoc = navLoc.parent()
				continue
			}
			n, _ := strconv.Atoi(args[0])
			view := buildCLINavView(allGroups, navLoc)
			if n < 1 || n > len(view.Items) {
				if len(view.Items) == 0 {
					setStatus("No subgroups here. Use cd .. to go up.")
				} else {
					setStatus(fmt.Sprintf("Invalid index. Use 1-%d.", len(view.Items)))
				}
				continue
			}
			if next, ok := navLoc.cdIndex(allGroups, n); ok {
				navLoc = next
			}
			continue
		case "ls":
			setStatus("Refreshed.")
			continue
		case "list", "sessions":
			setStatus("Refreshed.")
			continue
		case "resume":
			drainResize()
			pendingExtra = s.handleResumeCommand(ctx, wr, inputCh, args, activeSessionsForScope(), cliSessionMgr, userID, screenCols, screenRows, resizeChan)
			continue
		case "join":
			pendingExtra = s.handleJoinCommand(wr, inputCh, args, userID, readLine, prompt)
			continue
		case "watch":
			drainResize()
			pendingExtra = s.handleWatchCommand(wr, inputCh, args, userID, screenCols, screenRows, resizeChan)
			continue
		case "connect":
			drainResize()
			pendingExtra = s.handleConnectCommand(wr, inputCh, args, allGroups, navLoc, cliSessionMgr, readLine, prompt, userID, screenCols, screenRows, resizeChan)
			continue
		default:
			setStatus(fmt.Sprintf("Unknown command '%s'. Type 'help' for commands.", cmd))
		}
	}
}

// newCLILineReader returns a readLine helper bound to wr / inputCh that
// implements local echo, backspace, Tab completion against
// [cliCommands], and CSI swallowing.
func newCLILineReader(wr io.Writer, inputCh <-chan byte) func(echo bool, promptForRedraw string) (string, error) {
	prompt := func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(wr, format, args...)
	}
	return func(echo bool, promptForRedraw string) (string, error) {
		var line []byte
		eraseChar := func() {
			_, _ = wr.Write([]byte{0x08, ' ', 0x08}) // backspace, space, backspace.
		}
		for {
			b, ok := <-inputCh
			if !ok {
				if len(line) > 0 {
					return strings.TrimSpace(string(line)), nil
				}
				return "", io.EOF
			}
			if b == '\n' || b == '\r' {
				if echo {
					_, _ = wr.Write([]byte{'\r', '\n'})
				}
				return strings.TrimSpace(string(line)), nil
			}
			// Backspace (0x7f) or BS (0x08). Never echo when the line is
			// empty (would erase the prompt).
			if b == 0x7f || b == 0x08 {
				if len(line) > 0 {
					line = line[:len(line)-1]
					if echo {
						eraseChar()
					}
				}
				continue
			}
			// Tab completion (only when promptForRedraw is set, i.e. at
			// the main vantyx:/ prompt).
			if b == '\t' && promptForRedraw != "" {
				start := 0
				for i := len(line) - 1; i >= 0; i-- {
					if line[i] == ' ' {
						start = i + 1
						break
					}
				}
				word := string(line[start:])
				word = strings.TrimSpace(word)
				word = strings.TrimFunc(word, func(r rune) bool { return r < 32 || r == 127 })
				var matches []string
				if word != "" {
					for _, c := range cliCommands {
						if strings.HasPrefix(c, strings.ToLower(word)) {
							matches = append(matches, c)
						}
					}
				}
				if len(matches) == 1 {
					completion := matches[0]
					line = append(line[:start], []byte(completion)...)
					if echo {
						for range word {
							eraseChar()
						}
						_, _ = wr.Write([]byte(completion))
					}
				} else if len(matches) > 1 {
					_, _ = wr.Write([]byte{'\r', '\n'})
					for _, m := range matches {
						prompt("  %s\r\n", m)
					}
					prompt("\r\n%s%s", promptForRedraw, line)
				} else if echo {
					_, _ = wr.Write([]byte{'\a'})
				}
				continue
			}
			if b == '\t' {
				continue
			}
			if b == 0x1b {
				// Swallow a CSI / SS3 sequence so arrow keys don't end up
				// in the input line.
				for i := 0; i < 24; i++ {
					next, ok := <-inputCh
					if !ok {
						break
					}
					if next >= 0x40 && next <= 0x7e {
						break
					}
				}
				continue
			}
			if echo {
				_, _ = wr.Write([]byte{b})
			}
			line = append(line, b)
		}
	}
}

// handleResumeCommand implements the "resume" menu command. It returns
// status lines (success / error) intended for the next redraw.
//
// screenCols and screenRows are the latest known terminal dimensions
// from runMenu (updated from window-change). prepareCLIFrame uses both
// so the resumed session honors the user's real terminal size.
func (s *Server) handleResumeCommand(ctx context.Context, wr io.Writer, inputCh <-chan byte, args []string, activeSessions []*session.Session, cliSessionMgr *session.Manager, userID string, screenCols, screenRows int, resizeChan <-chan sshproxy.TerminalSize) []string {
	var status []string
	add := func(s string) { status = append(status, s) }

	if len(activeSessions) == 0 {
		add("No active sessions.")
		return status
	}
	if len(args) == 0 {
		lines := formatCLIActiveSessionLines(activeSessions, cliSessionMgr)
		lines = append(lines, "Use resume <n> to attach (e.g. resume 1).")
		return lines
	}
	n, _ := strconv.Atoi(args[0])
	if n < 1 || n > len(activeSessions) {
		add(fmt.Sprintf("Invalid index. Use 1-%d.", len(activeSessions)))
		return status
	}
	termSess := activeSessions[n-1]
	if cliSessionMgr != nil && cliSessionMgr.IsIdle(termSess) {
		add("Warning: session has been idle for a long time (not auto-stopped).")
	}
	resumeStopCh := make(chan struct{})
	resumeDone := make(chan struct{})
	var resumeReadStarted bool
	resumeProto := access.ProtocolSSH
	if t, err := s.targetStore.Get(ctx, access.TargetID(termSess.TargetID)); err == nil {
		resumeProto = t.Protocol
	}
	targetName := termSess.TargetName
	if targetName == "" {
		targetName = "(unknown)"
	}
	frame, bridgeResize, err := s.prepareCLIFrame(wr, screenCols, screenRows, targetName, resumeProto, false, resumeStopCh, resizeChan)
	if err != nil {
		add(fmt.Sprintf("Error: %v", err))
		return status
	}
	defer func() { _ = frame.Leave() }()
	streamAttach := s.newCLIStreamAttach(resumeProto, frame.SessionWriter(), inputCh, resumeStopCh, &resumeReadStarted, resumeDone, nil, cliStreamAttachOpts{detachOnCtrlBracket: true}, nil)
	select {
	case termSess.AttachCh <- session.AttachReq{Conn: streamAttach, UserID: userID, Mode: session.AttachModeWriter}:
		select {
		case bridgeResize <- sshproxy.TerminalSize{Cols: frame.SessionCols(), Rows: frame.SessionRows()}:
		default:
		}
		select {
		case <-resumeDone:
		case <-termSess.Done():
			close(resumeStopCh)
			if resumeReadStarted {
				<-resumeDone
			}
		}
	default:
		add("Session attach slot busy. Try again.")
	}
	return status
}

// handleConnectCommand implements the "connect" menu command. It parses
// the group / target indices, prompts for optional metadata and
// credentials, starts the bridge, and returns status lines for the
// next redraw.
//
// screenCols and screenRows are the latest known terminal dimensions
// from runMenu (updated from window-change). They drive both the
// session frame (DECSTBM scroll region) and the initial asciinema
// recording size, so a resize before "connect" is honored.
//
//nolint:gocyclo // connect orchestrates session start, recording, and IO.
func (s *Server) handleConnectCommand(wr io.Writer, inputCh <-chan byte, args []string, allGroups []cliGroupEntry, navLoc cliNavLocation, cliSessionMgr *session.Manager, readLine func(bool, string) (string, error), prompt func(string, ...interface{}), userID string, screenCols, screenRows int, resizeChan <-chan sshproxy.TerminalSize) []string {
	_ = cliSessionMgr
	var status []string
	add := func(s string) { status = append(status, s) }

	if len(allGroups) == 0 {
		add("No groups assigned.")
		return status
	}

	resolveTarget := func(loc cliNavLocation, hostIndex int) (*access.Target, bool) {
		view := buildCLINavView(allGroups, loc)
		if hostIndex < 1 || hostIndex > len(view.Hosts) {
			return nil, false
		}
		return view.Hosts[hostIndex-1], true
	}

	var target *access.Target
	switch {
	case !navLoc.atRoot() && len(args) == 1:
		ti, _ := strconv.Atoi(args[0])
		var ok bool
		target, ok = resolveTarget(navLoc, ti)
		if !ok {
			view := buildCLINavView(allGroups, navLoc)
			if len(view.Hosts) == 0 {
				add("No SSH/Telnet servers at this path.")
			} else {
				add(fmt.Sprintf("Invalid server index. Use 1-%d.", len(view.Hosts)))
			}
			return status
		}
	case len(args) == 1 && strings.Contains(args[0], "."):
		dot := strings.Index(args[0], ".")
		gi, _ := strconv.Atoi(strings.TrimSpace(args[0][:dot]))
		ti, _ := strconv.Atoi(strings.TrimSpace(args[0][dot+1:]))
		rootView := buildCLINavView(allGroups, cliNavLocation{})
		if gi < 1 || gi > len(rootView.Items) {
			add(fmt.Sprintf("Invalid group index. Use 1-%d.", len(rootView.Items)))
			return status
		}
		nextLoc, ok := cliNavLocation{}.cdIndex(allGroups, gi)
		if !ok {
			add("Invalid group index.")
			return status
		}
		var tok bool
		target, tok = resolveTarget(nextLoc, ti)
		if !tok {
			childView := buildCLINavView(allGroups, nextLoc)
			if len(childView.Hosts) == 0 {
				add("No SSH/Telnet servers in that group.")
			} else {
				add(fmt.Sprintf("Invalid server index. Group has servers 1-%d.", len(childView.Hosts)))
			}
			return status
		}
	case len(args) >= 2:
		gi, _ := strconv.Atoi(args[0])
		ti, _ := strconv.Atoi(args[1])
		baseLoc := navLoc
		if baseLoc.atRoot() {
			rootView := buildCLINavView(allGroups, baseLoc)
			if gi < 1 || gi > len(rootView.Items) {
				add(fmt.Sprintf("Invalid group index. Use 1-%d.", len(rootView.Items)))
				return status
			}
			nextLoc, ok := baseLoc.cdIndex(allGroups, gi)
			if !ok {
				add("Invalid group index.")
				return status
			}
			baseLoc = nextLoc
		}
		var ok bool
		target, ok = resolveTarget(baseLoc, ti)
		if !ok {
			view := buildCLINavView(allGroups, baseLoc)
			if len(view.Hosts) == 0 {
				add("No SSH/Telnet servers at this path.")
			} else {
				add(fmt.Sprintf("Invalid server index. Use 1-%d.", len(view.Hosts)))
			}
			return status
		}
	default:
		if !navLoc.atRoot() {
			add("Usage: connect <server index> (e.g. connect 1)")
		} else {
			add("Usage: connect <group> <server> or cd <group> then connect <server>")
		}
		return status
	}
	if target == nil {
		add("No target selected.")
		return status
	}

	prompt("Session name (optional): ")
	sessionName, _ := readLine(true, "")
	sessionName = strings.TrimSpace(sessionName)
	prompt("Description (optional): ")
	sessionDesc, _ := readLine(true, "")
	sessionDesc = strings.TrimSpace(sessionDesc)

	if target.Protocol == access.ProtocolSSH && target.SSHPrivateKey != "" && secret.IsEncrypted(target.SSHPrivateKey) {
		add("Saved credentials could not be decrypted. Check VANTYX_SSH_PASSWORD_ENCRYPTION_KEY.")
		return status
	}
	var targetUser, targetPass string
	var err error
	if target.SSHUsername != "" {
		targetUser = target.SSHUsername
		targetPass = target.SSHPassword
		prompt("\r\nUsing stored credentials for %s.\r\n\r\nConnecting to %s...\r\n", target.Name, target.Name)
	} else {
		prompt("\r\nTarget username for %s: ", target.Name)
		targetUser, err = readLine(true, "")
		if err != nil {
			return status
		}
		if targetUser == "" {
			add("Username required.")
			return status
		}
		prompt("Target password: ")
		targetPass, err = readLine(false, "")
		if err != nil {
			return status
		}
		prompt("\r\nConnecting to %s...\r\n", target.Name)
	}

	connectStopCh := make(chan struct{})
	frame, bridgeResize, err := s.prepareCLIFrame(wr, screenCols, screenRows, target.Name, target.Protocol, true, connectStopCh, resizeChan)
	if err != nil {
		add(fmt.Sprintf("Error: %v", err))
		return status
	}
	defer func() { _ = frame.Leave() }()

	// 32 random bytes -> 64-char hex (cryptographically unguessable).
	// Mirrors httpapi/newTerminalSessionID so attach/view APIs cannot
	// be brute-forced (CWE-330 / CWE-340).
	sidBytes := make([]byte, 32)
	if _, randErr := cryptorand.Read(sidBytes); randErr != nil {
		add(fmt.Sprintf("Error: %v", randErr))
		return status
	}
	sessionID := session.ID(hex.EncodeToString(sidBytes))
	bridgeDone := make(chan struct{})
	readDone := make(chan struct{})
	var readStarted bool
	var bridgeErr error
	var attachOutcome cliAttachOutcome
	sessionCols := frame.SessionCols()
	sessionRows := frame.SessionRows()
	opts := session.StartOptions{
		UserID:      userID,
		TargetID:    string(target.ID),
		TargetName:  target.Name,
		Name:        sessionName,
		Description: sessionDesc,
	}
	_, err = s.sessionManager.Start(sessionID, opts, func(bridgeCtx context.Context, sess *session.Session) {
		defer close(bridgeDone)
		touch := func() { s.sessionManager.Touch(sessionID) }
		streamAttach := s.newCLIStreamAttach(target.Protocol, frame.SessionWriter(), inputCh, connectStopCh, &readStarted, readDone, nil, cliStreamAttachOpts{endOnCtrlD: true, detachOnCtrlBracket: true}, &attachOutcome)
		select {
		case bridgeResize <- sshproxy.TerminalSize{Cols: sessionCols, Rows: sessionRows}:
		default:
		}
		var tee io.Writer
		var stdinRecorder sshproxy.StdinRecorder
		var resizeRecorder sshproxy.ResizeRecorder
		if s.recordingDir != "" && s.recordingStore != nil {
			_ = os.MkdirAll(s.recordingDir, 0750)
			safeName := strings.ReplaceAll(string(sessionID), ":", "-")
			safeName = strings.ReplaceAll(safeName, ".", "-")
			castPath := filepath.Join(s.recordingDir, safeName+".cast")
			// 0o600 so an over-permissive umask cannot expose
			// recordings (M-18 / CWE-732).
			f, createErr := os.OpenFile(castPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if createErr != nil {
				slog.Warn("CLI recording create failed", "session_id", sessionID, "path", castPath, "error", createErr)
			} else {
				// Record at the bridge's actual terminal size so the
				// playback honors the user's real window. sessionCols /
				// sessionRows come from the frame, which already accounts
				// for the header lines and the latest window-change.
				asc := recording.NewAsciinemaWriter(f, sessionCols, sessionRows)
				tee = asc
				stdinRecorder = asc
				resizeRecorder = asc
				startedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
				if insertErr := s.recordingStore.InsertRecording(bridgeCtx, string(sessionID), userID, string(target.ID), string(sessionID), "cli", castPath, startedAt, sessionName, sessionDesc); insertErr != nil {
					slog.Warn("CLI recording insert failed", "session_id", sessionID, "error", insertErr)
				}
				defer func() {
					_ = f.Sync()
					_ = f.Close()
					_ = s.recordingStore.UpdateRecordingEnded(context.Background(), string(sessionID), time.Now().UTC().Format("2006-01-02 15:04:05"))
				}()
			}
		}
		if s.sharingRegistry != nil {
			s.sharingRegistry.EnsureRoom(string(sessionID), string(target.ID), userID, userID)
		}
		ownerAttach := session.AttachReq{Conn: streamAttach, UserID: userID, Mode: session.AttachModeWriter}
		switch target.Protocol {
		case access.ProtocolTelnet:
			endMsg := "session_ended: Telnet session closed"
			var telStdin telnetproxy.StdinRecorder
			if stdinRecorder != nil {
				telStdin = telnetproxy.StdinRecorderFunc(stdinRecorder.RecordInput)
			}
			var sink telnetproxy.BridgeControlSink
			if s.sharingBridges != nil {
				sink = telnetBridgeSinkCLI{id: sessionID, reg: s.sharingBridges}
			}
			bridgeErr = telnetproxy.RunBridgeDetachable(bridgeCtx, endMsg, target.Host, target.Port, targetUser, targetPass, sess.Output, sess.AttachCh, ownerAttach, touch, tee, telStdin, sessionCols, sessionRows, bridgeResize, sink, resizeRecorder)
		default:
			endMsg := "session_ended: SSH session closed"
			opts := []sshproxy.BridgeOption{sshproxy.WithHostKeyFingerprint(target.SSHHostKeyFingerprint), sshproxy.WithResizeRecorder(resizeRecorder)}
			if target.SSHHostKeyInsecureSkipVerify {
				opts = append(opts, sshproxy.WithTargetInsecureSkipVerify())
			}
			if s.sharingBridges != nil {
				opts = append(opts, sshproxy.WithBridgeControlSink(sshBridgeSink{id: sessionID, reg: s.sharingBridges}))
			}
			bridgeErr = sshproxy.RunBridgeDetachable(bridgeCtx, endMsg, target.Host, target.Port, targetUser, targetPass, target.SSHPrivateKey, target.SSHPrivateKeyPassphrase, sess.Output, sess.AttachCh, ownerAttach, touch, tee, stdinRecorder, sessionCols, sessionRows, bridgeResize, opts...)
		}
	})
	if err != nil {
		add(fmt.Sprintf("Session start failed: %v", err))
		return status
	}
	attachWait := time.Now().Add(3 * time.Second)
	for time.Now().Before(attachWait) && !readStarted {
		time.Sleep(10 * time.Millisecond)
	}
	sessionEnded := false
	clientDetached := false
	select {
	case <-readDone:
		if attachOutcome.endedSession() {
			sessionEnded = true
			if stopper, ok := s.sessionManager.(SessionStopper); ok {
				stopper.Stop(sessionID)
			}
		} else {
			clientDetached = true
		}
	case <-bridgeDone:
	}
	close(connectStopCh)
	if readStarted {
		select {
		case <-readDone:
		default:
		}
	}
	if sessionEnded {
		select {
		case <-bridgeDone:
		default:
			<-bridgeDone
		}
	} else if !clientDetached {
		select {
		case <-bridgeDone:
		default:
			<-bridgeDone
		}
	}
	switch {
	case clientDetached:
		add("Detached. Session continues in background.")
	case bridgeErr != nil:
		add(fmt.Sprintf("Disconnected: %s", proxyerrors.BridgeErrorMessage(bridgeErr)))
	default:
		add(fmt.Sprintf("Disconnected from %s.", target.Name))
	}
	return status
}
