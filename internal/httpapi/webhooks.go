package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nullpo7z/vantyx/internal/access"
)

// Webhook notifications: admins register HTTP endpoints that receive
// selected audit events as JSON (generic) or Slack-style messages.
// Delivery is asynchronous and best-effort with a bounded queue, a
// short timeout, two retries and an HMAC-SHA256 signature so receivers
// can verify origin. Endpoint config lives in app_settings ("webhooks");
// VANTYX_WEBHOOK_URL / _EVENTS / _SECRET seed one endpoint when the
// table has none.

const (
	webhookSettingKey    = "webhooks"
	webhookQueueSize     = 1000
	webhookWorkers       = 4
	webhookTimeout       = 5 * time.Second
	webhookMaxRetries    = 2
	webhookMaxEndpoints  = 20
	webhookUserAgent     = "Vantyx-Webhook/1"
	webhookAllowLoopback = "VANTYX_WEBHOOK_ALLOW_RESTRICTED_HOSTS"
)

type webhookEndpoint struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret,omitempty"`
	Format  string   `json:"format"` // generic | slack
	Events  []string `json:"events"` // exact names or globs ("access_request_*", "*")
	Enabled bool     `json:"enabled"`
}

type webhookConfig struct {
	Endpoints []webhookEndpoint `json:"endpoints"`
}

