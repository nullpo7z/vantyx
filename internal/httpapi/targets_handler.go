package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/tftp"
)

type targetResponse struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Host                  string   `json:"host"`
	Port                  uint16   `json:"port"`
	Protocol              string   `json:"protocol"`
	Path                  string   `json:"path"`
	SSHUsername           string   `json:"ssh_username,omitempty"`
	HasStoredCredentials  bool     `json:"has_stored_credentials,omitempty"`
	HasSSHKey             bool     `json:"has_ssh_key,omitempty"`
	NeedsPassword         bool     `json:"needs_password,omitempty"`   // username stored but no password; SPA prompts at connect.
	NeedsPassphrase       bool     `json:"needs_passphrase,omitempty"` // encrypted private key stored but no passphrase; SPA prompts at connect.
	Tags                  []string `json:"tags,omitempty"`
	SFTPEnabled           bool     `json:"sftp_enabled"`
	FTPEnabled            bool     `json:"ftp_enabled"`
	TFTPEnabled           bool     `json:"tftp_enabled"`
	SSHHostKeyFingerprint string   `json:"ssh_host_key_fingerprint,omitempty"`
}

type createTargetRequest struct {
	Name                    string `json:"name"`
	Host                    string `json:"host"`
	Port                    uint16 `json:"port"`
	Protocol                string `json:"protocol"`
	Path                    string `json:"path"`
	GroupID                 string `json:"group_id"`
	SSHUsername             string `json:"ssh_username"`
	SSHPassword             string `json:"ssh_password"`
	SSHPrivateKey           string `json:"ssh_private_key"`
	SSHPrivateKeyPassphrase string `json:"ssh_private_key_passphrase"`
	SFTPEnabled             *bool  `json:"sftp_enabled,omitempty"`
	FTPEnabled              *bool  `json:"ftp_enabled,omitempty"`
	TFTPEnabled             *bool  `json:"tftp_enabled,omitempty"`
	// SSHHostKeyFingerprint, when non-empty, is recorded as the
	// expected SHA-256 fingerprint of the upstream SSH host key.
	// Format: "SHA256:<base64-without-padding>" (matching
	// `ssh-keygen -lf`). Empty means "no fingerprint yet — adopt
	// later via TOFU".
	SSHHostKeyFingerprint string `json:"ssh_host_key_fingerprint,omitempty"`
}

type updateTargetRequest struct {
	Name                    string  `json:"name"`
	Host                    string  `json:"host"`
	Port                    uint16  `json:"port"`
	Protocol                string  `json:"protocol"`
	Path                    string  `json:"path"`
	SSHUsername             string  `json:"ssh_username"`
	SSHPassword             *string `json:"ssh_password,omitempty"`               // nil = leave unchanged, empty string = clear.
	SSHPrivateKey           *string `json:"ssh_private_key,omitempty"`            // nil = leave unchanged, empty string = clear.
	SSHPrivateKeyPassphrase *string `json:"ssh_private_key_passphrase,omitempty"` // nil = leave unchanged, empty string = clear.
	SFTPEnabled             *bool   `json:"sftp_enabled,omitempty"`
	FTPEnabled              *bool   `json:"ftp_enabled,omitempty"`
	TFTPEnabled             *bool   `json:"tftp_enabled,omitempty"`
}

// isEncryptedPEMBlock reports whether the PEM block is encrypted
// (classic ENCRYPTED header or OpenSSH bcrypt KDF).
func isEncryptedPEMBlock(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "ENCRYPTED") {
		return true
	}
	if strings.Contains(s, "bcrypt") {
		return true
	}
	return false
}

