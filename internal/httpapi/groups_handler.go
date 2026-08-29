package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/auth"
)

// groupIDParam extracts the {group_id} route parameter. Access group
// IDs are hierarchical paths (e.g. "parent/child"); the SPA percent-
// encodes the "/" (encodeURIComponent) so the whole ID lands in a
// single path segment, and chi returns route params exactly as
// matched -- still percent-encoded. Decode it back here, mirroring
// the same pattern used for recording IDs, or a nested group's ID
// would fail validateGroupID with a spurious "invalid characters"
// error (the literal "%2F" doesn't match the allowed character set).
func groupIDParam(r *http.Request) string {
	raw := chi.URLParam(r, "group_id")
	if decoded, err := url.PathUnescape(raw); err == nil {
		raw = decoded
	}
	return strings.TrimSpace(raw)
}

type groupResponse struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Targets []targetResponse `json:"targets"`
}

type createGroupRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type updateGroupRequest struct {
	Name string `json:"name"`
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

// defaultGroupTag derives a tag from the group identifier so it always matches
// the backend tag validation (alnum, hyphen, underscore; 1–64 chars).
func defaultGroupTag(groupID string) string {
	s := strings.TrimSpace(groupID)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '/' || r == ' ' || r == '\t' || r == '.':
			b.WriteByte('_')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
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
	// Admins see every group and every target in it: the management UI
	// (Server management) is driven by this endpoint, and a second admin
	// who isn't a member of a group could otherwise neither see nor edit
	// it. Ordinary users stay on the membership/tag-based path.
	isAdmin, err := a.currentUserIsAdmin(r)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var groupIDs []access.GroupID
	if isAdmin {
		groupIDs, err = a.AccessGroupStore.AllGroupIDs(ctx, opts)
	} else {
		groupIDs, err = a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(userID), opts)
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var nextCursor string
	if paginate && pageLimit > 0 && len(groupIDs) > pageLimit {
		groupIDs = groupIDs[:pageLimit]
		nextCursor = string(groupIDs[pageLimit-1])
	}
	// Targets this user is allowed to see (membership or tag based);
	// admins are allowed to see all of them.
	allowedSet := make(map[access.TargetID]bool)
	if !isAdmin {
		allowedTargetIDs, err := a.AccessGroupStore.TargetIDsForUser(ctx, access.UserID(userID), nil)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for _, id := range allowedTargetIDs {
			allowedSet[id] = true
		}
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
		// leaking targets when access is tag based. Admins see all.
		var filtered []access.TargetID
		for _, tid := range tids {
			if isAdmin || allowedSet[tid] {
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
		// Use the JSON-shaped helper so the API surface stays
		// consistent (M-23 / CWE-1077). http.Error would have sent
		// plain text instead.
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
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
	// Create a default group tag on creation so targets can inherit it.
	if tag := defaultGroupTag(id); tag != "" {
		_ = a.AccessGroupStore.SetGroupTags(ctx, access.GroupID(id), []string{tag})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(groupResponse{ID: id, Name: req.Name, Targets: []targetResponse{}})
}

// handleUpdateGroup updates an access group's name. Admin-only.
func (a *App) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := groupIDParam(r)
	if groupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	var req updateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSONErrorKey(w, r, "groups.nameRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	updated, err := a.AccessGroupStore.Update(ctx, access.GroupID(groupID), req.Name)
	if err != nil {
		if writeAccessValidationError(w, r, err) {
			return
		}
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONErrorKey(w, r, "common.notFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(groupResponse{ID: string(updated.ID), Name: updated.Name, Targets: []targetResponse{}})
}

// handleDeleteGroup deletes an access group. Admin-only.
func (a *App) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := groupIDParam(r)
	if groupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	err := a.AccessGroupStore.Delete(ctx, access.GroupID(groupID))
	if err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONErrorKey(w, r, "common.notFound", http.StatusNotFound)
			return
		}
		if writeAccessValidationError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGroupMembers returns users belonging to the group. Admin only.
func (a *App) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := groupIDParam(r)
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
	groupID := groupIDParam(r)
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
	groupID := groupIDParam(r)
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
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
	groupID := groupIDParam(r)
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

// handleSetGroupTags sets tags for the group. Admin only (CWE-269):
// because group tags drive cross-user ACL via user_tags ⇄ group_tags,
// allowing non-admin group members to mutate them would let any
// member silently grant other users access to every target in the
// group. Members read their group tags via GET, which keeps the
// non-admin self-service flow intact.
func (a *App) handleSetGroupTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	groupID := groupIDParam(r)
	if groupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
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