// webhookStats is per-endpoint delivery state kept in memory.
type webhookStats struct {
	Sent        int64     `json:"sent"`
	Failed      int64     `json:"failed"`
	LastAt      time.Time `json:"last_at,omitempty"`
	LastStatus  int       `json:"last_status,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	LastEvent   string    `json:"last_event,omitempty"`
	DroppedFull int64     `json:"dropped_queue_full"`
}

type webhookDelivery struct {
	endpoint webhookEndpoint
	entry    AuditEntry
}

type webhookDispatcher struct {
	mu     sync.RWMutex
	cfg    webhookConfig
	stats  map[string]*webhookStats
	ch     chan webhookDelivery
	client *http.Client
	once   sync.Once
}

var globalWebhooks = &webhookDispatcher{
	stats:  map[string]*webhookStats{},
	ch:     make(chan webhookDelivery, webhookQueueSize),
	client: newWebhookClient(),
}

// newWebhookClient builds the outbound client: every connection goes
// through webhookDialContext (which re-checks the *resolved* address, so
// a DNS name pointing at loopback / link-local / the metadata service is
// refused like a literal IP), and redirects are never followed (a 3xx to
// an internal address would otherwise sidestep the check).
func newWebhookClient() *http.Client {
	return &http.Client{
		Timeout: webhookTimeout,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         webhookDialContext,
			TLSHandshakeTimeout: webhookTimeout,
			MaxIdleConns:        8,
			IdleConnTimeout:     30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("webhook: redirects are not followed")
		},
	}
}

// webhookRestrictedHostsAllowed reports the lab-only override.
func webhookRestrictedHostsAllowed() bool {
	return strings.TrimSpace(os.Getenv(webhookAllowLoopback)) == "1"
}

// webhookDialContext resolves the host itself and refuses restricted
// addresses before connecting, trying the remaining addresses in order.
func webhookDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		if strings.EqualFold(host, "localhost") && !webhookRestrictedHostsAllowed() {
			return nil, fmt.Errorf("%w: loopback", errWebhookURL)
		}
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, r := range resolved {
			ips = append(ips, r.IP)
		}
	}
	dialer := &net.Dialer{Timeout: webhookTimeout}
	var lastErr error
	for _, ip := range ips {
		if !webhookRestrictedHostsAllowed() {
			if err := access.CheckRestrictedHostIP(ip); err != nil {
				lastErr = fmt.Errorf("%w: %s resolves to %s (%s)", errWebhookURL, host, ip, err.Error())
				continue
			}
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: %s has no addresses", errWebhookURL, host)
	}
	return nil, lastErr
}

func (d *webhookDispatcher) start() {
	d.once.Do(func() {
		for i := 0; i < webhookWorkers; i++ {
			go d.loop()
		}
	})
}

func (d *webhookDispatcher) setConfig(cfg webhookConfig) {
	d.mu.Lock()
	d.cfg = cfg
	for _, ep := range cfg.Endpoints {
		if _, ok := d.stats[ep.ID]; !ok {
			d.stats[ep.ID] = &webhookStats{}
		}
	}
	d.mu.Unlock()
	d.start()
}

func (d *webhookDispatcher) config() webhookConfig {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg
}

func (d *webhookDispatcher) statsFor(id string) webhookStats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if s, ok := d.stats[id]; ok {
		return *s
	}
	return webhookStats{}
}

// webhookEventMatches implements exact / glob ("prefix_*", "*") matching.
func webhookEventMatches(patterns []string, event string) bool {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "*" || p == event {
			return true
		}
		if ok, _ := path.Match(p, event); ok {
			return true
		}
	}
	return false
}

// dispatch enqueues the entry for every enabled endpoint subscribed to it.
func (d *webhookDispatcher) dispatch(entry AuditEntry) {
	if d == nil {
		return
	}
	d.mu.RLock()
	eps := d.cfg.Endpoints
	d.mu.RUnlock()
	for _, ep := range eps {
		if !ep.Enabled || !webhookEventMatches(ep.Events, entry.Event) {
			continue
		}
		select {
		case d.ch <- webhookDelivery{endpoint: ep, entry: entry}:
		default:
			d.mu.Lock()
			if s, ok := d.stats[ep.ID]; ok {
				s.DroppedFull++
			}
			d.mu.Unlock()
		}
	}
}

func (d *webhookDispatcher) loop() {
	for del := range d.ch {
		status, err := d.deliver(context.Background(), del.endpoint, del.entry)
		d.record(del.endpoint.ID, del.entry.Event, status, err)
	}
}

func (d *webhookDispatcher) record(id, event string, status int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.stats[id]
	if !ok {
		s = &webhookStats{}
		d.stats[id] = s
	}
	s.LastAt = time.Now()
	s.LastStatus = status
	s.LastEvent = event
	if err != nil {
		s.Failed++
		s.LastError = err.Error()
	} else {
		s.Sent++
		s.LastError = ""
	}
}

// webhookPayload is the generic JSON body.
type webhookPayload struct {
	Event  string      `json:"event"`
	Time   string      `json:"time"`
	Source string      `json:"source"`
	Fields auditFields `json:"fields"`
}

func webhookBody(ep webhookEndpoint, entry AuditEntry) ([]byte, error) {
	if ep.Format == "slack" {
		var parts []string
		for k, v := range entry.Fields {
			if k == "event" {
				continue
			}
			parts = append(parts, slackEscape(k)+"="+slackEscape(fmt.Sprintf("%v", v)))
		}
		sortStrings(parts)
		text := fmt.Sprintf("*Vantyx* `%s` at %s\n%s", slackEscape(entry.Event), entry.Time.Format(time.RFC3339), strings.Join(parts, "  "))
		return json.Marshal(map[string]string{"text": text})
	}
	host, _ := os.Hostname()
	return json.Marshal(webhookPayload{
		Event:  entry.Event,
		Time:   entry.Time.Format(time.RFC3339Nano),
		Source: "vantyx@" + host,
		Fields: entry.Fields,
	})
}

func webhookSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// deliver POSTs one event with retries. Returns the last HTTP status.
func (d *webhookDispatcher) deliver(ctx context.Context, ep webhookEndpoint, entry AuditEntry) (int, error) {
	body, err := webhookBody(ep, entry)
	if err != nil {
		return 0, err
	}
	var lastErr error
	status := 0
	for attempt := 0; attempt <= webhookMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			case <-ctx.Done():
				return status, ctx.Err()
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", webhookUserAgent)
		req.Header.Set("X-Vantyx-Event", entry.Event)
		req.Header.Set("X-Vantyx-Delivery", fmt.Sprintf("%d-%d", entry.Time.UnixNano(), attempt))
		if ep.Secret != "" {
			req.Header.Set("X-Vantyx-Signature", webhookSignature(ep.Secret, body))
		}
		resp, err := d.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_ = resp.Body.Close()
		status = resp.StatusCode
		if status >= 200 && status < 300 {
			return status, nil
		}
		lastErr = fmt.Errorf("HTTP %d", status)
		if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			break // no point retrying client errors
		}
	}
	return status, lastErr
}

/* ---------------------------- persistence ---------------------------- */

func loadWebhookConfigFromDB(db *sql.DB) (webhookConfig, bool) {
	if db == nil {
		return webhookConfig{}, false
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, webhookSettingKey).Scan(&raw); err != nil {
		return webhookConfig{}, false
	}
	var cfg webhookConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return webhookConfig{}, false
	}
	return cfg, true
}

func saveWebhookConfigToDB(db *sql.DB, cfg webhookConfig) error {
	if db == nil {
		return sql.ErrConnDone
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		webhookSettingKey, string(b), time.Now().UTC())
	return err
}

func webhookConfigFromEnv() (webhookConfig, bool) {
	u := strings.TrimSpace(os.Getenv("VANTYX_WEBHOOK_URL"))
	if u == "" {
		return webhookConfig{}, false
	}
	events := strings.Fields(strings.ReplaceAll(os.Getenv("VANTYX_WEBHOOK_EVENTS"), ",", " "))
	if len(events) == 0 {
		events = []string{"login_failed", "login_rate_limited", "oidc_login_failed", "totp_reset_by_admin", "user_role_update", "access_request_*", "session_terminated_by_admin", "retention_purge"}
	}
	format := strings.ToLower(strings.TrimSpace(os.Getenv("VANTYX_WEBHOOK_FORMAT")))
	if format != "slack" {
		format = "generic"
	}
	return webhookConfig{Endpoints: []webhookEndpoint{{
		ID: "env", Name: "VANTYX_WEBHOOK_URL", URL: u, Secret: strings.TrimSpace(os.Getenv("VANTYX_WEBHOOK_SECRET")),
		Format: format, Events: events, Enabled: true,
	}}}, true
}

// initWebhooks loads the endpoint list (DB, else env) and starts the worker.
func (a *App) initWebhooks() {
	cfg, ok := loadWebhookConfigFromDB(a.DB)
	if !ok {
		cfg, _ = webhookConfigFromEnv()
	}
	globalWebhooks.setConfig(cfg)
}

/* ----------------------------- validation ---------------------------- */

var errWebhookURL = errors.New("invalid webhook url")

func validateWebhookEndpoint(ep *webhookEndpoint) error {
	ep.Name = strings.TrimSpace(ep.Name)
	ep.URL = strings.TrimSpace(ep.URL)
	ep.Secret = strings.TrimSpace(ep.Secret)
	ep.Format = strings.ToLower(strings.TrimSpace(ep.Format))
	if ep.Format == "" {
		ep.Format = "generic"
	}
	if ep.Format != "generic" && ep.Format != "slack" {
		return fmt.Errorf("%w: format", errWebhookURL)
	}
	if ep.Name == "" || len(ep.Name) > 100 {
		return errors.New("name required (max 100 chars)")
	}
	u, err := url.Parse(ep.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errWebhookURL
	}
	if !webhookRestrictedHostsAllowed() {
		host := u.Hostname()
		if ip := net.ParseIP(host); ip != nil {
			if err := access.CheckRestrictedHostIP(ip); err != nil {
				return fmt.Errorf("%w: %s", errWebhookURL, err.Error())
			}
		} else if strings.EqualFold(host, "localhost") {
			return fmt.Errorf("%w: loopback", errWebhookURL)
		}
	}
	clean := ep.Events[:0]
	for _, e := range ep.Events {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if len(e) > 64 || strings.ContainsAny(e, " \t\n\"") {
			return errors.New("invalid event pattern: " + e)
		}
		clean = append(clean, e)
	}
	ep.Events = clean
	if len(ep.Events) == 0 {
		return errors.New("at least one event pattern is required")
	}
	return nil
}

/* ------------------------------ handlers ----------------------------- */

type webhookEndpointResponse struct {
	webhookEndpoint
	HasSecret bool         `json:"has_secret"`
	Stats     webhookStats `json:"stats"`
}

func (a *App) webhookResponses() []webhookEndpointResponse {
	cfg := globalWebhooks.config()
	out := make([]webhookEndpointResponse, 0, len(cfg.Endpoints))
	for _, ep := range cfg.Endpoints {
		masked := ep
		masked.Secret = ""
		out = append(out, webhookEndpointResponse{webhookEndpoint: masked, HasSecret: ep.Secret != "", Stats: globalWebhooks.statsFor(ep.ID)})
	}
	return out
}

// handleGetWebhooks: GET /api/settings/webhooks (admin).
func (a *App) handleGetWebhooks(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	writeJSON(w, map[string]interface{}{"endpoints": a.webhookResponses()})
}

// handlePutWebhooks: PUT /api/settings/webhooks (admin) replaces the list.
// An endpoint whose secret is omitted keeps the stored one.
func (a *App) handlePutWebhooks(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var in webhookConfig
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
		return
	}
	if len(in.Endpoints) > webhookMaxEndpoints {
		writeJSONErrorKey(w, r, "settings.webhookTooMany", http.StatusBadRequest)
		return
	}
	current := globalWebhooks.config()
	existingSecret := map[string]string{}
	for _, ep := range current.Endpoints {
		existingSecret[ep.ID] = ep.Secret
	}
	seen := map[string]bool{}
	for i := range in.Endpoints {
		ep := &in.Endpoints[i]
		ep.ID = strings.TrimSpace(ep.ID)
		if ep.ID == "" {
			tok, err := randomToken()
			if err != nil {
				writeInternalError(w, err)
				return
			}
			ep.ID = tok[:12]
		}
		if seen[ep.ID] {
			writeJSONErrorKey(w, r, "settings.webhookDuplicateID", http.StatusBadRequest)
			return
		}
		seen[ep.ID] = true
		if ep.Secret == "" {
			ep.Secret = existingSecret[ep.ID]
		}
		if err := validateWebhookEndpoint(ep); err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if in.Endpoints == nil {
		in.Endpoints = []webhookEndpoint{}
	}
	if err := saveWebhookConfigToDB(a.DB, in); err != nil {
		writeJSONErrorKey(w, r, "settings.saveFailed", http.StatusInternalServerError)
		return
	}
	globalWebhooks.setConfig(in)
	audit("settings_update", auditFields{"user_id": a.currentUserID(r), "key": webhookSettingKey, "endpoints": len(in.Endpoints)})
	writeJSON(w, map[string]interface{}{"endpoints": a.webhookResponses()})
}

// handleTestWebhook: POST /api/settings/webhooks/{id}/test sends a
// synthetic event synchronously and reports the result.
func (a *App) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var target *webhookEndpoint
	for _, ep := range globalWebhooks.config().Endpoints {
		if ep.ID == id {
			e := ep
			target = &e
			break
		}
	}
	if target == nil {
		writeJSONErrorKey(w, r, "settings.webhookNotFound", http.StatusNotFound)
		return
	}
	entry := AuditEntry{Time: time.Now(), Event: "webhook_test", Fields: auditFields{"user_id": a.currentUserID(r), "endpoint": target.Name}}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	status, err := globalWebhooks.deliver(ctx, *target, entry)
	globalWebhooks.record(target.ID, entry.Event, status, err)
	resp := map[string]interface{}{"ok": err == nil, "status": status}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, resp)
}

// slackEscape applies Slack's mrkdwn control-character escaping so audit
// field values (usernames, reasons, notes) cannot inject links, mentions
// or formatting into the message.
func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
