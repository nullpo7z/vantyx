package tftp

import (
	"context"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/pin/tftp/v3"
)

// Client performs TFTP get/put. No persistent connection; each operation uses UDP.
// Call Close when done (no-op for TFTP).
type Client struct {
	addr    string
	timeout time.Duration
}

// NewClient returns a TFTP client for host:port. Port 0 is treated as 69.
// The caller may call Close(); it is a no-op.
func NewClient(ctx context.Context, host string, port uint16) (*Client, error) {
	_ = ctx
	p := port
	if p == 0 {
		p = 69
	}
	addr := net.JoinHostPort(host, strconv.Itoa(int(p)))
	return &Client{addr: addr, timeout: 15 * time.Second}, nil
}

// Close is a no-op for TFTP (stateless).
func (c *Client) Close() error {
	return nil
}

// Get reads the remote file and returns a reader. Size is 0 if unknown (TFTP may not provide tsize).
func (c *Client) Get(path string) (io.ReadCloser, int64, error) {
	client, err := tftp.NewClient(c.addr)
	if err != nil {
		return nil, 0, err
	}
	client.SetTimeout(c.timeout)
	wt, err := client.Receive(path, "octet")
	if err != nil {
		return nil, 0, err
	}
	size := int64(0)
	if sz, ok := wt.(tftp.IncomingTransfer).Size(); ok {
		size = int64(sz)
	}
	pr, pw := io.Pipe()
	go func() {
		_, err := wt.WriteTo(pw)
		_ = pw.CloseWithError(err)
	}()
	return &pipeReadCloser{Reader: pr, close: func() error { return pr.Close() }}, size, nil
}

// Put writes the reader to the remote file.
func (c *Client) Put(path string, r io.Reader) error {
	client, err := tftp.NewClient(c.addr)
	if err != nil {
		return err
	}
	client.SetTimeout(c.timeout)
	rf, err := client.Send(path, "octet")
	if err != nil {
		return err
	}
	_, err = rf.ReadFrom(r)
	return err
}

type pipeReadCloser struct {
	io.Reader
	close func() error
}

func (p *pipeReadCloser) Close() error {
	return p.close()
}
