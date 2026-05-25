package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/filetransfer"
	"github.com/nullpo7z/vantyx/internal/protocols"
)

const maxFileTransferMB = 64

// progressCopy copies from src to dst while updating job progress.
func progressCopy(job *filetransfer.Job, dst io.Writer, src io.Reader, total int64) error {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			written += int64(nw)
			job.SetProgress(written, total)
			if ew != nil {
				return ew
			}
			if nr != nw {
				return io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return nil
			}
			return er
		}
	}
}

// fileTransferCursorSeparator separates the RFC3339Nano timestamp and the job id
// inside an opaque (base64) cursor. The dot/space pair is unlikely to appear in
// either part.
const fileTransferCursorSeparator = " | "

// encodeFileTransferCursor returns a base64-encoded "updated_at|id" cursor.
func encodeFileTransferCursor(updated time.Time, id string) string {
	if id == "" {
		return ""
	}
	raw := updated.UTC().Format(time.RFC3339Nano) + fileTransferCursorSeparator + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeFileTransferCursor parses a cursor produced by encodeFileTransferCursor.
func decodeFileTransferCursor(s string) (time.Time, string, error) {
	if s == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid after_cursor")
	}
	parts := strings.SplitN(string(raw), fileTransferCursorSeparator, 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid after_cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid after_cursor")
	}
	return t.UTC(), parts[1], nil
}

// parseFileTransferStates parses repeated state query values into typed States.
func parseFileTransferStates(raw []string) ([]filetransfer.State, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]filetransfer.State, 0, len(raw))
	for _, v := range raw {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			switch filetransfer.State(part) {
			case filetransfer.StateReceiving, filetransfer.StateRunning,
				filetransfer.StateCompleted, filetransfer.StateFailed, filetransfer.StateCancelled:
				out = append(out, filetransfer.State(part))
			default:
				return nil, fmt.Errorf("invalid state: %s", part)
			}
		}
	}
	return out, nil
}

// parseFileTransferDirection returns the typed direction or "" for empty input.
func parseFileTransferDirection(s string) (filetransfer.Direction, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	switch filetransfer.Direction(s) {
	case filetransfer.DirectionUpload, filetransfer.DirectionDownload:
		return filetransfer.Direction(s), nil
	}
	return "", fmt.Errorf("invalid direction: %s", s)
}

// parseFileTransferBackend returns the typed backend or "" for empty input.
func parseFileTransferBackend(s string) (filetransfer.Backend, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	switch filetransfer.Backend(s) {
	case filetransfer.BackendRemote, filetransfer.BackendTFTPServer:
		return filetransfer.Backend(s), nil
	}
	return "", fmt.Errorf("invalid backend: %s", s)
}

// fileTransfersListResponse is the GET /api/file-transfers payload.
type fileTransfersListResponse struct {
	Items      []filetransfer.JobSnapshot `json:"items"`
	NextCursor string                     `json:"next_cursor,omitempty"`
}

func (a *App) handleFileTransfersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query()

	// Detect admin to allow user_id override / "all users" listing.
	isAdmin := false
	if u, err := a.UserStore.GetByID(userID); err == nil && u != nil && u.Role == auth.RoleAdmin {
		isAdmin = true
	}

	// Determine target user filter. Non-admins are always pinned to themselves.
	targetUserID := userID
	if isAdmin {
		if v := strings.TrimSpace(q.Get("user_id")); v != "" {
			targetUserID = v
		}
	}

	// If no filtering parameters are supplied, keep backwards-compatible
	// behaviour: return the user's recent jobs (no pagination).
	hasFilterParams := false
	for _, k := range []string{
		"limit", "query", "target_id", "direction", "backend", "state",
		"from", "to", "after_cursor", "user_id",
	} {
		if _, ok := q[k]; ok {
			hasFilterParams = true
			break
		}
	}
	if !hasFilterParams {
		jobs := a.FileTransferManager.ListByUser(targetUserID)
		items := make([]filetransfer.JobSnapshot, 0, len(jobs))
		for _, j := range jobs {
			items = append(items, j.Snapshot())
		}
		writeJSON(w, fileTransfersListResponse{Items: items})
		return
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	pageLimit := limit
	if pageLimit <= 0 || pageLimit > 500 {
		pageLimit = 100
	}

	states, err := parseFileTransferStates(q["state"])
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	dir, err := parseFileTransferDirection(q.Get("direction"))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	backend, err := parseFileTransferBackend(q.Get("backend"))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	afterUpdated, afterID, err := decodeFileTransferCursor(q.Get("after_cursor"))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	from, to, err := parseTimeRange(q.Get("from"), q.Get("to"), time.Now().UTC())
	if err != nil {
		writeTimeRangeError(w, err)
		return
	}

	filter := filetransfer.ListFilter{
		UserID:       targetUserID,
		Query:        strings.TrimSpace(q.Get("query")),
		TargetID:     strings.TrimSpace(q.Get("target_id")),
		Direction:    dir,
		Backend:      backend,
		States:       states,
		From:         from,
		To:           to,
		AfterUpdated: afterUpdated,
		AfterID:      afterID,
		Limit:        pageLimit + 1,
	}

	jobs, err := a.FileTransferManager.ListByUserFiltered(r.Context(), filter)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	items := make([]filetransfer.JobSnapshot, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, j.Snapshot())
	}
	var nextCursor string
	if len(items) > pageLimit {
		last := items[pageLimit-1]
		// last.UpdatedAt is RFC3339; parse back to time for the cursor.
		if t, perr := time.Parse(time.RFC3339, last.UpdatedAt); perr == nil {
			nextCursor = encodeFileTransferCursor(t, last.ID)
		}
		items = items[:pageLimit]
	}
	writeJSON(w, fileTransfersListResponse{Items: items, NextCursor: nextCursor})
}