// targetToResponse builds the JSON view of a target including the
// connection hints (NeedsPassword / NeedsPassphrase) used by the SPA.
func targetToResponse(t *access.Target, tags []string) targetResponse {
	r := targetResponse{
		ID:                    string(t.ID),
		Name:                  t.Name,
		Host:                  t.Host,
		Port:                  t.Port,
		Protocol:              string(t.Protocol),
		Path:                  t.Path,
		SSHUsername:           t.SSHUsername,
		Tags:                  tags,
		SFTPEnabled:           t.SFTPEnabled,
		FTPEnabled:            t.FTPEnabled,
		TFTPEnabled:           t.TFTPEnabled,
		SSHHostKeyFingerprint: t.SSHHostKeyFingerprint,
	}
	if tags == nil {
		r.Tags = []string{}
	}
	if t.SSHUsername != "" {
		r.HasStoredCredentials = true
	}
	if t.SSHPrivateKey != "" {
		r.HasSSHKey = true
	}
	if t.SSHUsername != "" && t.SSHPassword == "" && t.SSHPrivateKey == "" {
		r.NeedsPassword = true
	}
	if t.SSHPrivateKey != "" && t.SSHPrivateKeyPassphrase == "" && isEncryptedPEMBlock(t.SSHPrivateKey) {
		r.NeedsPassphrase = true
	}
	return r
}

// listOptsFromRequest parses limit and after_id from the query string.
// Returns nil when neither is set so callers can detect "no pagination
// requested" and stream the full list.
func listOptsFromRequest(r *http.Request) *access.ListOpts {
	q := r.URL.Query()
	afterID := strings.TrimSpace(q.Get("after_id"))
	limitStr := strings.TrimSpace(q.Get("limit"))
	if afterID == "" && limitStr == "" {
		return nil
	}
	opts := &access.ListOpts{AfterID: afterID}
	if limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n <= 0 {
			return opts
		}
		if n > 1000 {
			n = 1000
		}
		opts.Limit = n
	} else {
		opts.Limit = 100
	}
	return opts
}

// handleTargets returns the list of targets the current user can access.
func (a *App) handleTargets(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	opts := listOptsFromRequest(r)
	paginate := opts != nil
	pageLimit := 0
	if paginate {
		pageLimit = opts.Limit
		opts = &access.ListOpts{Limit: pageLimit + 1, AfterID: opts.AfterID}
	}
	ids, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), opts)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var nextCursor string
	if paginate && pageLimit > 0 && len(ids) > pageLimit {
		ids = ids[:pageLimit]
		nextCursor = string(ids[pageLimit-1])
	}
	targets, err := a.TargetStore.ListByIDs(ctx, ids, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]targetResponse, 0, len(targets))
	for _, t := range targets {
		tags, _ := a.TargetStore.TagsForTarget(ctx, t.ID)
		out = append(out, targetToResponse(t, tags))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if paginate {
		_ = json.NewEncoder(w).Encode(struct {
			Items      []targetResponse `json:"items"`
			NextCursor string           `json:"next_cursor,omitempty"`
		}{Items: out, NextCursor: nextCursor})
	} else {
		_ = json.NewEncoder(w).Encode(out)
	}
}

