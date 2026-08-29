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
	// Passkeys counts registered WebAuthn credentials (second factor).
	Passkeys int `json:"passkeys"`
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
		passkeys := 0
		if a.WebAuthn != nil {
			passkeys, _ = a.WebAuthn.Count(r.Context(), u.ID)
		}
		out = append(out, userResponse{ID: u.ID, Username: u.Username, Role: role, Locale: u.Locale, Tags: tags, TOTPEnabled: totpEnabled, Passkeys: passkeys})
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

type updateUserRequest struct {
	Role *string `json:"role"`
}

// handleUpdateUser: PATCH /api/users/{user_id} {role}. Admin only. An
// admin may not demote themselves (they would lock themselves out of
// this very screen) and the last remaining admin cannot be demoted.
// Role changes take effect on the user's next request: every admin
// check reads the role from the store, not from the session.
func (a *App) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeJSONErrorKey(w, r, "users.idRequired", http.StatusBadRequest)
		return
	}
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	target, err := a.UserStore.GetByID(userID)
	if err != nil || target == nil {
		writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
		return
	}
	if req.Role != nil {
		role := strings.TrimSpace(*req.Role)
		if role != auth.RoleAdmin && role != auth.RoleUser {
			writeJSONErrorKey(w, r, "users.invalidRole", http.StatusBadRequest)
			return
		}
		current := target.Role
		if current == "" {
			current = auth.RoleUser
		}
		if role != current {
			if role == auth.RoleUser {
				if userID == a.currentUserID(r) {
					writeJSONErrorKey(w, r, "users.cannotDemoteSelf", http.StatusBadRequest)
					return
				}
				n, err := a.countAdmins()
				if err != nil {
					writeInternalError(w, err)
					return
				}
				if n <= 1 {
					writeJSONErrorKey(w, r, "users.lastAdminRole", http.StatusConflict)
					return
				}
			}
			if err := a.UserStore.UpdateRole(userID, role); err != nil {
				switch {
				case errors.Is(err, auth.ErrUserNotFound):
					writeJSONErrorKey(w, r, "users.userNotFound", http.StatusNotFound)
				case errors.Is(err, auth.ErrInvalidRole):
					writeJSONErrorKey(w, r, "users.invalidRole", http.StatusBadRequest)
				default:
					writeInternalError(w, err)
				}
				return
			}
			audit("user_role_update", auditFields{
				"user_id":   a.currentUserID(r),
				"target_id": userID,
				"from":      current,
				"to":        role,
			})
			target.Role = role
		}
	}
	tags, _ := a.UserStore.TagsForUser(target.ID)
	if tags == nil {
		tags = []string{}
	}
	role := target.Role
	if role == "" {
		role = auth.RoleUser
	}
	totpEnabled := a.TOTPStore != nil && a.TOTPStore.Enabled(r.Context(), target.ID)
	writeJSON(w, userResponse{ID: target.ID, Username: target.Username, Role: role, Locale: target.Locale, Tags: tags, TOTPEnabled: totpEnabled})
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
