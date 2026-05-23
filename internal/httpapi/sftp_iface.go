package httpapi

import (
	"context"
	"io"
	"os"

	"github.com/nullpo7z/vantyx/internal/access"
)

// FileTransferFile is the minimal interface for a file opened from an SFTP client (Read, Close, Stat).
type FileTransferFile interface {
	io.Reader
	io.Closer
	Stat() (os.FileInfo, error)
}

// FileTransferClient is the minimal SFTP client interface used by file transfer handlers.
// The real *sftp.Client and the mock both implement this.
type FileTransferClient interface {
	Close() error
	ReadDir(path string) ([]os.FileInfo, error)
	Open(path string) (FileTransferFile, error)
	Create(path string) (io.WriteCloser, error)
	RemoveAll(path string) error
}

// SFTPClientFactoryFunc creates an SFTP client for the given target.
// When set on App (e.g. in tests), it is used instead of connecting to a real SSH server.
type SFTPClientFactoryFunc func(ctx context.Context, target *access.Target) (FileTransferClient, error)
