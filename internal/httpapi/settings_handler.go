package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/nullpo7z/vantyx/internal/auth"
)

type timezoneSettingResponse struct {
	// Timezone is the site-wide IANA zone name, or "" for "browser local".
	Timezone string `json:"timezone"`
}

// handleGetTimezoneSetting: GET /api/settings/timezone (any signed-in
// user; the value is also embedded in GET /api/me).
func (a *App) handleGetTimezoneSetting(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(a.currentUserID(r)) == "" {
		writeJSONErrorKey(w, r, "common.unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, timezoneSettingResponse{Timezone: a.globalTimezone()})
}

// handlePutTimezoneSetting: PUT /api/settings/timezone {timezone} (admin
// only). {"timezone":""} restores "browser local".
func (a *App) handlePutTimezoneSetting(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var in timezoneSettingResponse
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	tz, err := auth.NormalizeUITimezone(in.Timezone)
	if err != nil {
		writeJSONErrorKey(w, r, "auth.unsupportedTimezone", http.StatusBadRequest)
		return
	}
	if err := saveTimezoneSettingToDB(a.DB, tz); err != nil {
		writeJSONErrorKey(w, r, "settings.saveFailed", http.StatusInternalServerError)
		return
	}
	audit("settings_update", auditFields{
		"user_id":  a.currentUserID(r),
		"key":      timezoneSettingKey,
		"timezone": tz,
	})
	writeJSON(w, timezoneSettingResponse{Timezone: tz})
}

type auditForwarderSettingsResponse struct {
	// Saved config (admin-configured). When not configured, returns env-based defaults.
	Config auditForwarderConfig `json:"config"`
	Source string               `json:"source"` // "db" | "env"
}

func (a *App) handleGetAuditForwarderSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	cfg, ok := loadAuditForwarderConfigFromDB(a.DB)
	if ok {
		writeJSON(w, auditForwarderSettingsResponse{Config: cfg, Source: "db"})
		return
	}
	// Fallback: best-effort from env. (Not all env fields are introspectable; keep simple.)
	envCfg := auditForwarderConfig{
		Enabled: strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_ADDR")) != "" || strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_PROTO")) == "unixgram" || strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_PROTO")) == "unix",
		Proto:   strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_PROTO")),
		Addr:    strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_ADDR")),
		App:     strings.TrimSpace(os.Getenv("VANTYX_AUDIT_FORWARD_APP")),
	}
	writeJSON(w, auditForwarderSettingsResponse{Config: envCfg, Source: "env"})
}

func (a *App) handlePutAuditForwarderSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var in struct {
		Config auditForwarderConfig `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	cfg := in.Config
	cfg.Proto = strings.TrimSpace(cfg.Proto)
	cfg.Addr = strings.TrimSpace(cfg.Addr)
	cfg.App = strings.TrimSpace(cfg.App)
	if cfg.Proto == "" {
		cfg.Proto = "udp"
	}
	switch cfg.Proto {
	case "udp", "tcp", "unix", "unixgram":
	default:
		writeJSONErrorKey(w, r, "settings.invalidProto", http.StatusBadRequest)
		return
	}
	if cfg.Buffer < 0 || cfg.Buffer > 200000 {
		writeJSONErrorKey(w, r, "settings.invalidBuffer", http.StatusBadRequest)
		return
	}
	// If enabled, require addr (except unix/unixgram defaults to /dev/log when empty).
	if cfg.Enabled && cfg.Addr == "" && !(cfg.Proto == "unix" || cfg.Proto == "unixgram") {
		writeJSONErrorKey(w, r, "settings.addrRequired", http.StatusBadRequest)
		return
	}

	if err := saveAuditForwarderConfigToDB(a.DB, cfg); err != nil {
		writeJSONErrorKey(w, r, "settings.saveFailed", http.StatusInternalServerError)
		return
	}
	// Apply immediately.
	setAuditForwarder(cfg)
	audit("settings_update", auditFields{
		"user_id": a.currentUserID(r),
		"key":     auditForwardSettingKey,
	})
	writeJSON(w, auditForwarderSettingsResponse{Config: cfg, Source: "db"})
}
