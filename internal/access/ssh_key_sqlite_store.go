package access

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/secret"
)

type SQLiteSSHKeyStore struct {
	db      *sql.DB
	cfg     StoreConfig
	encKey  []byte
	timeout time.Duration
}

func NewSQLiteSSHKeyStore(db *sql.DB, cfg *StoreConfig, encKey []byte) *SQLiteSSHKeyStore {
	c := StoreConfig{QueryTimeout: 5 * time.Second, DefaultListLimit: DefaultListLimit}
	if cfg != nil {
		if cfg.QueryTimeout > 0 {
			c.QueryTimeout = cfg.QueryTimeout
		}
		if cfg.DefaultListLimit > 0 {
			c.DefaultListLimit = cfg.DefaultListLimit
		}
	}
	return &SQLiteSSHKeyStore{db: db, cfg: c, encKey: encKey, timeout: c.QueryTimeout}
}

func sshKeyAAD(id SSHKeyID, field string) []byte {
	return []byte("vantyx/ssh_key/" + string(id) + "/" + field)
}

func (s *SQLiteSSHKeyStore) encryptSecrets(id SSHKeyID, sshPrivateKey, sshPrivateKeyPassphrase string) (storedKey, storedPass string, err error) {
	if (sshPrivateKey != "" || sshPrivateKeyPassphrase != "") && (s.encKey == nil || len(s.encKey) != secret.KeySize) {
		return "", "", ErrEncryptionKeyRequired
	}
	if sshPrivateKey != "" {
		storedKey, err = secret.EncryptWithAAD(s.encKey, sshPrivateKey, sshKeyAAD(id, "ssh_private_key"))
		if err != nil {
			return "", "", err
		}
	}
	if sshPrivateKeyPassphrase != "" {
		storedPass, err = secret.EncryptWithAAD(s.encKey, sshPrivateKeyPassphrase, sshKeyAAD(id, "ssh_private_key_passphrase"))
		if err != nil {
			return "", "", err
		}
	}
	return storedKey, storedPass, nil
}

