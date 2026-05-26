package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
)

type groupResponse struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Targets []targetResponse `json:"targets"`
}

type createGroupRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type addGroupMemberRequest struct {
	UserID string `json:"user_id"`
}

type tagsResponse struct {
	Tags []string `json:"tags"`
}

type setTagsRequest struct {
	Tags []string `json:"tags"`
}

// handleGroups returns access groups the current user belongs to,
// including the targets they may see (membership or tag based).
func (a *App) handleGroups(w http.ResponseWriter, r *http.Request) {
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
	groupIDs, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(userID), opts)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var nextCursor string
	if paginate && pageLimit > 0 && len(groupIDs) > pageLimit {
		groupIDs = groupIDs[:pageLimit]
		nextCursor = string(groupIDs[pageLimit-1])
	}
	// Targets this user is allowed to see (membership or tag based).
	allowedTargetIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	allowedSet := make(map[access.TargetID]bool)
	for _, id := range allowedTargetIDs {
		allowedSet[id] = true
	}

	out := make([]groupResponse, 0, len(groupIDs))
	for _, gid := range groupIDs {
		g, err := a.AccessGroupStore.Get(ctx, gid)
		if err != nil {
			continue
		}
		tids, err := a.AccessGroupStore.TargetIDsForGroup(ctx, gid, nil)
		if err != nil {
			continue
		}
		// Only include targets the user is allowed to see; avoids
		// leaking targets when access is tag based.
		var filtered []access.TargetID
		for _, tid := range tids {
			if allowedSet[tid] {
				filtered = append(filtered, tid)
			}
		}
		targets, err := a.TargetStore.ListByIDs(ctx, filtered, nil)
		if err != nil {
			continue
		}
		tout := make([]targetResponse, 0, len(targets))
		for _, t := range targets {
			tags, _ := a.TargetStore.TagsForTarget(ctx, t.ID)
			tout = append(tout, targetToResponse(t, tags))
		}
		out = append(out, groupResponse{ID: string(g.ID), Name: g.Name, Targets: tout})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if paginate {
		_ = json.NewEncoder(w).Encode(struct {
			Items      []groupResponse `json:"items"`
			NextCursor string          `json:"next_cursor,omitempty"`
		}{Items: out, NextCursor: nextCursor})
	} else {
		_ = json.NewEncoder(w).Encode(out)
	}
}

// handleCreateGroup creates a new access group and adds the current
// admin to it. Admin-only (server-management operation).
func (a *App) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Path = normalizeTargetPath(req.Path)
	if req.Name == "" {
		writeJSONErrorKey(w, r, "groups.nameRequired", http.StatusBadRequest)
		return
	}

	base := slugID(req.Name)
	if req.Path != "" {
		base = req.Path + "/" + base
	}
	ctx := r.Context()
	id := base
	for i := 0; ; i++ {
		if i > 0 {
			id = base + "-" + strconv.Itoa(i)
		}
		_, err := a.AccessGroupStore.Create(ctx, access.GroupID(id), req.Name)
		if err == nil {
			break
		}
		if errors.Is(err, access.ErrGroupExists) {
			continue
		}
		if writeAccessValidationError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := a.AccessGroupStore.AddUserToGroup(ctx, access.UserID(userID), access.GroupID(id)); err != nil {
		writeInternalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(groupResponse{ID: id, Name: req.Name, Targets: []targetResponse{}})
}

// handleGroupMembers returns users belonging to the group. Admin only.
func (a *App) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := chi.URLParam(r, "group_id")
	if groupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	opts := listOptsFromRequest(r)
	userIDs, err := a.AccessGroupStore.UserIDsForGroup(ctx, access.GroupID(groupID), opts)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]userResponse, 0, len(userIDs))
	for _, uid := range userIDs {
		u, err := a.UserStore.GetByID(string(uid))
		if err != nil {
			continue
		}
		role := u.Role
		if role == "" {
			role = auth.RoleUser
		}
		out = append(out, userResponse{ID: u.ID, Username: u.Username, Role: role})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// handleAddGroupMember adds a user to an access group. Admin only.
func (a *App) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := chi.URLParam(r, "group_id")
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	var req addGroupMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeJSONErrorKey(w, r, "groups.userIDRequired", http.StatusBadRequest)
		return
	}
	if _, err := a.UserStore.GetByID(req.UserID); err != nil {
		writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if _, err := a.AccessGroupStore.Get(ctx, access.GroupID(groupID)); err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONErrorKey(w, r, "groups.notFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := a.AccessGroupStore.AddUserToGroup(ctx, access.UserID(req.UserID), access.GroupID(groupID)); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveGroupMember removes a user from an access group. Admin
// only.
func (a *App) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := chi.URLParam(r, "group_id")
	userID := chi.URLParam(r, "user_id")
	groupID = strings.TrimSpace(groupID)
	userID = strings.TrimSpace(userID)
	if groupID == "" || userID == "" {
		writeJSONErrorKey(w, r, "groups.groupAndUserIDRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if err := a.AccessGroupStore.RemoveUserFromGroup(ctx, access.UserID(userID), access.GroupID(groupID)); err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONErrorKey(w, r, "groups.notFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGroupTags returns tags for the group. The caller must be a
// member or an admin.
func (a *App) handleGroupTags(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "group_id")
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, ok := a.requireGroupMemberOrAdmin(w, r, access.GroupID(groupID)); !ok {
		return
	}
	tags, err := a.AccessGroupStore.TagsForGroup(ctx, access.GroupID(groupID))
	if err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONErrorKey(w, r, "groups.notFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse{Tags: tags})
}

// handleSetGroupTags sets tags for the group. The caller must be a
// member or an admin.
func (a *App) handleSetGroupTags(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "group_id")
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, ok := a.requireGroupMemberOrAdmin(w, r, access.GroupID(groupID)); !ok {
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
	if err := a.AccessGroupStore.SetGroupTags(ctx, access.GroupID(groupID), req.Tags); err != nil {
		switch {
		case errors.Is(err, access.ErrGroupNotFound):
			writeJSONErrorKey(w, r, "groups.notFound", http.StatusNotFound)
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
