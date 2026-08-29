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
	Locale   string   `json:"locale,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	// TOTPEnabled lets the admin list show who has a second factor so
	// the "reset 2FA" action is offered only where it applies.
	TOTPEnabled bool `json:"totp_enabled"`
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
		totpEnabled := false
		if a.TOTPStore != nil {
			totpEnabled = a.TOTPStore.Enabled(r.Context(), u.ID)
		}
		out = append(out, userResponse{ID: u.ID, Username: u.Username, Role: role, Locale: u.Locale, Tags: tags, TOTPEnabled: totpEnabled})
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
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.ID = strings.TrimSpace(req.ID)
	req.Role = strings.TrimSpace(req.Role)
	if req.Username == "" {
		writeJSONErrorKey(w, r, "users.usernameRequired", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		writeJSONErrorKey(w, r, "users.passwordRequired", http.StatusBadRequest)
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
		switch {
		case errors.Is(err, auth.ErrUserExists):
			writeJSONErrorKey(w, r, "users.alreadyExists", http.StatusConflict)
		case errors.Is(err, auth.ErrIDOrUsernameEmpty):
			writeJSONErrorKey(w, r, "validation.idUsernameEmpty", http.StatusBadRequest)
		case errors.Is(err, auth.ErrEmptyPassword):
			writeJSONErrorKey(w, r, "auth.passwordEmpty", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeJSONErrorKey(w, r, "auth.passwordTooShort", http.StatusBadRequest, "min", auth.MinPasswordLength)
		case errors.Is(err, auth.ErrPasswordNoUpper):
			writeJSONErrorKey(w, r, "auth.passwordNoUpper", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordNoLower):
			writeJSONErrorKey(w, r, "auth.passwordNoLower", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordNoDigit):
			writeJSONErrorKey(w, r, "auth.passwordNoDigit", http.StatusBadRequest)
		case errors.Is(err, auth.ErrPasswordNoSpecial):
			writeJSONErrorKey(w, r, "auth.passwordNoSpecial", http.StatusBadRequest)
		default:
			writeInternalError(w, err)
		}
		return
	}
	role := u.Role
	if role == "" {
		role = auth.RoleUser
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(userResponse{ID: u.ID, Username: u.Username, Role: role, Locale: u.Locale})
}

// handleUserTags returns tags for the user. Admin only.
func (a *App) handleUserTags(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := chi.URLParam(r, "user_id")
	userID = strings.TrimSpace(userID)
	if userID == "" {
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
		return
	}
	tags, err := a.UserStore.TagsForUser(userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
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
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
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
	if err := a.UserStore.SetUserTags(userID, req.Tags); err != nil {
		switch {
		case errors.Is(err, auth.ErrUserNotFound):
			writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
		case errors.Is(err, auth.ErrTagLength):
			writeJSONErrorKey(w, r, "tags.lengthInvalid", http.StatusBadRequest)
		case errors.Is(err, auth.ErrTagChars):
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