func (s *SQLiteSSHKeyStore) List(ctx context.Context) ([]SSHKeySummary, error) {
	if s.db == nil {
		return []SSHKeySummary{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id, label, COALESCE(key_type, ''), ssh_private_key_passphrase FROM ssh_keys ORDER BY label ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SSHKeySummary
	for rows.Next() {
		var id, label, keyType, pass string
		if err := rows.Scan(&id, &label, &keyType, &pass); err != nil {
			return nil, err
		}
		out = append(out, SSHKeySummary{
			ID:            SSHKeyID(id),
			Label:         label,
			KeyType:       keyType,
			HasPassphrase: strings.TrimSpace(pass) != "",
		})
	}
	return out, rows.Err()
}

func (s *SQLiteSSHKeyStore) Create(ctx context.Context, id SSHKeyID, label, sshPrivateKey, sshPrivateKeyPassphrase string) (*SSHKeySummary, error) {
	if err := validateSSHKeyID(id); err != nil {
		return nil, err
	}
	if err := validateSSHKeyLabel(label); err != nil {
		return nil, err
	}
	sshPrivateKey = strings.TrimSpace(sshPrivateKey)
	if sshPrivateKey == "" {
		return nil, ErrSSHKeyPrivateReq
	}
	keyType := DetectSSHKeyType(sshPrivateKey, sshPrivateKeyPassphrase)
	storedKey, storedPass, err := s.encryptSecrets(id, sshPrivateKey, sshPrivateKeyPassphrase)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `INSERT INTO ssh_keys (id, label, key_type, ssh_private_key, ssh_private_key_passphrase) VALUES (?, ?, ?, ?, ?)`,
		string(id), strings.TrimSpace(label), keyType, storedKey, storedPass)
	if err != nil {
		return nil, ErrSSHKeyExists
	}
	return &SSHKeySummary{
		ID:            id,
		Label:         strings.TrimSpace(label),
		KeyType:       keyType,
		HasPassphrase: sshPrivateKeyPassphrase != "",
	}, nil
}

func (s *SQLiteSSHKeyStore) Update(ctx context.Context, id SSHKeyID, label string, sshPrivateKey, sshPrivateKeyPassphrase *string) (*SSHKeySummary, error) {
	if err := validateSSHKeyID(id); err != nil {
		return nil, err
	}
	if label != "" {
		if err := validateSSHKeyLabel(label); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var curLabel, curKeyType, curKey, curPass string
	err := s.db.QueryRowContext(ctx, `SELECT label, COALESCE(key_type, ''), ssh_private_key, ssh_private_key_passphrase FROM ssh_keys WHERE id = ?`, string(id)).
		Scan(&curLabel, &curKeyType, &curKey, &curPass)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSSHKeyNotFound
		}
		return nil, err
	}
	nextLabel := curLabel
	if strings.TrimSpace(label) != "" {
		nextLabel = strings.TrimSpace(label)
	}
	nextKeyStored := curKey
	nextPassStored := curPass
	nextKeyType := curKeyType
	if sshPrivateKey != nil {
		raw := strings.TrimSpace(*sshPrivateKey)
		if raw == "" {
			return nil, ErrSSHKeyPrivateReq
		}
		passForDetect := ""
		if sshPrivateKeyPassphrase != nil {
			passForDetect = *sshPrivateKeyPassphrase
		} else if strings.TrimSpace(curPass) != "" && s.encKey != nil && len(s.encKey) == secret.KeySize {
			passForDetect, _ = secret.DecryptWithAAD(s.encKey, curPass, sshKeyAAD(id, "ssh_private_key_passphrase"))
		}
		nextKeyType = DetectSSHKeyType(raw, passForDetect)
		if s.encKey == nil || len(s.encKey) != secret.KeySize {
			return nil, ErrEncryptionKeyRequired
		}
		enc, err := secret.EncryptWithAAD(s.encKey, raw, sshKeyAAD(id, "ssh_private_key"))
		if err != nil {
			return nil, err
		}
		nextKeyStored = enc
	} else if sshPrivateKeyPassphrase != nil && strings.TrimSpace(curKey) != "" && s.encKey != nil && len(s.encKey) == secret.KeySize {
		rawKey, decErr := secret.DecryptWithAAD(s.encKey, curKey, sshKeyAAD(id, "ssh_private_key"))
		if decErr == nil {
			nextKeyType = DetectSSHKeyType(rawKey, *sshPrivateKeyPassphrase)
		}
	}
	if sshPrivateKeyPassphrase != nil {
		raw := *sshPrivateKeyPassphrase
		if raw == "" {
			nextPassStored = ""
		} else {
			if s.encKey == nil || len(s.encKey) != secret.KeySize {
				return nil, ErrEncryptionKeyRequired
			}
			enc, err := secret.EncryptWithAAD(s.encKey, raw, sshKeyAAD(id, "ssh_private_key_passphrase"))
			if err != nil {
				return nil, err
			}
			nextPassStored = enc
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE ssh_keys SET label = ?, key_type = ?, ssh_private_key = ?, ssh_private_key_passphrase = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		nextLabel, nextKeyType, nextKeyStored, nextPassStored, string(id))
	if err != nil {
		return nil, err
	}
	return &SSHKeySummary{
		ID:            id,
		Label:         nextLabel,
		KeyType:       nextKeyType,
		HasPassphrase: strings.TrimSpace(nextPassStored) != "",
	}, nil
}

func (s *SQLiteSSHKeyStore) Delete(ctx context.Context, id SSHKeyID) error {
	if err := validateSSHKeyID(id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM credential_identities WHERE ssh_key_id = ?`, string(id)).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrSSHKeyInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM ssh_keys WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return ErrSSHKeyNotFound
	}
	return nil
}

func (s *SQLiteSSHKeyStore) GetDecrypted(ctx context.Context, id SSHKeyID) (*SSHKey, error) {
	if err := validateSSHKeyID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var label, key, pass string
	err := s.db.QueryRowContext(ctx, `SELECT label, ssh_private_key, ssh_private_key_passphrase FROM ssh_keys WHERE id = ?`, string(id)).
		Scan(&label, &key, &pass)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSSHKeyNotFound
		}
		return nil, err
	}
	dec := func(stored, field string) string {
		if stored == "" {
			return ""
		}
		if s.encKey == nil || len(s.encKey) != secret.KeySize {
			return ""
		}
		v, err := secret.DecryptWithAAD(s.encKey, stored, sshKeyAAD(id, field))
		if err != nil {
			return ""
		}
		return v
	}
	return &SSHKey{
		ID:                      id,
		Label:                   label,
		SSHPrivateKey:           dec(key, "ssh_private_key"),
		SSHPrivateKeyPassphrase: dec(pass, "ssh_private_key_passphrase"),
	}, nil
}
