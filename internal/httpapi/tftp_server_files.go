package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
)

// tftpServerRoot returns the absolute filesystem root for TFTP server data.
// It mirrors the default used in cmd/vantyx-server/main.go.
func tftpServerRoot() string {
	root := strings.TrimSpace(os.Getenv("VANTYX_TFTP_ROOT"))
	if root == "" {
		root = "/app/data/tftp"
	}
	return root
}

// tftpServerTargetRoot returns the per-target directory under the TFTP root.
func tftpServerTargetRoot(targetID access.TargetID) string {
	root := tftpServerRoot()
	return filepath.Join(root, string(targetID))
}

// getTFTPServerTarget checks auth and permissions, and returns the TFTP target.
func (a *App) getTFTPServerTarget(w http.ResponseWriter, r *http.Request) *access.Target {
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	targetID := chi.URLParam(r, "target_id")
	if targetID == "" {
		writeJSONError(w, "target_id required", http.StatusBadRequest)
		return nil
	}
	ctx := r.Context()
	target, err := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err != nil {
		writeJSONError(w, "target not found", http.StatusNotFound)
		return nil
	}
	if target.Protocol != access.ProtocolTFTP {
		writeJSONError(w, "target is not a TFTP server", http.StatusBadRequest)
		return nil
	}
	allowed, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return nil
	}
	allowedSet := make(map[access.TargetID]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	if _, ok := allowedSet[access.TargetID(targetID)]; !ok {
		writeJSONError(w, "forbidden", http.StatusForbidden)
		return nil
	}
	return target
}

// tftpServerFullPath builds a safe absolute path under the per-target TFTP directory.
func tftpServerFullPath(targetID access.TargetID, relPath string) (string, error) {
	base := filepath.Clean(tftpServerTargetRoot(targetID))
	if relPath == "" || relPath == "/" {
		return base, nil
	}
	rel := strings.TrimPrefix(relPath, "/")
	full := filepath.Clean(filepath.Join(base, rel))
	if !strings.HasPrefix(full, base+string(os.PathSeparator)) && full != base {
		return "", errors.New("invalid path")
	}
	return full, nil
}

// handleTFTPServerListFiles lists files under the Vantyx TFTP server directory for a target.
// GET /api/tftp/targets/{target_id}/files?path=/
func (a *App) handleTFTPServerListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		p = "/"
	}
	full, err := tftpServerFullPath(target.ID, p)
	if err != nil {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		if os.IsNotExist(err) {
			// no files yet – treat as empty directory
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
			return
		}
		writeInternalError(w, err)
		return
	}
	out := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		modTime := ""
		if !info.ModTime().IsZero() {
			modTime = info.ModTime().Format(time.RFC3339)
		}
		out = append(out, fileEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			IsDir:   e.IsDir(),
			ModTime: modTime,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// handleTFTPServerDownloadFile downloads a single file from the Vantyx TFTP directory.
// GET /api/tftp/targets/{target_id}/files/download?path=/foo/bar.txt
func (a *App) handleTFTPServerDownloadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeJSONError(w, "path is required", http.StatusBadRequest)
		return
	}
	full, err := tftpServerFullPath(target.ID, p)
	if err != nil {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, "file not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if info.IsDir() {
		writeJSONError(w, "cannot download a directory", http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer f.Close()

	name := filepath.Base(full)
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

// handleTFTPServerUploadFile uploads a file into the Vantyx TFTP directory for a target.
// POST /api/tftp/targets/{target_id}/files/upload (multipart: path, file)
func (a *App) handleTFTPServerUploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}

	const maxUploadMB = 64
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadMB<<20)
	if err := r.ParseMultipartForm(maxUploadMB << 20); err != nil {
		writeJSONError(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}
	pathParam := strings.TrimSpace(r.FormValue("path"))
	if pathParam == "" {
		writeJSONError(w, "path is required", http.StatusBadRequest)
		return
	}
	full, err := tftpServerFullPath(target.ID, pathParam)
	if err != nil {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		writeInternalError(w, err)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, "file is required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	dst, err := os.Create(full)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleTFTPServerDeleteFile deletes a file (or empty directory) in the Vantyx TFTP directory.
// DELETE /api/tftp/targets/{target_id}/files?path=/foo/bar.txt
func (a *App) handleTFTPServerDeleteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := a.getTFTPServerTarget(w, r)
	if target == nil {
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeJSONError(w, "path is required", http.StatusBadRequest)
		return
	}
	full, err := tftpServerFullPath(target.ID, p)
	if err != nil {
		writeJSONError(w, "invalid path", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, "file not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if info.IsDir() {
		entries, _ := os.ReadDir(full)
		if len(entries) > 0 {
			writeJSONError(w, "directory is not empty", http.StatusBadRequest)
			return
		}
	}
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, "file not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

