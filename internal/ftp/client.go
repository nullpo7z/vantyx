package ftp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	jftp "github.com/jlaffaye/ftp"
)

// ftpSizeToInt64 converts FTP entry size (uint64) to int64 for os.FileInfo, capping at math.MaxInt64.
func ftpSizeToInt64(u uint64) int64 {
	if u > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(u)
}

// Client is a minimal FTP client wrapper that provides directory listing
// and file transfer operations used by the HTTP file handlers.
type Client struct {
	conn *jftp.ServerConn
}

// NewClient connects to an FTP server using host/port and username/password.
func NewClient(ctx context.Context, host string, port uint16, username, password string) (*Client, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	dialer := &net.Dialer{}
	dialFn := func(network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, address)
	}
	conn, err := jftp.Dial(addr,
		jftp.DialWithDialFunc(dialFn),
		jftp.DialWithTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("ftp dial failed: %w", err)
	}
	if err := conn.Login(username, password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("ftp login failed: %w", err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the underlying FTP connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Quit()
}

// ftpFileInfo adapts jlaffaye/ftp Entry to os.FileInfo.
type ftpFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi ftpFileInfo) Name() string       { return fi.name }
func (fi ftpFileInfo) Size() int64        { return fi.size }
func (fi ftpFileInfo) Mode() os.FileMode  { return fi.mode }
func (fi ftpFileInfo) ModTime() time.Time { return fi.modTime }
func (fi ftpFileInfo) IsDir() bool        { return fi.isDir }
func (fi ftpFileInfo) Sys() any           { return nil }

// ReadDir lists entries in the given remote directory.
func (c *Client) ReadDir(p string) ([]os.FileInfo, error) {
	if p == "" {
		p = "/"
	}
	entries, err := c.conn.List(p)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		isDir := e.Type == jftp.EntryTypeFolder
		mode := os.FileMode(0o644)
		if isDir {
			mode = os.FileMode(0o755) | os.ModeDir
		}
		out = append(out, ftpFileInfo{
			name:    e.Name,
			size:    ftpSizeToInt64(e.Size),
			mode:    mode,
			modTime: e.Time,
			isDir:   isDir,
		})
	}
	return out, nil
}

// stat returns a best-effort FileInfo for a single path by listing its parent.
func (c *Client) stat(p string) (os.FileInfo, error) {
	dir := path.Dir(p)
	base := path.Base(p)
	if dir == "" {
		dir = "/"
	}
	entries, err := c.conn.List(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e != nil && e.Name == base {
			isDir := e.Type == jftp.EntryTypeFolder
			mode := os.FileMode(0o644)
			if isDir {
				mode = os.FileMode(0o755) | os.ModeDir
			}
			return ftpFileInfo{
				name:    e.Name,
				size:    ftpSizeToInt64(e.Size),
				mode:    mode,
				modTime: e.Time,
				isDir:   isDir,
			}, nil
		}
	}
	return nil, os.ErrNotExist
}

// ftpFile implements httpapi.FileTransferFile over an FTP RETR response.
type ftpFile struct {
	rc   io.ReadCloser
	info os.FileInfo
}

func (f *ftpFile) Read(p []byte) (int, error) { return f.rc.Read(p) }
func (f *ftpFile) Close() error               { return f.rc.Close() }
func (f *ftpFile) Stat() (os.FileInfo, error) { return f.info, nil }

// Open opens a remote file for reading.
func (c *Client) Open(p string) (io.ReadCloser, error) {
	if p == "" {
		return nil, fmt.Errorf("path is required")
	}
	info, err := c.stat(p)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("cannot open directory")
	}
	rc, err := c.conn.Retr(p)
	if err != nil {
		return nil, err
	}
	return &ftpFile{rc: rc, info: info}, nil
}

// ftpWriter buffers content locally and uploads on Close via STOR.
type ftpWriter struct {
	client *Client
	path   string
	buf    *bytes.Buffer
}

func (w *ftpWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *ftpWriter) Close() error {
	if w.client == nil || w.client.conn == nil {
		return nil
	}
	return w.client.conn.Stor(w.path, bytes.NewReader(w.buf.Bytes()))
}

// Create creates or truncates a remote file and returns a WriteCloser.
func (c *Client) Create(p string) (io.WriteCloser, error) {
	if p == "" {
		return nil, fmt.Errorf("path is required")
	}
	return &ftpWriter{
		client: c,
		path:   p,
		buf:    &bytes.Buffer{},
	}, nil
}

// RemoveAll removes a file or directory tree.
func (c *Client) RemoveAll(p string) error {
	if p == "" || p == "/" {
		return fmt.Errorf("refusing to remove root")
	}
	// Use RemoveDirRecur for directories; fall back to Delete for files.
	if err := c.conn.RemoveDirRecur(p); err == nil {
		return nil
	}
	return c.conn.Delete(p)
}
