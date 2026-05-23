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
	if !protocols.SupportsFileTransfer(target.Protocol) {
		writeJSONError(w, "file transfer only for SSH, FTP, or TFTP targets", http.StatusBadRequest)
		return nil, nil
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
			writeJSONError(w, "failed to connect to target: "+err.Error(), http.StatusBadGateway)
			return nil, nil
		}
		return target, &tftpClientAdapter{Client: tftpClient}
	case access.ProtocolSSH:
		if target.SSHUsername == "" || (target.SSHPassword == "" && target.SSHPrivateKey == "") {
			writeJSONError(w, "stored credentials (password or SSH key) required for file transfer", http.StatusBadRequest)
			return nil, nil
		}
		if !target.SFTPEnabled {
			writeJSONError(w, "SFTP file transfer is disabled for this target", http.StatusForbidden)
			return nil, nil
		}
		if target.SSHPrivateKey != "" && strings.HasPrefix(target.SSHPrivateKey, secret.CiphertextVersionPrefix) {
			writeJSONError(w, "保存された認証情報の復号に失敗しています。VANTYX_ENCRYPTION_KEY を確認してください", http.StatusInternalServerError)
			return nil, nil
		}
		if a.SFTPClientFactory != nil {
			client, err := a.SFTPClientFactory(ctx, target)
			if err != nil {
				audit("files_sftp_connect_failed", auditFields{
					"user_id":   userID,
					"target_id": targetID,
					"error":     proxyerrors.UnwrapForAudit(err),
				})
				writeJSONError(w, proxyerrors.BridgeErrorMessage(err), http.StatusBadGateway)
				return nil, nil
			}
			return target, client
		}
		client, err := sftp.NewClient(r.Context(), target.Host, target.Port, target.SSHUsername, target.SSHPassword, target.SSHPrivateKey, target.SSHPrivateKeyPassphrase)
		if err != nil {
			audit("files_sftp_connect_failed", auditFields{
				"user_id":   userID,
				"target_id": targetID,
				"error":     proxyerrors.UnwrapForAudit(err),
			})
			writeJSONError(w, proxyerrors.BridgeErrorMessage(err), http.StatusBadGateway)
			return nil, nil
		}
		return target, &sftpClientAdapter{Client: client}
	case access.ProtocolFTP:
		if target.SSHUsername == "" || target.SSHPassword == "" {
			writeJSONError(w, "stored username and password are required for FTP file transfer", http.StatusBadRequest)
			return nil, nil
		}
		client, err := ftp.NewClient(r.Context(), target.Host, target.Port, target.SSHUsername, target.SSHPassword)
		if err != nil {
			audit("files_ftp_connect_failed", auditFields{
				"user_id":   userID,
				"target_id": targetID,
				"error":     err.Error(),
			})
			writeJSONError(w, "failed to connect to target: "+err.Error(), http.StatusBadGateway)
			return nil, nil
		}
		return target, &ftpClientAdapter{Client: client}
	default:
		writeJSONError(w, "file transfer only for SSH, FTP, or TFTP targets", http.StatusBadRequest)
		return nil, nil
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

// handleListFiles returns a directory listing for the target (SFTP). GET /api/targets/{target_id}/files?path=/
func (a *App) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
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
		writeJSONError(w, "list failed: "+err.Error(), http.StatusBadGateway)
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
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target, client := a.getTargetAndFileClient(w, r)
	if client == nil {
		return
	}
	defer client.Close()

	filePath := remotePath(r)
	f, err := client.Open(filePath)
	if err != nil {
		audit("files_open_failed", auditFields{
			"target_id": target.ID,
			"path":      filePath,
			"error":     err.Error(),
		})
		writeJSONError(w, "open failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeJSONError(w, "stat failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if info.IsDir() {
		writeJSONError(w, "cannot download a directory", http.StatusBadRequest)
		return
	}
	name := path.Base(filePath)
	if name == "" || name == "." {
		name = "download"
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
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
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target, client := a.getTargetAndFileClient(w, r)
	if client == nil {
		return
	}
	defer client.Close()

	const maxUploadMB = 64
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadMB<<20)
	if err := r.ParseMultipartForm(maxUploadMB << 20); err != nil { // #nosec G120 -- bounded by maxUploadMB and MaxBytesReader above
		writeJSONError(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}
	pathParam := strings.TrimSpace(r.FormValue("path"))
	if pathParam == "" {
		writeJSONError(w, "path is required", http.StatusBadRequest)
		return
	}
	if !path.IsAbs(pathParam) {
		pathParam = "/" + pathParam
	}
	pathParam = path.Clean(pathParam)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, "file is required: "+err.Error(), http.StatusBadRequest)
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
		writeJSONError(w, "create failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer remoteFile.Close()
	if _, err := io.Copy(remoteFile, file); err != nil {
		audit("files_upload_failed", auditFields{
			"target_id": target.ID,
			"path":      pathParam,
			"error":     err.Error(),
		})
		writeJSONError(w, "upload failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"path": pathParam, "status": "ok"})
}

// handleDeleteFile deletes a file or directory on the target. DELETE /api/targets/{target_id}/files?path=/remote/path
func (a *App) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target, client := a.getTargetAndFileClient(w, r)
	if client == nil {
		return
	}
	defer client.Close()

	filePath := remotePath(r)
	if filePath == "/" || filePath == "." {
		writeJSONError(w, "cannot delete root", http.StatusBadRequest)
		return
	}
	if err := client.RemoveAll(filePath); err != nil {
		if errors.Is(err, errTFTPNoDelete) {
			writeJSONError(w, "TFTP does not support delete", http.StatusNotImplemented)
			return
		}
		audit("files_remove_failed", auditFields{
			"target_id": target.ID,
			"path":      filePath,
			"error":     err.Error(),
		})
		writeJSONError(w, "remove failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
