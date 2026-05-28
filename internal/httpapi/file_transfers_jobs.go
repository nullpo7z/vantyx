package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/filetransfer"
)

// stageUploadFile streams the uploaded multipart payload to a temp
// file inside the transfer manager's staging directory and updates job
// progress as bytes are written.
func (a *App) stageUploadFile(job *filetransfer.Job, src io.Reader, total int64) (string, error) {
	f, err := os.CreateTemp(a.FileTransferManager.TempDir(), "upload-*")
	if err != nil {
		return "", err
	}
	tempPath := f.Name()
	var written int64
	buf := make([]byte, 32*1024)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := f.Write(buf[0:nr])
			written += int64(nw)
			job.SetProgress(written, total)
			if ew != nil {
				_ = f.Close()
				_ = os.Remove(tempPath)
				return "", ew
			}
		}
		if er != nil {
			if er == io.EOF {
				break
			}
			_ = f.Close()
			_ = os.Remove(tempPath)
			return "", er
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	return tempPath, nil
}

// runDownloadJob is the background goroutine that fetches a remote
// file into the staging directory and marks the job completed when the
// SPA can collect it via /api/file-transfers/{id}/content.
func (a *App) runDownloadJob(ctx context.Context, job *filetransfer.Job, target *access.Target, backend filetransfer.Backend, remotePath, transfer string) {
	tempPath, err := os.CreateTemp(a.FileTransferManager.TempDir(), "download-*")
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	tempName := tempPath.Name()
	_ = tempPath.Close()

	defer func() {
		if snap := job.Snapshot(); snap.State != string(filetransfer.StateCompleted) {
			_ = os.Remove(tempName)
		}
	}()

	select {
	case <-ctx.Done():
		job.SetState(filetransfer.StateCancelled, "")
		return
	default:
	}

	switch backend {
	case filetransfer.BackendRemote:
		a.runRemoteDownload(ctx, job, target, remotePath, tempName, transfer)
	case filetransfer.BackendTFTPServer:
		a.runTFTPServerDownload(ctx, job, target, remotePath, tempName)
	default:
		a.failTransferJob(job, "unknown backend")
	}
}

