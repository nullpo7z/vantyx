package sshd

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/auth"
)

var (
	testClientKeyOnce sync.Once
	testClientKey     ssh.Signer
)

// testClientSigner returns the process-wide client key the CLI tests
// authenticate with (generated once; registration is per test DB).
func testClientSigner() ssh.Signer {
	testClientKeyOnce.Do(func() {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic(err)
		}
		testClientKey, err = ssh.NewSignerFromKey(priv)
		if err != nil {
			panic(err)
		}
	})
	return testClientKey
}

// registerTestClientKey links the test client key to admin in the given
// store so ssh.PublicKeys(testClientSigner()) is accepted.
func registerTestClientKey(t *testing.T, store auth.UserStore) {
	t.Helper()
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(testClientSigner().PublicKey())))
	if _, err := store.AddPublicKey("admin", line); err != nil {
		t.Fatalf("register test client key: %v", err)
	}
}

// The CLI gateway must not accept passwords at all: a leaked password
// would otherwise bypass the web UI's second factor.
func TestServer_PasswordAuthIsNotOffered(t *testing.T) {
	_, addr, signer := setupServerWithTCP(t)

	for name, method := range map[string]ssh.AuthMethod{
		"password": ssh.Password(testAdminPassword),
		"keyboard-interactive": ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = testAdminPassword
			}
			return answers, nil
		}),
	} {
		cfg := &ssh.ClientConfig{
			User:            "admin",
			Auth:            []ssh.AuthMethod{method},
			HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
			Timeout:         5 * time.Second,
		}
		client, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			client.Close()
			t.Fatalf("%s auth with the correct password succeeded; only public keys may be accepted", name)
		}
		if !strings.Contains(err.Error(), "unable to authenticate") {
			t.Fatalf("%s: unexpected error %v", name, err)
		}
	}

	// The registered key still works, an unregistered one does not.
	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(testClientSigner())},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("public-key auth: %v", err)
	}
	client.Close()

	_, stray, _ := ed25519.GenerateKey(rand.Reader)
	straySigner, _ := ssh.NewSignerFromKey(stray)
	cfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(straySigner)}
	if client, err := ssh.Dial("tcp", addr, cfg); err == nil {
		client.Close()
		t.Fatal("unregistered key accepted")
	}
}

// Disabling or deleting an account must also end its live CLI sessions:
// CloseConnectionsForUser drops the tracked transport for that user only.
func TestServer_CloseConnectionsForUser(t *testing.T) {
	srv, addr, _ := setupServerWithTCP(t)
	cfg := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(testClientSigner())},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec G106 -- test
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		srv.connsMu.Lock()
		n := len(srv.conns)
		srv.connsMu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			if n != 1 {
				t.Fatalf("tracked conns = %d, want 1", n)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := srv.CloseConnectionsForUser("someone-else"); n != 0 {
		t.Fatalf("closed %d for another user", n)
	}
	if n := srv.CloseConnectionsForUser("admin"); n != 1 {
		t.Fatalf("closed %d, want 1", n)
	}
	done := make(chan error, 1)
	go func() { done <- client.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("client connection still open after CloseConnectionsForUser")
	}
}
