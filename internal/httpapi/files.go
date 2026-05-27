package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/ftp"
	"github.com/nullpo7z/vantyx/internal/protocols"
	"github.com/nullpo7z/vantyx/internal/proxyerrors"
	"github.com/nullpo7z/vantyx/internal/secret"
	"github.com/nullpo7z/vantyx/internal/sftp"
	"github.com/nullpo7z/vantyx/internal/sshproxy"
	"github.com/nullpo7z/vantyx/internal/tftp"
)

// fileEntry is a single entry in a directory listing.
type fileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time,omitempty"`
}

// getTargetAndFileClient checks auth, loads target, verifies user has access, and returns an SFTP or TFTP client.
// The caller must call client.Close(). Returns (nil, nil) if the response was already written (error case).
func (a *App) getTargetAndFileClient(w http.ResponseWriter, r *http.Request) (*access.Target, FileTransferClient) {
	targetID := chi.URLParam(r, "target_id")
	userID, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return nil, nil
	}
	client, ok := a.openFileTransferClient(w, r, userID, string(target.ID), target)
	if !ok {
		return nil, nil
	}
	return target, client
}

// openFileTransferClient connects a file transfer client for an already-authorized target.
func (a *App) openFileTransferClient(w http.ResponseWriter, r *http.Request, userID, targetID string, target *access.Target) (FileTransferClient, bool) {
	if !protocols.SupportsFileTransfer(target.Protocol) {
		writeJSONErrorKey(w, r, "files.transferOnlySSHFTPTFTP", http.StatusBadRequest)
		return nil, false
	}
	ctx := r.Context()
	switch target.Protocol {
	case access.ProtocolTFTP:
		tftpClient, err := tftp.NewClient(ctx, target.Host, target.Port)
		if err != nil {
			audit("files_tftp_connect_failed", auditFields{
				"user_id":   userID,
				"target_id": targetID,
				"error":     err.Error(),
			})
			writeJSONErrorKey(w, r, "files.connectFailed", http.StatusBadGateway, "error", err)
			return nil, false
		}
		return &tftpClientAdapter{Client: tftpClient}, true
	case access.ProtocolSSH:
		if target.SSHUsername == "" || (target.SSHPassword == "" && target.SSHPrivateKey == "") {
			writeJSONErrorKey(w, r, "files.credentialsRequired", http.StatusBadRequest)
			return nil, false
		}
		if !target.SFTPEnabled {
			writeJSONErrorKey(w, r, "files.sftpDisabled", http.StatusForbidden)
			return nil, false
		}
		if target.SSHPrivateKey != "" && strings.HasPrefix(target.SSHPrivateKey, secret.CiphertextVersionPrefix) {
			writeJSONErrorKey(w, r, "files.credentialsDecryptFailed", http.StatusInternalServerError)
			return nil, false
		}
		if a.SFTPClientFactory != nil {
			client, err := a.SFTPClientFactory(ctx, target)
			if err != nil {
				audit("files_sftp_connect_failed", auditFields{
					"user_id":   userID,
					"target_id": targetID,
					"error":     proxyerrors.UnwrapForAudit(err),
				})
				writeProxyError(w, r, err, http.StatusBadGateway)
				return nil, false
			}
			return client, true
		}
		client, err := sftp.NewClient(r.Context(), target.Host, target.Port, target.SSHUsername, target.SSHPassword, target.SSHPrivateKey, target.SSHPrivateKeyPassphrase, sshproxy.WithHostKeyFingerprint(target.SSHHostKeyFingerprint))
		if err != nil {
			audit("files_sftp_connect_failed", auditFields{
				"user_id":   userID,
				"target_id": targetID,
				"error":     proxyerrors.UnwrapForAudit(err),
			})
			writeProxyError(w, r, err, http.StatusBadGateway)
			return nil, false
		}
		return &sftpClientAdapter{Client: client}, true
	case access.ProtocolFTP:
		if target.SSHUsername == "" || target.SSHPassword == "" {
			writeJSONErrorKey(w, r, "files.ftpCredentialsRequired", http.StatusBadRequest)
			return nil, false
		}
		client, err := ftp.NewClient(r.Context(), target.Host, target.Port, target.SSHUsername, target.SSHPassword)
		if err != nil {
			audit("files_ftp_connect_failed", auditFields{
				"user_id":   userID,
				"target_id": targetID,
				"error":     err.Error(),
			})
			writeJSONErrorKey(w, r, "files.connectFailed", http.StatusBadGateway, "error", err)
			return nil, false
		}
		return &ftpClientAdapter{Client: client}, true
	default:
		writeJSONErrorKey(w, r, "files.transferOnlySSHFTPTFTP", http.StatusBadRequest)
		return nil, false
	}
}

// sftpClientAdapter adapts *sftp.Client to FileTransferClient (Open and Create return interface types).
type sftpClientAdapter struct {
	*sftp.Client
}

func (a *sftpClientAdapter) Open(path string) (FileTransferFile, error) {
	return a.Client.Open(path)
}

func (a *sftpClientAdapter) Create(path string) (io.WriteCloser, error) {
	return a.Client.Create(path)
}

// ftpClientAdapter adapts *ftp.Client to FileTransferClient.
type ftpClientAdapter struct {
	*ftp.Client
}

