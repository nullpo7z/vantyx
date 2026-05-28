package access

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/secret"
)

type SQLiteCredentialIdentityStore struct {
	db       *sql.DB
	keyStore *SQLiteSSHKeyStore
	cfg      StoreConfig
	encKey   []byte
	timeout  time.Duration
}

func NewSQLiteCredentialIdentityStore(db *sql.DB, keyStore *SQLiteSSHKeyStore, cfg *StoreConfig, encKey []byte) *SQLiteCredentialIdentityStore {
	c := StoreConfig{QueryTimeout: 5 * time.Second, DefaultListLimit: DefaultListLimit}
	if cfg != nil {
		if cfg.QueryTimeout > 0 {
			c.QueryTimeout = cfg.QueryTimeout
		}
		if cfg.DefaultListLimit > 0 {
			c.DefaultListLimit = cfg.DefaultListLimit
		}
	}
	return &SQLiteCredentialIdentityStore{db: db, keyStore: keyStore, cfg: c, encKey: encKey, timeout: c.QueryTimeout}
}

func credentialIdentityAAD(id CredentialIdentityID, field string) []byte {
	return []byte("vantyx/credential_identity/" + string(id) + "/" + field)
}

func (s *SQLiteCredentialIdentityStore) summaryFromRow(id, label, user, pw, keyID, keyLabel, keyPass string) CredentialIdentitySummary {
	sum := CredentialIdentitySummary{
		ID:            CredentialIdentityID(id),
		Label:         label,
		SSHUsername:   user,
		HasPassword:   strings.TrimSpace(pw) != "",
		SSHKeyID:      SSHKeyID(keyID),
		SSHKeyLabel:   keyLabel,
		HasSSHKey:     strings.TrimSpace(keyID) != "",
		HasPassphrase: strings.TrimSpace(keyPass) != "",
	}
	return sum
}

