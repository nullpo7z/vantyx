package sftp

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/sshproxy"
)

// Client wraps an SFTP client and its SSH connection. Call Close when done.
type Client struct {
	*sftp.Client
	ssh *ssh.Client
}

// Close closes the SFTP and SSH clients.
func (c *Client) Close() error {
	if c.Client != nil {
		_ = c.Client.Close()
	}
	if c.ssh != nil {
		_ = c.ssh.Close()
	}
	return nil
}

// NewClient connects to host:port with username and password and/or private key (PEM + optional passphrase).
// The caller must call Close on the returned client.
// ctx is reserved for future cancellation.
func NewClient(ctx context.Context, host string, port uint16, username, password, privateKeyPEM, keyPassphrase string) (*Client, error) {
	_ = ctx
	auth, err := sshproxy.AuthMethods(password, privateKeyPEM, keyPassphrase)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User: username,
		Auth: auth,
		// #nosec G106 -- Phase 2: accept any host key; verify in Phase 3
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	config.Ciphers = sshproxy.ClientCiphers()
	addr := net.JoinHostPort(host, portString(port))
	sshClient, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, err
	}
	return &Client{Client: sc, ssh: sshClient}, nil
}

func portString(p uint16) string {
	if p == 0 {
		return "22"
	}
	return strconv.Itoa(int(p))
}
