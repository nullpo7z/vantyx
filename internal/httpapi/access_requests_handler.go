package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
)

// Access requests let a user ask for (time-limited) membership of a
// group; approving creates the membership through the same path the
// admin UI uses, so expiry and hierarchy semantics are identical.

const (
	accessRequestMaxReason   = 500
	accessRequestMaxDuration = 365 * 24 * 3600 // seconds
)

type accessRequestResponse struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	Username        string `json:"username,omitempty"`
	GroupID         string `json:"group_id"`
	GroupName       string `json:"group_name,omitempty"`
	Reason          string `json:"reason"`
	DurationSeconds int64  `json:"duration_seconds"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	DecidedAt       string `json:"decided_at,omitempty"`
	DecidedBy       string `json:"decided_by,omitempty"`
	DecisionNote    string `json:"decision_note,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
}

func (a *App) accessRequestToResponse(r *http.Request, ar *access.AccessRequest) accessRequestResponse {
	loc := a.serverLocation()
	out := accessRequestResponse{
		ID:              ar.ID,
		UserID:          string(ar.UserID),
		GroupID:         string(ar.GroupID),
		Reason:          ar.Reason,
		DurationSeconds: ar.DurationSeconds,
		Status:          string(ar.Status),
		CreatedAt:       ar.CreatedAt.In(loc).Format(time.RFC3339),
		DecidedBy:       ar.DecidedBy,
		DecisionNote:    ar.DecisionNote,
	}
	if ar.DecidedAt != nil {
		out.DecidedAt = ar.DecidedAt.In(loc).Format(time.RFC3339)
	}
	if ar.ExpiresAt != nil {
		out.ExpiresAt = ar.ExpiresAt.In(loc).Format(time.RFC3339)
	}
	out.Username = a.usernameFor(r.Context(), string(ar.UserID))
	if g, err := a.AccessGroupStore.Get(r.Context(), ar.GroupID); err == nil && g != nil {
		out.GroupName = g.Name
	}
	return out
}

// handleAccessRequestGroups: GET /api/access-requests/groups lists every
// group (id, name) with whether the caller already reaches it, so the
// request form can offer the rest. Group names are not considered
// sensitive; target details stay hidden.
func (a *App) handleAccessRequestGroups(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	all, err := a.AccessGroupStore.AllGroupIDs(ctx, &access.ListOpts{Limit: 1000})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	mine, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(userID), &access.ListOpts{Limit: 1000})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	has := map[access.GroupID]bool{}
	for _, g := range mine {
		has[g] = true
	}
	type item struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		HasAccess bool   `json:"has_access"`
	}
	out := make([]item, 0, len(all))
	for _, gid := range all {
		name := string(gid)
		if g, err := a.AccessGroupStore.Get(ctx, gid); err == nil && g != nil && g.Name != "" {
			name = g.Name
		}
		out = append(out, item{ID: string(gid), Name: name, HasAccess: has[gid]})
	}
	writeJSON(w, out)
}

type createAccessRequestRequest struct {
	GroupID         string `json:"group_id"`
	Reason          string `json:"reason"`
	DurationSeconds int64  `json:"duration_seconds"`
}

// handleCreateAccessRequest: POST /api/access-requests.
func (a *App) handleCreateAccessRequest(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	var req createAccessRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.GroupID == "" {
		writeJSONErrorKey(w, r, "groups.idRequired", http.StatusBadRequest)
		return
	}
	if len(req.Reason) > accessRequestMaxReason {
		writeJSONErrorKey(w, r, "accessRequests.reasonTooLong", http.StatusBadRequest)
		return
	}
	if req.DurationSeconds < 0 || req.DurationSeconds > accessRequestMaxDuration {
		writeJSONErrorKey(w, r, "accessRequests.durationInvalid", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if _, err := a.AccessGroupStore.Get(ctx, access.GroupID(req.GroupID)); err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONErrorKey(w, r, "groups.notFound", http.StatusNotFound)
			return
		}
		if writeAccessValidationError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	mine, err := a.AccessGroupStore.GroupIDsForUser(ctx, access.UserID(userID), &access.ListOpts{Limit: 1000})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	for _, g := range mine {
		if string(g) == req.GroupID {
			writeJSONErrorKey(w, r, "accessRequests.alreadyHasAccess", http.StatusConflict)
			return
		}
	}
	if pending, err := a.AccessRequests.HasPending(ctx, access.UserID(userID), access.GroupID(req.GroupID)); err != nil {
		writeInternalError(w, err)
		return
	} else if pending {
		writeJSONErrorKey(w, r, "accessRequests.pendingExists", http.StatusConflict)
		return
	}
	id, err := randomToken()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	ar := &access.AccessRequest{
		ID:              id[:22],
		UserID:          access.UserID(userID),
		GroupID:         access.GroupID(req.GroupID),
		Reason:          req.Reason,
		DurationSeconds: req.DurationSeconds,
		Status:          access.RequestPending,
		CreatedAt:       time.Now().UTC(),
	}
	if err := a.AccessRequests.Create(ctx, ar); err != nil {
		writeInternalError(w, err)
		return
	}
	audit("access_request_created", auditFields{
		"user_id":          userID,
		"request_id":       ar.ID,
		"group_id":         req.GroupID,
		"duration_seconds": req.DurationSeconds,
	})
	writeJSONStatus(w, http.StatusCreated, a.accessRequestToResponse(r, ar))
}

func writeJSONStatus(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleListAccessRequests: GET /api/access-requests[?status=][&mine=1].
// Non-admins only ever see their own requests.
func (a *App) handleListAccessRequests(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	isAdmin, err := a.currentUserIsAdmin(r)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	f := access.AccessRequestFilter{Status: access.AccessRequestStatus(strings.TrimSpace(r.URL.Query().Get("status")))}
	if !isAdmin || r.URL.Query().Get("mine") == "1" {
		f.UserID = access.UserID(userID)
	}
	list, err := a.AccessRequests.List(r.Context(), f)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]accessRequestResponse, 0, len(list))
	for i := range list {
		out = append(out, a.accessRequestToResponse(r, &list[i]))
	}
	writeJSON(w, out)
}