func (s *SQLiteCredentialIdentityStore) List(ctx context.Context) ([]CredentialIdentitySummary, error) {
	if s.db == nil {
		return []CredentialIdentitySummary{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.label, i.ssh_username, i.ssh_password, COALESCE(i.ssh_key_id, ''), COALESCE(k.label, ''), COALESCE(k.ssh_private_key_passphrase, '')
		FROM credential_identities i
		LEFT JOIN ssh_keys k ON k.id = i.ssh_key_id
		ORDER BY i.label ASC, i.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CredentialIdentitySummary
	for rows.Next() {
		var id, label, user, pw, keyID, keyLabel, keyPass string
		if err := rows.Scan(&id, &label, &user, &pw, &keyID, &keyLabel, &keyPass); err != nil {
			return nil, err
		}
		out = append(out, s.summaryFromRow(id, label, user, pw, keyID, keyLabel, keyPass))
	}
	return out, rows.Err()
}

func (s *SQLiteCredentialIdentityStore) Create(ctx context.Context, id CredentialIdentityID, label, sshUsername, sshPassword string, sshKeyID SSHKeyID) (*CredentialIdentitySummary, error) {
	if err := validateCredentialIdentityID(id); err != nil {
		return nil, err
	}
	if err := validateCredentialIdentityLabel(label); err != nil {
		return nil, err
	}
	sshUsername = strings.TrimSpace(sshUsername)
	if sshUsername == "" {
		return nil, ErrCredentialIdentityUserReq
	}
	sshKeyID = SSHKeyID(strings.TrimSpace(string(sshKeyID)))
	if err := validateCredentialIdentityAuth(sshPassword, sshKeyID); err != nil {
		return nil, err
	}
	if sshKeyID != "" {
		if _, err := s.keyStore.GetDecrypted(ctx, sshKeyID); err != nil {
			return nil, err
		}
	}
	var storedPw string
	if strings.TrimSpace(sshPassword) != "" {
		if s.encKey == nil || len(s.encKey) != secret.KeySize {
			return nil, ErrEncryptionKeyRequired
		}
		enc, err := secret.EncryptWithAAD(s.encKey, strings.TrimSpace(sshPassword), credentialIdentityAAD(id, "ssh_password"))
		if err != nil {
			return nil, err
		}
		storedPw = enc
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	keyIDVal := ""
	if sshKeyID != "" {
		keyIDVal = string(sshKeyID)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO credential_identities (id, label, ssh_username, ssh_password, ssh_key_id) VALUES (?, ?, ?, ?, NULLIF(?, ''))`,
		string(id), strings.TrimSpace(label), sshUsername, storedPw, keyIDVal)
	if err != nil {
		return nil, ErrCredentialIdentityExists
	}
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.ID == id {
			return &it, nil
		}
	}
	return &CredentialIdentitySummary{ID: id, Label: strings.TrimSpace(label), SSHUsername: sshUsername, HasPassword: storedPw != "", SSHKeyID: sshKeyID, HasSSHKey: sshKeyID != ""}, nil
}

func (s *SQLiteCredentialIdentityStore) Update(ctx context.Context, id CredentialIdentityID, label, sshUsername string, sshPassword *string, sshKeyID *string) (*CredentialIdentitySummary, error) {
	if err := validateCredentialIdentityID(id); err != nil {
		return nil, err
	}
	if label != "" {
		if err := validateCredentialIdentityLabel(label); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var curLabel, curUser, curPw, curKeyID string
	err := s.db.QueryRowContext(ctx, `SELECT label, ssh_username, ssh_password, COALESCE(ssh_key_id, '') FROM credential_identities WHERE id = ?`, string(id)).
		Scan(&curLabel, &curUser, &curPw, &curKeyID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrCredentialIdentityNotFound
		}
		return nil, err
	}
	nextLabel := curLabel
	if strings.TrimSpace(label) != "" {
		nextLabel = strings.TrimSpace(label)
	}
	nextUser := strings.TrimSpace(sshUsername)
	if nextUser == "" {
		nextUser = curUser
	}
	if nextUser == "" {
		return nil, ErrCredentialIdentityUserReq
	}
	nextPwStored := curPw
	if sshPassword != nil {
		raw := strings.TrimSpace(*sshPassword)
		if raw == "" {
			nextPwStored = ""
		} else {
			if s.encKey == nil || len(s.encKey) != secret.KeySize {
				return nil, ErrEncryptionKeyRequired
			}
			enc, err := secret.EncryptWithAAD(s.encKey, raw, credentialIdentityAAD(id, "ssh_password"))
			if err != nil {
				return nil, err
			}
			nextPwStored = enc
		}
	}
	nextKeyID := curKeyID
	if sshKeyID != nil {
		nextKeyID = strings.TrimSpace(*sshKeyID)
		if nextKeyID != "" {
			if _, err := s.keyStore.GetDecrypted(ctx, SSHKeyID(nextKeyID)); err != nil {
				return nil, err
			}
		}
	}
	if err := validateCredentialIdentityAuth(decryptIdentityPassword(s, id, nextPwStored), SSHKeyID(nextKeyID)); err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE credential_identities SET label = ?, ssh_username = ?, ssh_password = ?, ssh_key_id = NULLIF(?, ''), updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		nextLabel, nextUser, nextPwStored, nextKeyID, string(id))
	if err != nil {
		return nil, err
	}
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.ID == id {
			return &it, nil
		}
	}
	return nil, ErrCredentialIdentityNotFound
}

func decryptIdentityPassword(s *SQLiteCredentialIdentityStore, id CredentialIdentityID, stored string) string {
	if stored == "" {
		return ""
	}
	if s.encKey == nil || len(s.encKey) != secret.KeySize {
		return ""
	}
	v, err := secret.DecryptWithAAD(s.encKey, stored, credentialIdentityAAD(id, "ssh_password"))
	if err != nil {
		return ""
	}
	return v
}

func (s *SQLiteCredentialIdentityStore) Delete(ctx context.Context, id CredentialIdentityID) error {
	if err := validateCredentialIdentityID(id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM credential_identities WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCredentialIdentityNotFound
	}
	return nil
}

func (s *SQLiteCredentialIdentityStore) GetDecrypted(ctx context.Context, id CredentialIdentityID) (*CredentialIdentity, error) {
	if err := validateCredentialIdentityID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var label, user, pw, keyID string
	err := s.db.QueryRowContext(ctx, `SELECT label, ssh_username, ssh_password, COALESCE(ssh_key_id, '') FROM credential_identities WHERE id = ?`, string(id)).
		Scan(&label, &user, &pw, &keyID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrCredentialIdentityNotFound
		}
		return nil, err
	}
	out := &CredentialIdentity{
		ID:          id,
		Label:       label,
		SSHUsername: user,
		SSHPassword: decryptIdentityPassword(s, id, pw),
		SSHKeyID:    SSHKeyID(keyID),
	}
	if keyID != "" {
		k, err := s.keyStore.GetDecrypted(ctx, SSHKeyID(keyID))
		if err != nil {
			return nil, err
		}
		out.Key = k
	}
	return out, nil
}
