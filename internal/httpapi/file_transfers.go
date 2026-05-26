package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
	"github.com/nullpo7z/vantyx/internal/filetransfer"
	"github.com/nullpo7z/vantyx/internal/protocols"
)

// maxFileTransferMB is the maximum multipart upload size, in MB.
const maxFileTransferMB = 64

// handleFileTransfersList returns a paginated list of file transfer
// jobs. Non-admins always see only their own jobs.
//
//nolint:gocyclo // listing builds up a multi-field filter from the query string.
func (a *App) handleFileTransfersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query()

	// Admins may override the user_id filter to inspect others' jobs.
	isAdmin := false
	if u, err := a.UserStore.GetByID(userID); err == nil && u != nil && u.Role == auth.RoleAdmin {
		isAdmin = true
	}
	targetUserID := userID
	if isAdmin {
		if v := strings.TrimSpace(q.Get("user_id")); v != "" {
			targetUserID = v
		}
	}

	// Backwards-compatible "no filter" path: return the user's recent
	// jobs without pagination.
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
		writeJSONErrorKey(w, r, "transfers.invalidState", http.StatusBadRequest)
		return
	}
	dir, err := parseFileTransferDirection(q.Get("direction"))
	if err != nil {
		writeJSONErrorKey(w, r, "transfers.invalidDirection", http.StatusBadRequest)
		return
	}
	backend, err := parseFileTransferBackend(q.Get("backend"))
	if err != nil {
		writeJSONErrorKey(w, r, "transfers.invalidBackend", http.StatusBadRequest)
		return
	}
	afterUpdated, afterID, err := decodeFileTransferCursor(q.Get("after_cursor"))
	if err != nil {
		writeJSONErrorKey(w, r, "transfers.invalidCursor", http.StatusBadRequest)
		return
	}
	from, to, err := parseTimeRange(q.Get("from"), q.Get("to"), time.Now().UTC())
	if err != nil {
		writeTimeRangeError(w, r, err)
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
		if t, perr := time.Parse(time.RFC3339, last.UpdatedAt); perr == nil {
			nextCursor = encodeFileTransferCursor(t, last.ID)
		}
		items = items[:pageLimit]
	}
	writeJSON(w, fileTransfersListResponse{Items: items, NextCursor: nextCursor})
}

// handleFileTransferGet returns a single job's snapshot. Only the
// owner may read it.
func (a *App) handleFileTransferGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "transfer_id")
	j, ok := a.FileTransferManager.Get(id)
	if !ok || j.UserID != userID {
		writeJSONErrorKey(w, r, "common.notFound", http.StatusNotFound)
		return
	}
	writeJSON(w, j.Snapshot())
}

// handleFileTransferDelete cancels a job and cleans up its staging
// file. Only the owner may delete it.
func (a *App) handleFileTransferDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "transfer_id")
	if err := a.FileTransferManager.Cancel(id, userID); err != nil {
		if errors.Is(err, filetransfer.ErrNotFound) {
			writeJSONErrorKey(w, r, "common.notFound", http.StatusNotFound)
			return
		}
		if errors.Is(err, filetransfer.ErrForbidden) {
			writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
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

// handleFileTransferContent serves the completed download staging
// file. GET /api/file-transfers/{transfer_id}/content.
func (a *App) handleFileTransferContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "transfer_id")
	j, ok := a.FileTransferManager.Get(id)
	if !ok || j.UserID != userID {
		writeJSONErrorKey(w, r, "common.notFound", http.StatusNotFound)
		return
	}
	snap := j.Snapshot()
	if snap.Direction != string(filetransfer.DirectionDownload) {
		writeJSONErrorKey(w, r, "transfers.notADownload", http.StatusBadRequest)
		return
	}
	if snap.State != string(filetransfer.StateCompleted) {
		writeJSONErrorKey(w, r, "transfers.notReady", http.StatusConflict)
		return
	}
	tempPath := j.GetTempPath()
	fileName := j.GetFileName()
	if tempPath == "" {
		writeJSONErrorKey(w, r, "transfers.notReady", http.StatusConflict)
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
	setAttachmentDisposition(w, fileName)
	w.Header().Set("Content-Type", "application/octet-stream")
	if info.Size() > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// fileTransferDownloadRequest is the JSON body for
// POST /api/file-transfers/download.
type fileTransferDownloadRequest struct {
	Backend  string `json:"backend"`
	TargetID string `json:"target_id"`
	Path     string `json:"path"`
}

// handleFileTransferStartDownload starts a background download.
// POST /api/file-transfers/download.
func (a *App) handleFileTransferStartDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	var req fileTransferDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidJSON", http.StatusBadRequest)
		return
	}
	backend := filetransfer.Backend(strings.TrimSpace(req.Backend))
	if backend != filetransfer.BackendRemote && backend != filetransfer.BackendTFTPServer {
		writeJSONErrorKey(w, r, "transfers.backendInvalid", http.StatusBadRequest)
		return
	}
	targetID := strings.TrimSpace(req.TargetID)
	pathParam := strings.TrimSpace(req.Path)
	if targetID == "" || pathParam == "" {
		writeJSONErrorKey(w, r, "transfers.targetAndPathReq", http.StatusBadRequest)
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

// handleFileTransferUpload receives a file into staging then completes
// the transfer in the background.
// POST /api/file-transfers/upload (multipart: backend, target_id, path, file).
func (a *App) handleFileTransferUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	const memoryThresholdMB = 8 // overflow spills to disk (CWE-770).
	r.Body = http.MaxBytesReader(w, r.Body, maxFileTransferMB<<20)
	if err := r.ParseMultipartForm(memoryThresholdMB << 20); err != nil { // #nosec G120
		writeJSONErrorKey(w, r, "files.invalidMultipart", http.StatusBadRequest, "error", err)
		return
	}
	backend := filetransfer.Backend(strings.TrimSpace(r.FormValue("backend")))
	if backend != filetransfer.BackendRemote && backend != filetransfer.BackendTFTPServer {
		writeJSONErrorKey(w, r, "transfers.backendInvalid", http.StatusBadRequest)
		return
	}
	targetID := strings.TrimSpace(r.FormValue("target_id"))
	pathParam := strings.TrimSpace(r.FormValue("path"))
	if targetID == "" || pathParam == "" {
		writeJSONErrorKey(w, r, "transfers.targetAndPathReq", http.StatusBadRequest)
		return
	}
	if !path.IsAbs(pathParam) {
		pathParam = "/" + pathParam
	}
	pathParam = path.Clean(pathParam)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSONErrorKey(w, r, "files.fileRequired", http.StatusBadRequest, "error", err)
		return
	}
	defer file.Close()
	_, target, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}
	if backend == filetransfer.BackendRemote && !protocols.SupportsFileTransfer(target.Protocol) {
		writeJSONErrorKey(w, r, "transfers.notSupportedForTarget", http.StatusBadRequest)
		return
	}
	if backend == filetransfer.BackendTFTPServer && !protocols.Supports(target.Protocol, protocols.CapabilityTFTPServer) {
		writeJSONErrorKey(w, r, "tftp.notTFTPServer", http.StatusBadRequest)
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
		writeInternalError(w, err)
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