func (a *App) handleFileTransferGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "transfer_id")
	j, ok := a.FileTransferManager.Get(id)
	if !ok || j.UserID != userID {
		writeJSONError(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, j.Snapshot())
}

func (a *App) handleFileTransferDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "transfer_id")
	if err := a.FileTransferManager.Cancel(id, userID); err != nil {
		if errors.Is(err, filetransfer.ErrNotFound) {
			writeJSONError(w, "not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, filetransfer.ErrForbidden) {
			writeJSONError(w, "forbidden", http.StatusForbidden)
			return
		}
		writeInternalError(w, err)
		return
	}
	j, ok := a.FileTransferManager.Get(id)
	if ok {
		a.cleanupTransferJob(j)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFileTransferContent serves a completed download staging file.
// GET /api/file-transfers/{transfer_id}/content
func (a *App) handleFileTransferContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "transfer_id")
	j, ok := a.FileTransferManager.Get(id)
	if !ok || j.UserID != userID {
		writeJSONError(w, "not found", http.StatusNotFound)
		return
	}
	snap := j.Snapshot()
	if snap.Direction != string(filetransfer.DirectionDownload) {
		writeJSONError(w, "not a download transfer", http.StatusBadRequest)
		return
	}
	if snap.State != string(filetransfer.StateCompleted) {
		writeJSONError(w, "transfer not ready", http.StatusConflict)
		return
	}
	tempPath := j.GetTempPath()
	fileName := j.GetFileName()
	if tempPath == "" {
		writeJSONError(w, "transfer not ready", http.StatusConflict)
		return
	}
	f, err := os.Open(tempPath)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fileName+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	if info.Size() > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

type fileTransferDownloadRequest struct {
	Backend  string `json:"backend"`
	TargetID string `json:"target_id"`
	Path     string `json:"path"`
}

// handleFileTransferStartDownload starts a background download. POST /api/file-transfers/download
func (a *App) handleFileTransferStartDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req fileTransferDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	backend := filetransfer.Backend(strings.TrimSpace(req.Backend))
	if backend != filetransfer.BackendRemote && backend != filetransfer.BackendTFTPServer {
		writeJSONError(w, "backend must be remote or tftp_server", http.StatusBadRequest)
		return
	}
	targetID := strings.TrimSpace(req.TargetID)
	pathParam := strings.TrimSpace(req.Path)
	if targetID == "" || pathParam == "" {
		writeJSONError(w, "target_id and path are required", http.StatusBadRequest)
		return
	}
	if !path.IsAbs(pathParam) {
		pathParam = "/" + pathParam
	}
	pathParam = path.Clean(pathParam)
	_, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}
	fileName := path.Base(pathParam)
	if fileName == "" || fileName == "." {
		fileName = "download"
	}
	ctx, cancel := context.WithCancel(context.Background())
	job, err := a.FileTransferManager.Create(filetransfer.CreateOpts{
		UserID:       userID,
		TargetID:     targetID,
		TargetName:   target.Name,
		Backend:      backend,
		Direction:    filetransfer.DirectionDownload,
		RemotePath:   pathParam,
		FileName:     fileName,
		InitialState: filetransfer.StateRunning,
	}, cancel)
	if err != nil {
		cancel()
		writeInternalError(w, err)
		return
	}
	go func() {
		defer cancel()
		a.runDownloadJob(ctx, job, target, backend, pathParam)
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(job.Snapshot())
}

