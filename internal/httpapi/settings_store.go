package httpapi

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const auditForwardSettingKey = "audit_forwarder"

// timezoneSettingKey holds the site-wide IANA timezone used by every
// client to format timestamps ("" = each browser's local zone). It is
// an admin decision, not a per-user preference, so recordings, audit
// entries and session lists read the same everywhere.
const timezoneSettingKey = "timezone"

func loadTimezoneSettingFromDB(db *sql.DB) string {
	if db == nil {
		return ""
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, timezoneSettingKey).Scan(&raw); err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

func saveTimezoneSettingToDB(db *sql.DB, tz string) error {
	if db == nil {
		return sql.ErrConnDone
	}
	_, err := db.Exec(
		`INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		timezoneSettingKey, tz, time.Now().UTC(),
	)
	return err
}

// globalTimezone returns the configured site-wide timezone ("" when
// unset). Included in login / GET /api/me responses so every client
// applies it on boot.
func (a *App) globalTimezone() string {
	return loadTimezoneSettingFromDB(a.DB)
}

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
