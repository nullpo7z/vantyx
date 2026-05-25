// Package ftp is the FTP client adapter used by Vantyx's file transfer
// pages.
//
// It wraps the [github.com/jlaffaye/ftp] driver, normalises directory
// listings, and provides the small subset of operations the HTTP API
// needs: open, create, read directory, and remove. The adapter
// satisfies the [internal/httpapi.FileTransferClient] interface so
// FTP targets share code with SFTP and TFTP transfers.
package ftp
