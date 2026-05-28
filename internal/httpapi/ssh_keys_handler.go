package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
)

type sshKeySummaryResponse struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	KeyType       string `json:"key_type,omitempty"`
	HasPassphrase bool   `json:"has_passphrase"`
}

type createSSHKeyRequest struct {
	ID                     string `json:"id"`
	Label                  string `json:"label"`
	SSHPrivateKey          string `json:"ssh_private_key"`
	SSHPrivateKeyPassphrase string `json:"ssh_private_key_passphrase"`
}

type updateSSHKeyRequest struct {
	Label                  string  `json:"label"`
	SSHPrivateKey          *string `json:"ssh_private_key,omitempty"`
	SSHPrivateKeyPassphrase *string `json:"ssh_private_key_passphrase,omitempty"`
}

func sshKeyToResponse(s access.SSHKeySummary) sshKeySummaryResponse {
	return sshKeySummaryResponse{
		ID:            string(s.ID),
		Label:         s.Label,
		KeyType:       s.KeyType,
		HasPassphrase: s.HasPassphrase,
	}
}

func (a *App) handleSSHKeysList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	items, err := a.SSHKeyStore.List(r.Context())
	if err != nil {
		if writeCredentialSchemaError(w, r, err) {
			return
		}
		writeInternalError(w, err)
		return
	}
	out := make([]sshKeySummaryResponse, 0, len(items))
	for _, it := range items {
		out = append(out, sshKeyToResponse(it))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
}

func (a *App) handleSSHKeysCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req createSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Label = strings.TrimSpace(req.Label)
	sum, err := a.SSHKeyStore.Create(r.Context(), access.SSHKeyID(req.ID), req.Label, req.SSHPrivateKey, req.SSHPrivateKeyPassphrase)
	if err != nil {
		if writeAccessValidationError(w, r, err) {
			return
		}
		switch err {
		case access.ErrSSHKeyExists:
			writeJSONErrorKey(w, r, "sshKeys.exists", http.StatusConflict)
		case access.ErrEncryptionKeyRequired:
			writeJSONErrorKey(w, r, "credentials.encryptionKeyRequired", http.StatusServiceUnavailable)
		default:
			writeInternalError(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sshKeyToResponse(*sum))
}

func (a *App) handleSSHKeysUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "key_id"))
	if id == "" {
		writeJSONErrorKey(w, r, "sshKeys.idRequired", http.StatusBadRequest)
		return
	}
	var req updateSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	sum, err := a.SSHKeyStore.Update(r.Context(), access.SSHKeyID(id), req.Label, req.SSHPrivateKey, req.SSHPrivateKeyPassphrase)
	if err != nil {
		if writeAccessValidationError(w, r, err) {
			return
		}
		switch err {
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
	_ = json.NewEncoder(w).Encode(sshKeyToResponse(*sum))
}

func (a *App) handleSSHKeysDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "key_id"))
	if id == "" {
		writeJSONErrorKey(w, r, "sshKeys.idRequired", http.StatusBadRequest)
		return
	}
	err := a.SSHKeyStore.Delete(r.Context(), access.SSHKeyID(id))
	if err != nil {
		switch err {
		case access.ErrSSHKeyNotFound:
			writeJSONErrorKey(w, r, "sshKeys.notFound", http.StatusNotFound)
		case access.ErrSSHKeyInUse:
			writeJSONErrorKey(w, r, "sshKeys.inUse", http.StatusConflict)
		default:
			writeInternalError(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