// handleFileTransferUpload receives a file into staging then completes transfer in background.
// POST /api/file-transfers/upload (multipart: backend, target_id, path, file)
func (a *App) handleFileTransferUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferMB<<20)
	if err := r.ParseMultipartForm(maxFileTransferMB << 20); err != nil { // #nosec G120
		writeJSONError(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}
	backend := filetransfer.Backend(strings.TrimSpace(r.FormValue("backend")))
	if backend != filetransfer.BackendRemote && backend != filetransfer.BackendTFTPServer {
		writeJSONError(w, "backend must be remote or tftp_server", http.StatusBadRequest)
		return
	}
	targetID := strings.TrimSpace(r.FormValue("target_id"))
	pathParam := strings.TrimSpace(r.FormValue("path"))
	if targetID == "" || pathParam == "" {
		writeJSONError(w, "target_id and path are required", http.StatusBadRequest)
		return
	}
	if !path.IsAbs(pathParam) {
		pathParam = "/" + pathParam
	}
	pathParam = path.Clean(pathParam)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, "file is required: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	_, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}
	if backend == filetransfer.BackendRemote && !protocols.SupportsFileTransfer(target.Protocol) {
		writeJSONError(w, "file transfer not supported for this target", http.StatusBadRequest)
		return
	}
	if backend == filetransfer.BackendTFTPServer && !protocols.Supports(target.Protocol, protocols.CapabilityTFTPServer) {
		writeJSONError(w, "target is not a TFTP server", http.StatusBadRequest)
		return
	}
	fileName := path.Base(pathParam)
	if fileName == "" || fileName == "." {
		if hdr != nil && hdr.Filename != "" {
			fileName = path.Base(hdr.Filename)
		} else {
			fileName = "upload"
		}
	}
	total := int64(0)
	if hdr != nil {
		total = hdr.Size
	}
	ctx, cancel := context.WithCancel(context.Background())
	job, err := a.FileTransferManager.Create(filetransfer.CreateOpts{
		UserID:       userID,
		TargetID:     targetID,
		TargetName:   target.Name,
		Backend:      backend,
		Direction:    filetransfer.DirectionUpload,
		RemotePath:   pathParam,
		FileName:     fileName,
		InitialState: filetransfer.StateReceiving,
		Total:        total,
	}, cancel)
	if err != nil {
		cancel()
		writeInternalError(w, err)
		return
	}
	tempPath, err := a.stageUploadFile(job, file, total)
	if err != nil {
		cancel()
		a.failTransferJob(job, err.Error())
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	job.SetTempPath(tempPath)
	job.SetState(filetransfer.StateRunning, "")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(job.Snapshot())
	go func() {
		defer cancel()
		a.runUploadJob(ctx, job, target, backend, pathParam, tempPath)
	}()
}

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

func (a *App) runDownloadJob(ctx context.Context, job *filetransfer.Job, target *access.Target, backend filetransfer.Backend, remotePath string) {
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
		a.runRemoteDownload(ctx, job, target, remotePath, tempName)
	case filetransfer.BackendTFTPServer:
		a.runTFTPServerDownload(ctx, job, target, remotePath, tempName)
	default:
		a.failTransferJob(job, "unknown backend")
	}
}

func (a *App) runRemoteDownload(ctx context.Context, job *filetransfer.Job, target *access.Target, remotePath, tempName string) {
	client, err := a.openFileTransferClientNoHTTP(ctx, job.UserID, string(target.ID), target)
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

func (a *App) runUploadJob(ctx context.Context, job *filetransfer.Job, target *access.Target, backend filetransfer.Backend, remotePath, tempPath string) {
	defer a.cleanupTransferTemp(tempPath)
	select {
	case <-ctx.Done():
		job.SetState(filetransfer.StateCancelled, "")
		return
	default:
	}
	switch backend {
	case filetransfer.BackendRemote:
		a.runRemoteUpload(ctx, job, target, remotePath, tempPath)
	case filetransfer.BackendTFTPServer:
		a.runTFTPServerUpload(ctx, job, target, remotePath, tempPath)
	default:
		a.failTransferJob(job, "unknown backend")
	}
}

func (a *App) runRemoteUpload(ctx context.Context, job *filetransfer.Job, target *access.Target, remotePath, tempPath string) {
	client, err := a.openFileTransferClientNoHTTP(ctx, job.UserID, string(target.ID), target)
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

func (a *App) failTransferJob(job *filetransfer.Job, msg string) {
	job.SetState(filetransfer.StateFailed, msg)
}

func (a *App) cleanupTransferJob(j *filetransfer.Job) {
	a.cleanupTransferTemp(j.GetTempPath())
	a.FileTransferManager.Remove(j.ID)
}

func (a *App) cleanupTransferTemp(temp string) {
	if temp != "" {
		_ = os.Remove(temp)
	}
}

// openFileTransferClientNoHTTP connects for background jobs and returns errors instead of HTTP responses.
func (a *App) openFileTransferClientNoHTTP(ctx context.Context, userID, targetID string, target *access.Target) (FileTransferClient, error) {
	rec := &responseRecorder{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
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

// responseRecorder captures handler output for background file client setup.
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