type decideAccessRequestRequest struct {
	// DurationSeconds overrides the requested duration on approval
	// (nil = use the request's; 0 = permanent).
	DurationSeconds *int64 `json:"duration_seconds"`
	Note            string `json:"note"`
}

func (a *App) loadPendingRequest(w http.ResponseWriter, r *http.Request) (*access.AccessRequest, bool) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSONErrorKey(w, r, "accessRequests.notFound", http.StatusNotFound)
		return nil, false
	}
	ar, err := a.AccessRequests.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, access.ErrRequestNotFound) {
			writeJSONErrorKey(w, r, "accessRequests.notFound", http.StatusNotFound)
			return nil, false
		}
		writeInternalError(w, err)
		return nil, false
	}
	if ar.Status != access.RequestPending {
		writeJSONErrorKey(w, r, "accessRequests.notPending", http.StatusConflict)
		return nil, false
	}
	return ar, true
}

// handleApproveAccessRequest: POST /api/access-requests/{id}/approve (admin).
func (a *App) handleApproveAccessRequest(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	ar, ok := a.loadPendingRequest(w, r)
	if !ok {
		return
	}
	var req decideAccessRequestRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
	}
	duration := ar.DurationSeconds
	if req.DurationSeconds != nil {
		duration = *req.DurationSeconds
	}
	if duration < 0 || duration > accessRequestMaxDuration {
		writeJSONErrorKey(w, r, "accessRequests.durationInvalid", http.StatusBadRequest)
		return
	}
	var expiresAt *time.Time
	if duration > 0 {
		t := time.Now().Add(time.Duration(duration) * time.Second).UTC()
		expiresAt = &t
	}
	ctx := r.Context()
	adminID := a.currentUserID(r)
	if err := a.AccessGroupStore.AddUserToGroupUntil(ctx, ar.UserID, ar.GroupID, expiresAt); err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeJSONErrorKey(w, r, "groups.notFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := a.AccessRequests.Decide(ctx, ar.ID, access.RequestApproved, adminID, strings.TrimSpace(req.Note), expiresAt); err != nil {
		// Two admins deciding at once: the membership is idempotent, the
		// second decision simply loses.
		if errors.Is(err, access.ErrRequestNotPending) {
			writeJSONErrorKey(w, r, "accessRequests.notPending", http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}
	fields := auditFields{
		"user_id":    adminID,
		"request_id": ar.ID,
		"member_id":  string(ar.UserID),
		"group_id":   string(ar.GroupID),
	}
	if expiresAt != nil {
		fields["expires_at"] = expiresAt.Format(time.RFC3339)
	}
	audit("access_request_approved", fields)
	updated, _ := a.AccessRequests.Get(ctx, ar.ID)
	if updated == nil {
		updated = ar
	}
	writeJSON(w, a.accessRequestToResponse(r, updated))
}

// handleDenyAccessRequest: POST /api/access-requests/{id}/deny (admin).
func (a *App) handleDenyAccessRequest(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	ar, ok := a.loadPendingRequest(w, r)
	if !ok {
		return
	}
	var req decideAccessRequestRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
	}
	adminID := a.currentUserID(r)
	if err := a.AccessRequests.Decide(r.Context(), ar.ID, access.RequestDenied, adminID, strings.TrimSpace(req.Note), nil); err != nil {
		if errors.Is(err, access.ErrRequestNotPending) {
			writeJSONErrorKey(w, r, "accessRequests.notPending", http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("access_request_denied", auditFields{
		"user_id":    adminID,
		"request_id": ar.ID,
		"member_id":  string(ar.UserID),
		"group_id":   string(ar.GroupID),
	})
	updated, _ := a.AccessRequests.Get(r.Context(), ar.ID)
	if updated == nil {
		updated = ar
	}
	writeJSON(w, a.accessRequestToResponse(r, updated))
}

// handleCancelAccessRequest: DELETE /api/access-requests/{id} -- the
// requester withdraws a pending request (admins may cancel any).
func (a *App) handleCancelAccessRequest(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(a.currentUserID(r))
	if userID == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	ar, ok := a.loadPendingRequest(w, r)
	if !ok {
		return
	}
	isAdmin, _ := a.currentUserIsAdmin(r)
	if string(ar.UserID) != userID && !isAdmin {
		writeJSONErrorKey(w, r, "common.forbidden", http.StatusForbidden)
		return
	}
	if err := a.AccessRequests.Decide(r.Context(), ar.ID, access.RequestCancelled, userID, "", nil); err != nil {
		if errors.Is(err, access.ErrRequestNotPending) {
			writeJSONErrorKey(w, r, "accessRequests.notPending", http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("access_request_cancelled", auditFields{"user_id": userID, "request_id": ar.ID, "group_id": string(ar.GroupID)})
	w.WriteHeader(http.StatusNoContent)
}
