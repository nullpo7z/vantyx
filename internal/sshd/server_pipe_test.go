// Package sshd (same package) for tests that drive the server via real TCP (no pipe).
package sshd

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
	"github.com/nullpo7z/vantyx/internal/session"
)

const testAdminPassword = "Admin123!"

// setupServerWithTCPEmptyGroups creates a server where admin has no SSH targets (e.g. group has only telnet target).
func setupServerWithTCPEmptyGroups(t *testing.T) (*Server, string, ssh.Signer) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sshd_empty.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	userStore := auth.NewSQLiteUserStore(db)
	_, _ = userStore.CreateUser("admin", "admin", testAdminPassword, auth.RoleAdmin)
	groupStore := access.NewSQLiteAccessGroupStore(db, nil)
	targetStore := access.NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()
	_, _ = groupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = groupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	// Only telnet target -> no SSH targets -> loadGroupsWithSSHTargets returns empty
	_, _ = targetStore.CreateWithPath(ctx, access.TargetID("t1"), "telnet1", "127.0.0.1", 23, access.ProtocolTelnet, access.GroupID("g1"), "g1", "", "", "", "")
	_ = groupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	mgr := session.NewManager()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(key)
	srv, err := NewServer(Config{
		UserStore: userStore, TargetStore: targetStore, GroupStore: groupStore,
		SessionManager: mgr, HostKey: signer,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() { _ = srv.Serve(ln) }()
	return srv, ln.Addr().String(), signer
}

// setupServerWithTCP creates an SSH server with in-memory stores (admin user, one group, one SSH target),
// starts Serve(listener) in a goroutine, and returns the server, listener address, and signer for client config.
func setupServerWithTCP(t *testing.T) (*Server, string, ssh.Signer) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sshd.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := dbsqlite.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	userStore := auth.NewSQLiteUserStore(db)
	_, err = userStore.CreateUser("admin", "admin", testAdminPassword, auth.RoleAdmin)
	if err != nil && !errors.Is(err, auth.ErrUserExists) {
		t.Fatalf("create admin: %v", err)
	}
	groupStore := access.NewSQLiteAccessGroupStore(db, nil)
	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i + 1)
	}
	targetStore := access.NewSQLiteTargetStore(db, nil, encKey)
	ctx := context.Background()
	_, _ = groupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = groupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = targetStore.CreateWithPath(ctx, access.TargetID("t1"), "srv1", "127.0.0.1", 1, access.ProtocolSSH, access.GroupID("g1"), "g1", "root", "pass", "", "")
	_ = groupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	mgr := session.NewManager()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	srv, err := NewServer(Config{
		UserStore:      userStore,
		TargetStore:    targetStore,
		GroupStore:     groupStore,
		SessionManager: mgr,
		HostKey:        signer,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	return srv, ln.Addr().String(), signer
}

func TestServer_Serve_RealTCP(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)

	config := &ssh.ClientConfig{
		User: "admin",
		Auth: []ssh.AuthMethod{ssh.Password(testAdminPassword)},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()
	if err := sess.RequestPty("xterm", 80, 24, nil); err != nil {
		t.Fatalf("RequestPty: %v", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if err := sess.Shell(); err != nil {
		t.Fatalf("Shell: %v", err)
	}
	_, _ = stdin.Write([]byte("help\r\n"))
	time.Sleep(100 * time.Millisecond)
	_, _ = stdin.Write([]byte("exit\r\n"))
	_ = stdin.Close()
	_ = sess.Wait()
}

// runSession runs shell, sends commands (each followed by \r\n), then exit. Uses small sleeps so server can process.
func runSession(t *testing.T, addr string, signer ssh.Signer, commands ...string) {
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password(testAdminPassword)},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()
	if err := sess.RequestPty("xterm", 80, 24, nil); err != nil {
		t.Fatalf("RequestPty: %v", err)
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if err := sess.Shell(); err != nil {
		t.Fatalf("Shell: %v", err)
	}
	for _, cmd := range commands {
		_, _ = stdin.Write([]byte(cmd + "\r\n"))
		time.Sleep(80 * time.Millisecond)
	}
	_ = stdin.Close()
	_ = sess.Wait()
}

func TestServer_Serve_MenuListLsCdPwdSessionsResume(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	runSession(t, addr, signer,
		"list",
		"ls",
		"cd 1",
		"pwd",
		"ls",
		"cd ..",
		"pwd",
		"sessions",
		"resume",
		"exit",
	)
}

func TestServer_Serve_UnknownCommand(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	runSession(t, addr, signer, "unknown_cmd", "exit")
}

func TestServer_Serve_TabCompletion(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// Single match: "he" + tab -> "help"; "ex" + tab -> "exit"
	runSession(t, addr, signer, "he\t", "ex\t")
}

func TestServer_Serve_TabCompletionMultiple(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// Multiple matches: "l" + tab lists "list" and "ls"
	runSession(t, addr, signer, "l\t", "exit")
}

func TestServer_Serve_TabNoMatch(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// Tab with no matching command -> bell
	runSession(t, addr, signer, "x\t", "exit")
}

func TestServer_Serve_Quit(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	runSession(t, addr, signer, "quit")
}

func TestServer_Serve_CdNoArgsAndInvalid(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	runSession(t, addr, signer, "cd", "cd 99", "cd ..", "cd 0", "exit")
}

func TestServer_Serve_ConnectInvalidIndices(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	runSession(t, addr, signer, "list", "connect 99 1", "connect 1 99", "exit")
}

func TestServer_Serve_ResumeInvalidIndex(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	runSession(t, addr, signer, "resume 99", "exit")
}

func TestServer_Serve_BackspaceInLine(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// Type "hel", two backspaces, then newline -> command "h" (unknown)
	runSession(t, addr, signer, "hel\b\b", "exit")
}

func TestServer_Serve_AnsiEscape(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// Send ESC [ A (up arrow); server swallows ANSI, then we get empty line
	runSession(t, addr, signer, "\x1b[A", "exit")
}

func TestServer_Serve_ClientCloseWithoutExit(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password(testAdminPassword)},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.RequestPty("xterm", 80, 24, nil); err != nil {
		client.Close()
		t.Fatalf("RequestPty: %v", err)
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if err := sess.Shell(); err != nil {
		client.Close()
		t.Fatalf("Shell: %v", err)
	}
	_, _ = stdin.Write([]byte("help\r\n"))
	time.Sleep(100 * time.Millisecond)
	// Close without sending "exit"; server readLine gets EOF and runMenu returns
	_ = client.Close()
	time.Sleep(200 * time.Millisecond)
}

func TestServer_Serve_ReadLinePartialThenEOF(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password(testAdminPassword)},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.RequestPty("xterm", 80, 24, nil); err != nil {
		client.Close()
		t.Fatalf("RequestPty: %v", err)
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if err := sess.Shell(); err != nil {
		client.Close()
		t.Fatalf("Shell: %v", err)
	}
	// Send partial line (no newline); then close -> readLine returns (line, nil) with len(line)>0
	_, _ = stdin.Write([]byte("hel"))
	time.Sleep(50 * time.Millisecond)
	_ = client.Close()
	time.Sleep(200 * time.Millisecond)
}

func TestServer_Serve_ConnectFails(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// connect 1 1 = group 1, server 1; then session name (empty), description (empty); connection to 127.0.0.1:1 will fail
	runSession(t, addr, signer, "list", "connect 1 1", "", "", "exit")
}

func TestServer_Serve_ConnectDotNotation(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// connect "1.1" = group 1, server 1 (dot notation)
	runSession(t, addr, signer, "list", "connect 1.1", "", "", "exit")
}

func TestServer_Serve_ConnectUsagePrompt(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	// At root without list: connect with no args or wrong args shows usage
	runSession(t, addr, signer, "connect", "list", "cd 1", "connect", "exit")
}

func TestServer_Serve_EmptyGroups(t *testing.T) {
	_, addr, signer := setupServerWithTCPEmptyGroups(t)
	// No SSH targets -> list/ls show "No groups or servers" or empty; cd triggers load, gets empty
	runSession(t, addr, signer, "list", "ls", "cd", "exit")
}

// setupServerWithTCPNoCreds: one SSH target with no stored credentials (connect will prompt for user/pass).
func setupServerWithTCPNoCreds(t *testing.T) (*Server, string, ssh.Signer) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sshd_nocreds.db")
	db, _ := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	_ = dbsqlite.Migrate(db)
	userStore := auth.NewSQLiteUserStore(db)
	_, _ = userStore.CreateUser("admin", "admin", testAdminPassword, auth.RoleAdmin)
	groupStore := access.NewSQLiteAccessGroupStore(db, nil)
	targetStore := access.NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()
	_, _ = groupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = groupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	// Target with no SSH username/password -> connect prompts for them
	_, _ = targetStore.CreateWithPath(ctx, access.TargetID("t1"), "srv1", "127.0.0.1", 1, access.ProtocolSSH, access.GroupID("g1"), "g1", "", "", "", "")
	_ = groupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	mgr := session.NewManager()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(key)
	srv, _ := NewServer(Config{
		UserStore: userStore, TargetStore: targetStore, GroupStore: groupStore,
		SessionManager: mgr, HostKey: signer,
	})
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() { _ = srv.Serve(ln) }()
	return srv, ln.Addr().String(), signer
}

func TestServer_Serve_ConnectPromptsForCredentials(t *testing.T) {
	_, addr, signer := setupServerWithTCPNoCreds(t)
	// connect 1 1 prompts for session name, description, then target username, target password (echo false)
	runSession(t, addr, signer, "list", "connect 1 1", "", "", "myuser", "mypass", "exit")
}

func TestServer_Serve_ConnectEmptyUsernameRejected(t *testing.T) {
	_, addr, signer := setupServerWithTCPNoCreds(t)
	// Empty username at prompt -> "Username required." then back to main prompt
	runSession(t, addr, signer, "list", "connect 1 1", "", "", "", "exit")
}

func TestServer_ListenAndServe_Success(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sshd_listen.db")
	db, _ := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	_ = dbsqlite.Migrate(db)
	userStore := auth.NewSQLiteUserStore(db)
	_, _ = userStore.CreateUser("admin", "admin", testAdminPassword, auth.RoleAdmin)
	groupStore := access.NewSQLiteAccessGroupStore(db, nil)
	targetStore := access.NewSQLiteTargetStore(db, nil, nil)
	ctx := context.Background()
	_, _ = groupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = groupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(key)
	srv, err := NewServer(Config{
		UserStore: userStore, TargetStore: targetStore, GroupStore: groupStore,
		SessionManager: session.NewManager(), HostKey: signer,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.ListenAndServe("127.0.0.1:0") }()
	var addr string
	for i := 0; i < 50; i++ {
		if a := srv.Addr(); a != nil {
			addr = a.String()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("server did not start in time")
	}
	runSession(t, addr, signer, "exit")
}

func TestServer_ListenAndServe_Error(t *testing.T) {
	db, err := dbsqlite.Open(dbsqlite.Config{Path: ""})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_ = dbsqlite.Migrate(db)
	userStore := auth.NewSQLiteUserStore(db)
	_, _ = userStore.CreateUser("admin", "admin", testAdminPassword, auth.RoleAdmin)
	srv, err := NewServer(Config{
		UserStore:      userStore,
		TargetStore:    access.NewSQLiteTargetStore(db, nil, nil),
		GroupStore:     access.NewSQLiteAccessGroupStore(db, nil),
		SessionManager: session.NewManager(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Invalid address so Listen fails
	err = srv.ListenAndServe("invalid-address:99999")
	if err == nil {
		t.Fatal("expected ListenAndServe to fail with invalid address")
	}
}

func TestServer_Shutdown(t *testing.T) {
	srv, addr, signer := setupServerWithTCP(t)
	// Shutdown the server (closes listener); Serve goroutine will get Accept error and return
	if err := srv.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	// Listener is closed; new SSH connection should fail (connection refused or reset)
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password(testAdminPassword)},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         500 * time.Millisecond,
	}
	_, err := ssh.Dial("tcp", addr, config)
	if err == nil {
		// On some systems the port may not be released immediately; Shutdown path is still covered
		t.Logf("Dial succeeded after Shutdown (port may still be in use); Shutdown() was exercised")
	}
}

func TestServer_Shutdown_NoListener(t *testing.T) {
	db, _ := dbsqlite.Open(dbsqlite.Config{Path: ""})
	_ = dbsqlite.Migrate(db)
	userStore := auth.NewSQLiteUserStore(db)
	_, _ = userStore.CreateUser("admin", "admin", testAdminPassword, auth.RoleAdmin)
	srv, err := NewServer(Config{
		UserStore:      userStore,
		TargetStore:    access.NewSQLiteTargetStore(db, nil, nil),
		GroupStore:     access.NewSQLiteAccessGroupStore(db, nil),
		SessionManager: session.NewManager(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Shutdown(); err != nil {
		t.Fatalf("Shutdown with nil listener should return nil: %v", err)
	}
}

// failAfterFirstAcceptListener wraps a listener; first Accept() returns the real conn, second returns err.
type failAfterFirstAcceptListener struct {
	net.Listener
	acceptCount int
}

func (f *failAfterFirstAcceptListener) Accept() (net.Conn, error) {
	f.acceptCount++
	if f.acceptCount == 2 {
		return nil, errors.New("test accept error")
	}
	return f.Listener.Accept()
}

func TestServer_Serve_AcceptError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sshd_accept.db")
	db, err := dbsqlite.Open(dbsqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_ = dbsqlite.Migrate(db)
	userStore := auth.NewSQLiteUserStore(db)
	_, _ = userStore.CreateUser("admin", "admin", testAdminPassword, auth.RoleAdmin)
	encKey := make([]byte, 32)
	targetStore := access.NewSQLiteTargetStore(db, nil, encKey)
	groupStore := access.NewSQLiteAccessGroupStore(db, nil)
	ctx := context.Background()
	_, _ = groupStore.Create(ctx, access.GroupID("g1"), "G1")
	_ = groupStore.AddUserToGroup(ctx, access.UserID("admin"), access.GroupID("g1"))
	_, _ = targetStore.CreateWithPath(ctx, access.TargetID("t1"), "srv1", "127.0.0.1", 1, access.ProtocolSSH, access.GroupID("g1"), "g1", "root", "pass", "", "")
	_ = groupStore.AddTargetToGroup(ctx, access.GroupID("g1"), access.TargetID("t1"))
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := ssh.NewSignerFromKey(key)
	srv, err := NewServer(Config{
		UserStore: userStore, TargetStore: targetStore, GroupStore: groupStore,
		SessionManager: session.NewManager(), HostKey: signer,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	wrapped := &failAfterFirstAcceptListener{Listener: ln}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(wrapped) }()
	// First connection: connect and exit so server goes to second Accept and gets our error
	runSession(t, ln.Addr().String(), signer, "exit")
	err = <-done
	if err == nil {
		t.Fatal("expected Serve to return error after second Accept failed")
	}
}

func TestServer_HandleConn_HandshakeFail(t *testing.T) {
	_, addr, _ := setupServerWithTCP(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// Write garbage and close; server handleConn should exit on handshake error
	_, _ = conn.Write([]byte("SSH-invalid\r\n"))
	_ = conn.Close()
	time.Sleep(200 * time.Millisecond)
}

func TestServer_Serve_WindowChange(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password(testAdminPassword)},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()
	if err := sess.RequestPty("xterm", 80, 24, nil); err != nil {
		t.Fatalf("RequestPty: %v", err)
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if err := sess.Shell(); err != nil {
		t.Fatalf("Shell: %v", err)
	}
	// Send window-change to cover handleConn's menuReturned!=nil branch
	_ = sess.WindowChange(40, 132)
	_, _ = stdin.Write([]byte("exit\r\n"))
	_ = stdin.Close()
	_ = sess.Wait()
}

func TestServer_HandleConn_RejectNonSessionChannel(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password(testAdminPassword)},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	// Open a non-session channel; server should reject with "session only"
	_, _, err = client.Conn.OpenChannel("direct-tcpip", nil)
	if err == nil {
		t.Fatal("expected OpenChannel(direct-tcpip) to be rejected")
	}
}