// handleCreateTarget creates a new target and adds it to the specified
// access group. Admin-only (server-management operation).
func (a *App) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.requireAdmin(w, r) {
		return
	}
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	_ = sess // reserved for future per-user permission checks.

	var req createTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.Path = normalizeTargetPath(req.Path)
	req.GroupID = strings.TrimSpace(req.GroupID)
	if req.Name == "" || req.Host == "" {
		writeJSONErrorKey(w, r, "targets.nameHostRequired", http.StatusBadRequest)
		return
	}
	if req.GroupID == "" {
		writeJSONErrorKey(w, r, "targets.groupIDRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, err := a.AccessGroupStore.Get(ctx, access.GroupID(req.GroupID)); err != nil {
		writeJSONErrorKey(w, r, "targets.groupNotFound", http.StatusNotFound)
		return
	}
	allowedGroups, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowed := false
	for _, gid := range allowedGroups {
		if gid == access.GroupID(req.GroupID) {
			allowed = true
			break
		}
	}
	if !allowed {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	protocol, err := parseProtocolField(req.Protocol)
	if err != nil {
		if errors.Is(err, ErrInvalidProtocol) {
			writeJSONErrorKey(w, r, "targets.protocolInvalid", http.StatusBadRequest)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	sftpEnabled := protocol == access.ProtocolSSH
	ftpEnabled := false
	tftpEnabled := false
	if req.SFTPEnabled != nil {
		sftpEnabled = *req.SFTPEnabled
	}
	if req.FTPEnabled != nil {
		ftpEnabled = *req.FTPEnabled
	}
	if req.TFTPEnabled != nil {
		tftpEnabled = *req.TFTPEnabled
	}
	baseID := slugID(req.Name)
	id := baseID
	for i := 0; ; i++ {
		if i > 0 {
			id = baseID + "-" + strconv.Itoa(i)
		}
		path := req.Path
		if path == "" {
			path = req.GroupID
		}
		_, err := a.TargetStore.CreateWithPath(ctx, access.TargetID(id), req.Name, req.Host, req.Port, protocol, access.GroupID(req.GroupID), path, strings.TrimSpace(req.SSHUsername), req.SSHPassword, req.SSHPrivateKey, req.SSHPrivateKeyPassphrase, sftpEnabled, ftpEnabled, tftpEnabled)
		if err == nil {
			break
		}
		if errors.Is(err, access.ErrEncryptionKeyRequired) {
			writeServiceUnavailableError(w, err)
			return
		}
		if errors.Is(err, access.ErrTargetExists) {
			continue
		}
		if writeAccessValidationError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := a.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID(req.GroupID), access.TargetID(id)); err != nil {
		writeJSONErrorKey(w, r, "targets.assignFailed", http.StatusInternalServerError)
		return
	}
	if fp := strings.TrimSpace(req.SSHHostKeyFingerprint); fp != "" {
		if err := a.TargetStore.SetSSHHostKeyFingerprint(ctx, access.TargetID(id), fp); err != nil {
			if writeAccessValidationError(w, r, err) {
				return
			}
			writeInternalError(w, err)
			return
		}
		audit("target_host_key_adopted", auditFields{
			"user_id":     sess.UserID,
			"target_id":   id,
			"fingerprint": fp,
			"source":      "create",
		})
	}
	tftp.NotifyTargetCreated(ctx, a.TargetStore, protocol)

	t, _ := a.TargetStore.Get(ctx, access.TargetID(id))
	tags, _ := a.TargetStore.TagsForTarget(ctx, access.TargetID(id))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(targetToResponse(t, tags))
}

// handleUpdateTarget updates an existing target. Admin-only.
func (a *App) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.requireAdmin(w, r) {
		return
	}
	targetID := chi.URLParam(r, "target_id")
	_, cur, ok := a.getSessionAndTargetWithAccess(w, r, targetID)
	if !ok {
		return
	}
	ctx := r.Context()
	var req updateTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.Path = normalizeTargetPath(req.Path)
	if req.Name == "" || req.Host == "" {
		writeJSONErrorKey(w, r, "targets.nameHostRequired", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	protocol, err := parseProtocolField(req.Protocol)
	if err != nil {
		if errors.Is(err, ErrInvalidProtocol) {
			writeJSONErrorKey(w, r, "targets.protocolInvalid", http.StatusBadRequest)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	var sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string
	if cur != nil {
		sshPassword = cur.SSHPassword
		sshPrivateKey = cur.SSHPrivateKey
		sshPrivateKeyPassphrase = cur.SSHPrivateKeyPassphrase
	}
	if req.SSHPassword != nil {
		sshPassword = *req.SSHPassword
	}
	if req.SSHPrivateKey != nil {
		sshPrivateKey = *req.SSHPrivateKey
	}
	if req.SSHPrivateKeyPassphrase != nil {
		sshPrivateKeyPassphrase = *req.SSHPrivateKeyPassphrase
	}
	sftpEnabled := cur != nil && cur.SFTPEnabled
	ftpEnabled := cur != nil && cur.FTPEnabled
	tftpEnabled := cur != nil && cur.TFTPEnabled
	if req.SFTPEnabled != nil {
		sftpEnabled = *req.SFTPEnabled
	}
	if req.FTPEnabled != nil {
		ftpEnabled = *req.FTPEnabled
	}
	if req.TFTPEnabled != nil {
		tftpEnabled = *req.TFTPEnabled
	}
	t, err := a.TargetStore.Update(ctx, access.TargetID(targetID), req.Name, req.Host, req.Port, protocol, req.Path, strings.TrimSpace(req.SSHUsername), sshPassword, sshPrivateKey, sshPrivateKeyPassphrase, sftpEnabled, ftpEnabled, tftpEnabled)
	if err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONErrorKey(w, r, "common.targetNotFound", http.StatusNotFound)
			return
		}
		if errors.Is(err, access.ErrEncryptionKeyRequired) {
			writeServiceUnavailableError(w, err)
			return
		}
		if writeAccessValidationError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	tags, _ := a.TargetStore.TagsForTarget(ctx, t.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(targetToResponse(t, tags))
}

// handleDeleteTarget deletes a target. Admin-only.
func (a *App) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSONErrorKey(w, r, "common.methodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.requireAdmin(w, r) {
		return
	}
	targetID := chi.URLParam(r, "target_id")
	if targetID == "" {
		writeJSONErrorKey(w, r, "common.targetIDRequired", http.StatusBadRequest)
		return
	}
	c, err := r.Cookie("vantyx_session")
	if err != nil || c.Value == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	sess, err := a.SessionStore.Get(c.Value)
	if err != nil {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	allowedIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(sess.UserID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowed := false
	for _, id := range allowedIDs {
		if id == access.TargetID(targetID) {
			allowed = true
			break
		}
	}
	if !allowed {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	cur, _ := a.TargetStore.Get(ctx, access.TargetID(targetID))
	if err := a.TargetStore.Delete(ctx, access.TargetID(targetID)); err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONErrorKey(w, r, "common.targetNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if cur != nil {
		tftp.NotifyTargetDeleted(cur.Protocol)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTargetTags returns tags for the target. The caller must have
// access to the target.
func (a *App) handleTargetTags(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "target_id")
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		writeJSONErrorKey(w, r, "common.targetIDRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, ok := a.requireTargetAccess(w, r, access.TargetID(targetID)); !ok {
		return
	}
	tags, err := a.TargetStore.TagsForTarget(ctx, access.TargetID(targetID))
	if err != nil {
		if errors.Is(err, access.ErrTargetNotFound) {
			writeJSONErrorKey(w, r, "common.targetNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse{Tags: tags})
}

// handleSetTargetTags sets tags for the target. Admin-only.
func (a *App) handleSetTargetTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	targetID := chi.URLParam(r, "target_id")
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		writeJSONErrorKey(w, r, "common.targetIDRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, ok := a.requireTargetAccess(w, r, access.TargetID(targetID)); !ok {
		return
	}
	var req setTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	if err := a.TargetStore.SetTargetTags(ctx, access.TargetID(targetID), req.Tags); err != nil {
		switch {
		case errors.Is(err, access.ErrTargetNotFound):
			writeJSONErrorKey(w, r, "common.targetNotFound", http.StatusNotFound)
		case errors.Is(err, access.ErrTagLength):
			writeJSONErrorKey(w, r, "tags.lengthInvalid", http.StatusBadRequest)
		case errors.Is(err, access.ErrTagChars):
			writeJSONErrorKey(w, r, "tags.charsInvalid", http.StatusBadRequest)
		default:
			writeInternalError(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse(req))
}
