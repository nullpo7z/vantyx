package httpapi

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/ssh"

	"github.com/nullpo7z/vantyx/internal/access"
)

type sshKeySummaryResponse struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	KeyType       string `json:"key_type,omitempty"`
	HasPassphrase bool   `json:"has_passphrase"`
}

type createSSHKeyRequest struct {
	ID                      string `json:"id"`
	Label                   string `json:"label"`
	SSHPrivateKey           string `json:"ssh_private_key"`
	SSHPrivateKeyPassphrase string `json:"ssh_private_key_passphrase"`
}

type updateSSHKeyRequest struct {
	Label                   string  `json:"label"`
	SSHPrivateKey           *string `json:"ssh_private_key,omitempty"`
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

type generateSSHKeyRequest struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	KeyType    string `json:"key_type,omitempty"` // "ed25519" (default) or "rsa"
	Passphrase string `json:"passphrase,omitempty"`
}

// generateSSHKeyResponse is a one-time reveal: PrivateKey / PublicKey are
// only ever present in this response. The key is stored via the same
// path as a manually pasted key (SSHKeyStore.Create), which never
// returns the raw private key again afterwards -- List/Get only expose
// the label / key type / has_passphrase, matching every other secret
// in this store (ASVS V6: secrets are write-only via the API once set).
type generateSSHKeyResponse struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	KeyType       string `json:"key_type,omitempty"`
	HasPassphrase bool   `json:"has_passphrase"`
	PrivateKey    string `json:"private_key"`
	PublicKey     string `json:"public_key"`
}

var errUnsupportedSSHKeyType = errors.New("unsupported ssh key type")

// generateSSHKeyMaterial creates a fresh SSH key pair and returns the
// private key PEM-encoded in OpenSSH format (passphrase-encrypted when
// passphrase is non-empty) plus the corresponding authorized_keys
// line. keyType is "ed25519" (default, recommended) or "rsa" (4096-bit,
// for targets that don't yet support ed25519).
func generateSSHKeyMaterial(keyType, passphrase, comment string) (privatePEM, publicLine string, err error) {
	var pub crypto.PublicKey
	var priv crypto.PrivateKey
	switch keyType {
	case "", "ed25519":
		pk, sk, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return "", "", genErr
		}
		pub, priv = pk, sk
	case "rsa":
		sk, genErr := rsa.GenerateKey(rand.Reader, 4096)
		if genErr != nil {
			return "", "", genErr
		}
		pub, priv = &sk.PublicKey, sk
	default:
		return "", "", errUnsupportedSSHKeyType
	}
	var block *pem.Block
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return "", "", err
	}
	privatePEM = string(pem.EncodeToMemory(block))
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	publicLine = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		publicLine += " " + comment
	}
	return privatePEM, publicLine, nil
}

// handleSSHKeysGenerate creates a brand-new SSH key pair, stores it
// exactly like a manually pasted key, and returns the private/public
// key material once so the admin can download it and install the
// public half on the target. Admin-only.
func (a *App) handleSSHKeysGenerate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req generateSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Label = strings.TrimSpace(req.Label)
	keyType := strings.ToLower(strings.TrimSpace(req.KeyType))
	privatePEM, publicLine, err := generateSSHKeyMaterial(keyType, req.Passphrase, req.Label)
	if err != nil {
		if errors.Is(err, errUnsupportedSSHKeyType) {
			writeJSONErrorKey(w, r, "sshKeys.unsupportedKeyType", http.StatusBadRequest)
			return
		}
		writeInternalError(w, err)
		return
	}
	sum, err := a.SSHKeyStore.Create(r.Context(), access.SSHKeyID(req.ID), req.Label, privatePEM, req.Passphrase)
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
	_ = json.NewEncoder(w).Encode(generateSSHKeyResponse{
		ID:            string(sum.ID),
		Label:         sum.Label,
		KeyType:       sum.KeyType,
		HasPassphrase: sum.HasPassphrase,
		PrivateKey:    privatePEM,
		PublicKey:     publicLine,
	})
}
