package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

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
