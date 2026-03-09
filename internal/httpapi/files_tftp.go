package httpapi

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/tftp"
)

// errTFTPNoDelete is returned by RemoveAll for TFTP (protocol does not support delete).
var errTFTPNoDelete = errors.New("TFTP does not support delete")

// tftpClientAdapter adapts *tftp.Client to FileTransferClient.
// TFTP has no directory listing; ReadDir returns empty. RemoveAll returns errTFTPNoDelete.
type tftpClientAdapter struct {
	*tftp.Client
}

func (a *tftpClientAdapter) ReadDir(string) ([]os.FileInfo, error) {
	return nil, nil
}

func (a *tftpClientAdapter) Open(path string) (FileTransferFile, error) {
	// TFTP typically expects filename without leading slash.
	remote := strings.TrimPrefix(path, "/")
	if remote == "" {
		remote = path
	}
	r, size, err := a.Client.Get(remote)
	if err != nil {
		return nil, err
	}
	return &tftpFile{ReadCloser: r, size: size, name: filepath.Base(path)}, nil
}

func (a *tftpClientAdapter) Create(path string) (io.WriteCloser, error) {
	remote := strings.TrimPrefix(path, "/")
	if remote == "" {
		remote = path
	}
	return &tftpWriteCloser{client: a.Client, path: remote}, nil
}

func (a *tftpClientAdapter) RemoveAll(string) error {
	return errTFTPNoDelete
}

type tftpFile struct {
	io.ReadCloser
	size int64
	name string
}

func (f *tftpFile) Stat() (os.FileInfo, error) {
	return &tftpFileInfo{name: f.name, size: f.size}, nil
}

type tftpFileInfo struct {
	name string
	size int64
}

func (i *tftpFileInfo) Name() string       { return i.name }
func (i *tftpFileInfo) Size() int64        { return i.size }
func (i *tftpFileInfo) Mode() os.FileMode  { return 0 }
func (i *tftpFileInfo) ModTime() time.Time { return time.Time{} }
func (i *tftpFileInfo) IsDir() bool        { return false }
func (i *tftpFileInfo) Sys() interface{}   { return nil }

// tftpWriteCloser buffers writes and on Close uploads via TFTP Put.
type tftpWriteCloser struct {
	buf    bytes.Buffer
	client *tftp.Client
	path   string
}

func (w *tftpWriteCloser) Write(p []byte) (n int, err error) {
	return w.buf.Write(p)
}

func (w *tftpWriteCloser) Close() error {
	return w.client.Put(w.path, &w.buf)
}
