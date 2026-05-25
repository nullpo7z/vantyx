package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/auth"
)

type userResponse struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Role     string   `json:"role"`
	Tags     []string `json:"tags,omitempty"`
}

type createUserRequest struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// handleListUsers returns all users. Admin only. Password hashes are
// never returned.
func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	users, err := a.UserStore.ListUsers(limit, offset)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, u := range users {
		role := u.Role
		if role == "" {
			role = auth.RoleUser
		}
		tags, _ := a.UserStore.TagsForUser(u.ID)
		if tags == nil {
			tags = []string{}
		}
		out = append(out, userResponse{ID: u.ID, Username: u.Username, Role: role, Tags: tags})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// handleCreateUser creates a new user. Admin only.
func (a *App) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.ID = strings.TrimSpace(req.ID)
	req.Role = strings.TrimSpace(req.Role)
	if req.Username == "" {
		writeJSONError(w, "username is required", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		writeJSONError(w, "password is required", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		req.ID = slugID(req.Username)
	}
	if req.Role != auth.RoleAdmin && req.Role != auth.RoleUser {
		req.Role = auth.RoleUser
	}
	u, err := a.UserStore.CreateUser(req.ID, req.Username, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeJSONError(w, "user already exists (id or username)", http.StatusConflict)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	role := u.Role
	if role == "" {
		role = auth.RoleUser
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(userResponse{ID: u.ID, Username: u.Username, Role: role})
}

// handleUserTags returns tags for the user. Admin only.
func (a *App) handleUserTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := chi.URLParam(r, "user_id")
	userID = strings.TrimSpace(userID)
	if userID == "" {
		writeJSONError(w, "user_id required", http.StatusBadRequest)
		return
	}
	tags, err := a.UserStore.TagsForUser(userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "user not found", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse{Tags: tags})
}

// handleSetUserTags sets tags for the user. Admin only.
func (a *App) handleSetUserTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := chi.URLParam(r, "user_id")
	userID = strings.TrimSpace(userID)
	if userID == "" {
		writeJSONError(w, "user_id required", http.StatusBadRequest)
		return
	}
	var req setTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	if err := a.UserStore.SetUserTags(userID, req.Tags); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONError(w, "user not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tagsResponse(req))
}
