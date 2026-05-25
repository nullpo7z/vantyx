// Package sftp is the SFTP client adapter used by Vantyx's file
// transfer pages.
//
// It wraps [github.com/pkg/sftp] together with the SSH transport from
// [golang.org/x/crypto/ssh] and exposes the small subset of operations
// the HTTP API needs: open, create, read directory, remove. The adapter
// satisfies the [internal/httpapi.FileTransferClient] interface so
// SFTP targets share code with FTP and TFTP transfers.
package sftp