func (a *ftpClientAdapter) Open(path string) (FileTransferFile, error) {
	rc, err := a.Client.Open(path)
	if err != nil {
		return nil, err
	}
	f, ok := rc.(FileTransferFile)
	if !ok {
		return nil, errors.New("ftp Open did not return a FileTransferFile")
	}
	return f, nil
}

// remotePath returns the path query parameter, defaulting to "/". No local filesystem use.
func remotePath(r *http.Request) string {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		return "/"
	}
	if !path.IsAbs(p) {
		p = "/" + p
	}
	return path.Clean(p)
}

// requireNonRootFilePath rejects paths that resolve to the filesystem root.
// Listing "/" is fine, but file operations like upload/download should not
// target "/" or ".".
func requireNonRootFilePath(w http.ResponseWriter, r *http.Request, p string) bool {
	if p == "/" || p == "." {
		writeJSONErrorKey(w, r, "common.invalidPath", http.StatusBadRequest)
		return false
	}
	return true
}

// handleListFiles returns a directory listing for the target (SFTP). GET /api/targets/{target_id}/files?path=/
func (a *App) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	target, client := a.getTargetAndFileClient(w, r)
	if client == nil {
		return
	}
	defer client.Close()

	dirPath := remotePath(r)
	entries, err := client.ReadDir(dirPath)
	if err != nil {
		audit("files_list_failed", auditFields{
			"target_id": target.ID,
			"path":      dirPath,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "files.listFailed", http.StatusBadGateway, "error", err)
		return
	}
	out := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		modTime := ""
		if !e.ModTime().IsZero() {
			modTime = e.ModTime().Format(time.RFC3339)
		}
		out = append(out, fileEntry{
			Name:    e.Name(),
			Size:    e.Size(),
			IsDir:   e.IsDir(),
			ModTime: modTime,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// handleDownloadFile streams a file from the target. GET /api/targets/{target_id}/files/download?path=/remote/file
func (a *App) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	target, client := a.getTargetAndFileClient(w, r)
	if client == nil {
		return
	}
	defer client.Close()

	filePath := remotePath(r)
	if !requireNonRootFilePath(w, r, filePath) {
		return
	}
	f, err := client.Open(filePath)
	if err != nil {
		audit("files_open_failed", auditFields{
			"target_id": target.ID,
			"path":      filePath,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "files.openFailed", http.StatusBadGateway, "error", err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeJSONErrorKey(w, r, "files.statFailed", http.StatusBadGateway, "error", err)
		return
	}
	if info.IsDir() {
		writeJSONErrorKey(w, r, "common.cannotDownloadDirectory", http.StatusBadRequest)
		return
	}
	name := path.Base(filePath)
	if name == "" || name == "." {
		name = "download"
	}
	setAttachmentDisposition(w, name)
	w.Header().Set("Content-Type", "application/octet-stream")
	if info.Size() > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// handleUploadFile uploads a file to the target. POST /api/targets/{target_id}/files/upload (multipart: path, file)
func (a *App) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	target, client := a.getTargetAndFileClient(w, r)
	if client == nil {
		return
	}
	defer client.Close()

	const maxUploadMB = 64
	const memoryThresholdMB = 8 // overflow spills to disk; bounds memory pressure under parallel uploads (CWE-770).
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadMB<<20)
	if err := r.ParseMultipartForm(memoryThresholdMB << 20); err != nil { // #nosec G120 -- bounded by MaxBytesReader above
		writeJSONErrorKey(w, r, "files.invalidMultipart", http.StatusBadRequest, "error", err)
		return
	}
	pathParam := strings.TrimSpace(r.FormValue("path"))
	if pathParam == "" {
		writeJSONErrorKey(w, r, "common.pathRequired", http.StatusBadRequest)
		return
	}
	if !path.IsAbs(pathParam) {
		pathParam = "/" + pathParam
	}
	pathParam = path.Clean(pathParam)
	if !requireNonRootFilePath(w, r, pathParam) {
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSONErrorKey(w, r, "files.fileRequired", http.StatusBadRequest, "error", err)
		return
	}
	defer file.Close()

	remoteFile, err := client.Create(pathParam)
	if err != nil {
		audit("files_create_failed", auditFields{
			"target_id": target.ID,
			"path":      pathParam,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "files.createFailed", http.StatusBadGateway, "error", err)
		return
	}
	defer remoteFile.Close()
	if _, err := io.Copy(remoteFile, file); err != nil {
		audit("files_upload_failed", auditFields{
			"target_id": target.ID,
			"path":      pathParam,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "files.uploadFailed", http.StatusBadGateway, "error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"path": pathParam, "status": "ok"})
}

// handleDeleteFile deletes a file or directory on the target. DELETE /api/targets/{target_id}/files?path=/remote/path
func (a *App) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	target, client := a.getTargetAndFileClient(w, r)
	if client == nil {
		return
	}
	defer client.Close()

	filePath := remotePath(r)
	if filePath == "/" || filePath == "." {
		writeJSONErrorKey(w, r, "files.cannotDeleteRoot", http.StatusBadRequest)
		return
	}
	if err := client.RemoveAll(filePath); err != nil {
		if errors.Is(err, errTFTPNoDelete) {
			writeJSONErrorKey(w, r, "files.tftpDeleteUnsupported", http.StatusNotImplemented)
			return
		}
		audit("files_remove_failed", auditFields{
			"target_id": target.ID,
			"path":      filePath,
			"error":     err.Error(),
		})
		writeJSONErrorKey(w, r, "files.removeFailed", http.StatusBadGateway, "error", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
