package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
)

type credentialIdentitySummaryResponse struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	SSHUsername   string `json:"ssh_username"`
	HasPassword   bool   `json:"has_password"`
	SSHKeyID      string `json:"ssh_key_id,omitempty"`
	SSHKeyLabel   string `json:"ssh_key_label,omitempty"`
	HasSSHKey     bool   `json:"has_ssh_key"`
	HasPassphrase bool   `json:"has_passphrase"`
	AuthMethod    string `json:"auth_method,omitempty"`
}

type createCredentialIdentityRequest struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	SSHUsername string `json:"ssh_username"`
	SSHPassword string `json:"ssh_password"`
	SSHKeyID    string `json:"ssh_key_id"`
}

type updateCredentialIdentityRequest struct {
	Label       string  `json:"label"`
	SSHUsername string  `json:"ssh_username"`
	SSHPassword *string `json:"ssh_password,omitempty"`
	SSHKeyID    *string `json:"ssh_key_id,omitempty"`
}

func identityAuthMethod(s access.CredentialIdentitySummary) string {
	if s.HasPassword && s.HasSSHKey {
		return "password_and_key"
	}
	if s.HasSSHKey {
		return "key"
	}
	if s.HasPassword {
		return "password"
	}
	return ""
}

func identityToResponse(s access.CredentialIdentitySummary) credentialIdentitySummaryResponse {
	return credentialIdentitySummaryResponse{
		ID:            string(s.ID),
		Label:         s.Label,
		SSHUsername:   s.SSHUsername,
		HasPassword:   s.HasPassword,
		SSHKeyID:      string(s.SSHKeyID),
		SSHKeyLabel:   s.SSHKeyLabel,
		HasSSHKey:     s.HasSSHKey,
		HasPassphrase: s.HasPassphrase,
		AuthMethod:    identityAuthMethod(s),
	}
}

func (a *App) handleCredentialIdentitiesList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	items, err := a.CredentialIdentityStore.List(r.Context())
	if err != nil {
		if writeCredentialSchemaError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	out := make([]credentialIdentitySummaryResponse, 0, len(items))
	for _, it := range items {
		out = append(out, identityToResponse(it))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
}

func (a *App) handleCredentialIdentitiesCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req createCredentialIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Label = strings.TrimSpace(req.Label)
	req.SSHUsername = strings.TrimSpace(req.SSHUsername)
	req.SSHKeyID = strings.TrimSpace(req.SSHKeyID)
	sum, err := a.CredentialIdentityStore.Create(r.Context(), access.CredentialIdentityID(req.ID), req.Label, req.SSHUsername, req.SSHPassword, access.SSHKeyID(req.SSHKeyID))
	if err != nil {
		if writeAccessValidationError(w, r, err) {
			return
		}
		switch err {
		case access.ErrCredentialIdentityExists:
			writeJSONErrorKey(w, r, "credentialIdentities.exists", http.StatusConflict)
		case access.ErrSSHKeyNotFound:
			writeJSONErrorKey(w, r, "sshKeys.notFound", http.StatusNotFound)
		case access.ErrEncryptionKeyRequired:
			writeJSONErrorKey(w, r, "credentials.encryptionKeyRequired", http.StatusServiceUnavailable)
		default:
			writeInternalError(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(identityToResponse(*sum))
}

func (a *App) handleCredentialIdentitiesUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "identity_id"))
	if id == "" {
		writeJSONErrorKey(w, r, "credentialIdentities.idRequired", http.StatusBadRequest)
		return
	}
	var req updateCredentialIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	req.SSHUsername = strings.TrimSpace(req.SSHUsername)
	var keyPtr *string
	if req.SSHKeyID != nil {
		v := strings.TrimSpace(*req.SSHKeyID)
		keyPtr = &v
	}
	sum, err := a.CredentialIdentityStore.Update(r.Context(), access.CredentialIdentityID(id), req.Label, req.SSHUsername, req.SSHPassword, keyPtr)
	if err != nil {
		if writeAccessValidationError(w, r, err) {
			return
		}
		switch err {
		case access.ErrCredentialIdentityNotFound:
			writeJSONErrorKey(w, r, "credentialIdentities.notFound", http.StatusNotFound)
		case access.ErrSSHKeyNotFound:
			writeJSONErrorKey(w, r, "sshKeys.notFound", http.StatusNotFound)
		case access.ErrEncryptionKeyRequired:
			writeJSONErrorKey(w, r, "credentials.encryptionKeyRequired", http.StatusServiceUnavailable)
		default:
			writeInternalError(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(identityToResponse(*sum))
}

func (a *App) handleCredentialIdentitiesDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "identity_id"))
	if id == "" {
		writeJSONErrorKey(w, r, "credentialIdentities.idRequired", http.StatusBadRequest)
		return
	}
	err := a.CredentialIdentityStore.Delete(r.Context(), access.CredentialIdentityID(id))
	if err != nil {
		switch err {
		case access.ErrCredentialIdentityNotFound:
			writeJSONErrorKey(w, r, "credentialIdentities.notFound", http.StatusNotFound)
		case access.ErrCredentialIdentityInUse:
			writeJSONErrorKey(w, r, "credentialIdentities.inUse", http.StatusConflict)
		default:
			writeInternalError(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
