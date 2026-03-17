package httpapi

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const auditForwardSettingKey = "audit_forwarder"

func loadAuditForwarderConfigFromDB(db *sql.DB) (auditForwarderConfig, bool) {
	if db == nil {
		return auditForwarderConfig{}, false
	}
	var raw string
	err := db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, auditForwardSettingKey).Scan(&raw)
	if err != nil {
		return auditForwarderConfig{}, false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return auditForwarderConfig{}, true
	}
	var cfg auditForwarderConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return auditForwarderConfig{}, false
	}
	return cfg, true
}

func saveAuditForwarderConfigToDB(db *sql.DB, cfg auditForwarderConfig) error {
	if db == nil {
		return sql.ErrConnDone
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		auditForwardSettingKey, string(b), time.Now().UTC(),
	)
	return err
}