// runRemoteDownload pulls a file from an SFTP / FTP target.
func (a *App) runRemoteDownload(ctx context.Context, job *filetransfer.Job, target *access.Target, remotePath, tempName, transfer string) {
	client, err := a.openFileTransferClientNoHTTP(ctx, job.UserID, string(target.ID), target, transfer)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	defer client.Close()

	f, err := client.Open(remotePath)
	if err != nil {
		a.failTransferJob(job, "open failed: "+err.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.failTransferJob(job, "stat failed: "+err.Error())
		return
	}
	if info.IsDir() {
		a.failTransferJob(job, "cannot download a directory")
		return
	}
	total := info.Size()
	out, err := os.Create(tempName)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	if err := progressCopy(job, out, f, total); err != nil {
		_ = out.Close()
		a.failTransferJob(job, err.Error())
		return
	}
	if err := out.Close(); err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	select {
	case <-ctx.Done():
		job.SetState(filetransfer.StateCancelled, "")
		_ = os.Remove(tempName)
		return
	default:
	}
	job.SetTempPath(tempName)
	job.SetState(filetransfer.StateCompleted, "")
}

// runTFTPServerDownload reads a file from the embedded TFTP server's
// per-target root and stages it for HTTP delivery.
func (a *App) runTFTPServerDownload(ctx context.Context, job *filetransfer.Job, target *access.Target, remotePath, tempName string) {
	full, err := tftpServerFullPath(target.ID, remotePath)
	if err != nil {
		a.failTransferJob(job, "invalid path")
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	if info.IsDir() {
		a.failTransferJob(job, "cannot download a directory")
		return
	}
	in, err := os.Open(full)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	defer in.Close()
	out, err := os.Create(tempName)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	if err := progressCopy(job, out, in, info.Size()); err != nil {
		_ = out.Close()
		a.failTransferJob(job, err.Error())
		return
	}
	if err := out.Close(); err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	select {
	case <-ctx.Done():
		job.SetState(filetransfer.StateCancelled, "")
		_ = os.Remove(tempName)
		return
	default:
	}
	job.SetTempPath(tempName)
	job.SetState(filetransfer.StateCompleted, "")
}

// runUploadJob is the background goroutine that pushes a previously
// staged file to the chosen backend.
func (a *App) runUploadJob(ctx context.Context, job *filetransfer.Job, target *access.Target, backend filetransfer.Backend, remotePath, tempPath, transfer string) {
	defer a.cleanupTransferTemp(tempPath)
	select {
	case <-ctx.Done():
		job.SetState(filetransfer.StateCancelled, "")
		return
	default:
	}
	switch backend {
	case filetransfer.BackendRemote:
		a.runRemoteUpload(ctx, job, target, remotePath, tempPath, transfer)
	case filetransfer.BackendTFTPServer:
		a.runTFTPServerUpload(ctx, job, target, remotePath, tempPath)
	default:
		a.failTransferJob(job, "unknown backend")
	}
}

// runRemoteUpload pushes a staged file to an SFTP / FTP target.
func (a *App) runRemoteUpload(ctx context.Context, job *filetransfer.Job, target *access.Target, remotePath, tempPath, transfer string) {
	client, err := a.openFileTransferClientNoHTTP(ctx, job.UserID, string(target.ID), target, transfer)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	defer client.Close()
	info, err := os.Stat(tempPath)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	in, err := os.Open(tempPath)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	defer in.Close()
	remoteFile, err := client.Create(remotePath)
	if err != nil {
		a.failTransferJob(job, "create failed: "+err.Error())
		return
	}
	defer remoteFile.Close()
	if err := progressCopy(job, remoteFile, in, info.Size()); err != nil {
		a.failTransferJob(job, "upload failed: "+err.Error())
		return
	}
	select {
	case <-ctx.Done():
		job.SetState(filetransfer.StateCancelled, "")
		return
	default:
	}
	job.SetState(filetransfer.StateCompleted, "")
	audit("files_upload_ok", auditFields{
		"target_id":   string(target.ID),
		"path":        remotePath,
		"transfer_id": job.ID,
	})
}

// runTFTPServerUpload copies a staged file into the embedded TFTP
// server's per-target root.
func (a *App) runTFTPServerUpload(ctx context.Context, job *filetransfer.Job, target *access.Target, remotePath, tempPath string) {
	full, err := tftpServerFullPath(target.ID, remotePath)
	if err != nil {
		a.failTransferJob(job, "invalid path")
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	in, err := os.Open(tempPath)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	defer in.Close()
	dst, err := os.Create(full)
	if err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	if err := progressCopy(job, dst, in, info.Size()); err != nil {
		_ = dst.Close()
		_ = os.Remove(full)
		a.failTransferJob(job, err.Error())
		return
	}
	if err := dst.Close(); err != nil {
		a.failTransferJob(job, err.Error())
		return
	}
	select {
	case <-ctx.Done():
		job.SetState(filetransfer.StateCancelled, "")
		_ = os.Remove(full)
		return
	default:
	}
	job.SetState(filetransfer.StateCompleted, "")
}

// failTransferJob marks a job as failed with the supplied user-visible
// message. Kept as a method so future callers can hook in audit events.
func (a *App) failTransferJob(job *filetransfer.Job, msg string) {
	job.SetState(filetransfer.StateFailed, msg)
}

// cleanupTransferJob removes the staging file (if any) and drops the
// job from the in-memory registry.
func (a *App) cleanupTransferJob(j *filetransfer.Job) {
	a.cleanupTransferTemp(j.GetTempPath())
	a.FileTransferManager.Remove(j.ID)
}

// cleanupTransferTemp removes the named temp file if non-empty.
func (a *App) cleanupTransferTemp(temp string) {
	if temp != "" {
		_ = os.Remove(temp)
	}
}

// openFileTransferClientNoHTTP connects for background jobs and
// returns errors instead of HTTP responses. It reuses the HTTP-facing
// [App.openFileTransferClient] by capturing its response in a recorder
// and extracting the JSON message.
func (a *App) openFileTransferClientNoHTTP(ctx context.Context, userID, targetID string, target *access.Target, transfer string) (FileTransferClient, error) {
	rec := &responseRecorder{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	if transfer != "" {
		q := req.URL.Query()
		q.Set("transfer", transfer)
		req.URL.RawQuery = q.Encode()
	}
	client, ok := a.openFileTransferClient(rec, req, userID, targetID, target)
	if !ok || client == nil {
		var er struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal([]byte(rec.body.String()), &er)
		if er.Message != "" {
			return nil, errors.New(er.Message)
		}
		return nil, errors.New("failed to connect")
	}
	return client, nil
}

// responseRecorder captures handler output for background file client
// setup. It implements just enough of [http.ResponseWriter] to capture
// status, body, and headers.
type responseRecorder struct {
	header http.Header
	body   strings.Builder
	status int
}

func (r *responseRecorder) Header() http.Header  { return r.header }
func (r *responseRecorder) WriteHeader(code int) { r.status = code }
func (r *responseRecorder) Write(b []byte) (int, error) {
	_, _ = r.body.Write(b)
	return len(b), nil
}
