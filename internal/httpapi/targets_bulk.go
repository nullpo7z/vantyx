package httpapi

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nullpo7z/vantyx/internal/access"
)

// Bulk target management for admins: export the inventory as CSV/JSON,
// import many targets at once (CSV/JSON, with dry-run), and probe TCP
// reachability of targets.

const (
	targetImportMaxRows   = 2000
	targetImportMaxBytes  = 16 << 20
	targetCheckTimeout    = 3 * time.Second
	targetCheckConcurrent = 16
)

// targetExportRow is one line of the export (no secrets).
type targetExportRow struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Host                  string   `json:"host"`
	Port                  uint16   `json:"port"`
	Protocol              string   `json:"protocol"`
	GroupID               string   `json:"group_id"`
	Path                  string   `json:"path,omitempty"`
	Tags                  []string `json:"tags"`
	SSHUsername           string   `json:"ssh_username,omitempty"`
	SFTPEnabled           bool     `json:"sftp_enabled"`
	FTPEnabled            bool     `json:"ftp_enabled"`
	TFTPEnabled           bool     `json:"tftp_enabled"`
	SSHHostKeyFingerprint string   `json:"ssh_host_key_fingerprint,omitempty"`
	CredentialIdentityID  string   `json:"credential_identity_id,omitempty"`
	SSHKeyID              string   `json:"ssh_key_id,omitempty"`
	HasStoredCredentials  bool     `json:"has_stored_credentials"`
}

var targetCSVHeader = []string{"name", "host", "port", "protocol", "group_id", "path", "tags", "ssh_username", "ssh_password", "sftp_enabled", "ftp_enabled", "tftp_enabled", "ssh_host_key_fingerprint", "credential_identity_id", "ssh_key_id"}

func (a *App) exportTargets(ctx context.Context) ([]targetExportRow, error) {
	ids, err := a.TargetStore.AllIDs(ctx, &access.ListOpts{Limit: 10000})
	if err != nil {
		return nil, err
	}
	targets, err := a.TargetStore.ListByIDs(ctx, ids, &access.ListOpts{Limit: 10000})
	if err != nil {
		return nil, err
	}
	out := make([]targetExportRow, 0, len(targets))
	for _, t := range targets {
		if t == nil {
			continue
		}
		groupID := ""
		if gids, err := a.AccessGroupStore.GroupIDsForTarget(ctx, t.ID); err == nil && len(gids) > 0 {
			groupID = string(gids[0])
		}
		tags, _ := a.TargetStore.TagsForTarget(ctx, t.ID)
		if tags == nil {
			tags = []string{}
		}
		out = append(out, targetExportRow{
			ID: string(t.ID), Name: t.Name, Host: t.Host, Port: t.Port, Protocol: string(t.Protocol),
			GroupID: groupID, Path: t.Path, Tags: tags, SSHUsername: t.SSHUsername,
			SFTPEnabled: t.SFTPEnabled, FTPEnabled: t.FTPEnabled, TFTPEnabled: t.TFTPEnabled,
			SSHHostKeyFingerprint: t.SSHHostKeyFingerprint,
			CredentialIdentityID:  string(t.CredentialIdentityID), SSHKeyID: string(t.SSHKeyID),
			HasStoredCredentials: t.SSHUsername != "",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GroupID != out[j].GroupID {
			return out[i].GroupID < out[j].GroupID
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// handleExportTargets: GET /api/targets/export?format=csv|json (admin).
func (a *App) handleExportTargets(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	rows, err := a.exportTargets(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	audit("targets_exported", auditFields{"user_id": a.currentUserID(r), "count": len(rows)})
	stamp := time.Now().Format("20060102-150405")
	if strings.EqualFold(r.URL.Query().Get("format"), "json") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="vantyx-targets-`+stamp+`.json"`)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"targets": rows})
		return
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write(targetCSVHeader)
	for _, t := range rows {
		_ = cw.Write([]string{
			csvSafe(t.Name), csvSafe(t.Host), strconv.Itoa(int(t.Port)), t.Protocol, csvSafe(t.GroupID), csvSafe(t.Path), csvSafe(strings.Join(t.Tags, " ")),
			csvSafe(t.SSHUsername), "", strconv.FormatBool(t.SFTPEnabled), strconv.FormatBool(t.FTPEnabled), strconv.FormatBool(t.TFTPEnabled),
			csvSafe(t.SSHHostKeyFingerprint), csvSafe(t.CredentialIdentityID), csvSafe(t.SSHKeyID),
		})
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="vantyx-targets-`+stamp+`.csv"`)
	_, _ = w.Write(buf.Bytes())
}

// csvSafe neutralizes spreadsheet formula injection: a cell starting with
// =, +, -, @ or a tab / CR would otherwise be evaluated by Excel / LibreOffice
// when the export is opened. The leading quote is stripped again by
// csvUnsafe on import, so our own exports round-trip unchanged.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// csvUnsafe reverses csvSafe for values that came from an export.
func csvUnsafe(s string) string {
	if len(s) >= 2 && s[0] == '\'' {
		switch s[1] {
		case '=', '+', '-', '@', '\t', '\r':
			return s[1:]
		}
	}
	return s
}

/* -------------------------------- import ------------------------------ */

type targetImportRow struct {
	Name                  string   `json:"name"`
	Host                  string   `json:"host"`
	Port                  uint16   `json:"port"`
	Protocol              string   `json:"protocol"`
	GroupID               string   `json:"group_id"`
	Path                  string   `json:"path"`
	Tags                  []string `json:"tags"`
	SSHUsername           string   `json:"ssh_username"`
	SSHPassword           string   `json:"ssh_password"`
	SFTPEnabled           *bool    `json:"sftp_enabled"`
	FTPEnabled            *bool    `json:"ftp_enabled"`
	TFTPEnabled           *bool    `json:"tftp_enabled"`
	SSHHostKeyFingerprint string   `json:"ssh_host_key_fingerprint"`
	CredentialIdentityID  string   `json:"credential_identity_id"`
	SSHKeyID              string   `json:"ssh_key_id"`
}

type targetImportResult struct {
	Row    int    `json:"row"`
	Name   string `json:"name"`
	Status string `json:"status"` // created | skipped | error | valid
	ID     string `json:"id,omitempty"`
	Error  string `json:"error,omitempty"`
}

func parseBoolField(s string) (*bool, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil, nil
	}
	switch s {
	case "1", "true", "yes", "on":
		b := true
		return &b, nil
	case "0", "false", "no", "off":
		b := false
		return &b, nil
	}
	return nil, fmt.Errorf("invalid boolean %q", s)
}

// parseTargetCSV reads the CSV export format (header row required;
// column order free). Tags are space- or semicolon-separated.
func parseTargetCSV(data []byte) ([]targetImportRow, error) {
	cr := csv.NewReader(bytes.NewReader(data))
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("empty file")
	}
	idx := map[string]int{}
	for i, h := range records[0] {
		idx[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	if _, ok := idx["name"]; !ok {
		return nil, errors.New("header row must contain at least name, host, group_id")
	}
	get := func(rec []string, col string) string {
		i, ok := idx[col]
		if !ok || i >= len(rec) {
			return ""
		}
		return csvUnsafe(strings.TrimSpace(rec[i]))
	}
	var out []targetImportRow
	for _, rec := range records[1:] {
		if len(rec) == 0 || (len(rec) == 1 && strings.TrimSpace(rec[0]) == "") {
			continue
		}
		row := targetImportRow{
			Name: get(rec, "name"), Host: get(rec, "host"), Protocol: get(rec, "protocol"), GroupID: get(rec, "group_id"), Path: get(rec, "path"),
			SSHUsername: get(rec, "ssh_username"), SSHPassword: get(rec, "ssh_password"),
			SSHHostKeyFingerprint: get(rec, "ssh_host_key_fingerprint"),
			CredentialIdentityID:  get(rec, "credential_identity_id"), SSHKeyID: get(rec, "ssh_key_id"),
		}
		if p := get(rec, "port"); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 65535 {
				return nil, fmt.Errorf("row %q: invalid port %q", row.Name, p)
			}
			row.Port = uint16(n)
		}
		row.Tags = strings.FieldsFunc(get(rec, "tags"), func(r rune) bool { return r == ' ' || r == ';' || r == '|' })
		var perr error
		if row.SFTPEnabled, perr = parseBoolField(get(rec, "sftp_enabled")); perr != nil {
			return nil, fmt.Errorf("row %q: %w", row.Name, perr)
		}
		if row.FTPEnabled, perr = parseBoolField(get(rec, "ftp_enabled")); perr != nil {
			return nil, fmt.Errorf("row %q: %w", row.Name, perr)
		}
		if row.TFTPEnabled, perr = parseBoolField(get(rec, "tftp_enabled")); perr != nil {
			return nil, fmt.Errorf("row %q: %w", row.Name, perr)
		}
		out = append(out, row)
	}
	return out, nil
}

// createTargetFromImport mirrors handleCreateTarget's rules for one row.
func (a *App) createTargetFromImport(ctx context.Context, row targetImportRow, dryRun bool) (string, error) {
	row.Name = strings.TrimSpace(row.Name)
	row.Host = strings.TrimSpace(row.Host)
	row.GroupID = strings.TrimSpace(row.GroupID)
	if row.Name == "" || row.Host == "" {
		return "", errors.New("name and host are required")
	}
	if row.GroupID == "" {
		return "", errors.New("group_id is required")
	}
	if _, err := a.AccessGroupStore.Get(ctx, access.GroupID(row.GroupID)); err != nil {
		return "", fmt.Errorf("group %q not found", row.GroupID)
	}
	protocol, err := parseProtocolField(row.Protocol)
	if err != nil {
		return "", fmt.Errorf("invalid protocol %q", row.Protocol)
	}
	if row.Port == 0 {
		row.Port = defaultPortFor(protocol)
	}
	sshUser, sshPass, sshKey, sshPass2 := strings.TrimSpace(row.SSHUsername), row.SSHPassword, "", ""
	if err := a.applyStoredCredentials(ctx, strings.TrimSpace(row.CredentialIdentityID), strings.TrimSpace(row.SSHKeyID), &sshUser, &sshPass, &sshKey, &sshPass2, true); err != nil {
		return "", err
	}
	// Duplicate detection: same host:port:protocol already in this group.
	if existing, err := a.AccessGroupStore.TargetIDsForGroup(ctx, access.GroupID(row.GroupID), &access.ListOpts{Limit: 10000}); err == nil {
		if ts, err := a.TargetStore.ListByIDs(ctx, existing, &access.ListOpts{Limit: 10000}); err == nil {
			for _, t := range ts {
				if t != nil && strings.EqualFold(t.Host, row.Host) && t.Port == row.Port && t.Protocol == protocol {
					return string(t.ID), errTargetImportExists
				}
			}
		}
	}
	if dryRun {
		return "", nil
	}
	sftp := protocol == access.ProtocolSSH
	ftp, tftp := false, false
	if row.SFTPEnabled != nil {
		sftp = *row.SFTPEnabled
	}
	if row.FTPEnabled != nil {
		ftp = *row.FTPEnabled
	}
	if row.TFTPEnabled != nil {
		tftp = *row.TFTPEnabled
	}
	path := strings.TrimSpace(row.Path)
	if path == "" {
		path = row.GroupID
	}
	baseID := slugID(row.Name)
	id := baseID
	for i := 0; ; i++ {
		if i > 0 {
			id = baseID + "-" + strconv.Itoa(i)
		}
		_, err := a.TargetStore.CreateWithPath(ctx, access.TargetID(id), row.Name, row.Host, row.Port, protocol, access.GroupID(row.GroupID), path, sshUser, sshPass, sshKey, sshPass2, sftp, ftp, tftp)
		if err == nil {
			break
		}
		if errors.Is(err, access.ErrTargetExists) {
			continue
		}
		return "", err
	}
	// Everything after the row exists is one unit: on failure remove the
	// half-configured target so a re-run of the import starts clean.
	finish := func() error {
		if err := a.AccessGroupStore.AddTargetToGroup(ctx, access.GroupID(row.GroupID), access.TargetID(id)); err != nil {
			return err
		}
		if row.CredentialIdentityID != "" || row.SSHKeyID != "" {
			if err := a.TargetStore.SetCredentialSource(ctx, access.TargetID(id), access.CredentialIdentityID(strings.TrimSpace(row.CredentialIdentityID)), access.SSHKeyID(strings.TrimSpace(row.SSHKeyID))); err != nil {
				return fmt.Errorf("credential source: %w", err)
			}
		}
		tags := append([]string(nil), row.Tags...)
		if gt, err := a.AccessGroupStore.TagsForGroup(ctx, access.GroupID(row.GroupID)); err == nil {
			tags = append(tags, gt...)
		}
		if len(tags) > 0 {
			if err := a.TargetStore.SetTargetTags(ctx, access.TargetID(id), dedupeStrings(tags)); err != nil {
				return fmt.Errorf("tags: %w", err)
			}
		}
		if fp := strings.TrimSpace(row.SSHHostKeyFingerprint); fp != "" {
			if err := a.TargetStore.SetSSHHostKeyFingerprint(ctx, access.TargetID(id), fp); err != nil {
				return fmt.Errorf("host key fingerprint: %w", err)
			}
		}
		return nil
	}
	if err := finish(); err != nil {
		_ = a.TargetStore.Delete(ctx, access.TargetID(id))
		return "", err
	}
	return id, nil
}

var errTargetImportExists = errors.New("target already exists in this group")

func defaultPortFor(p access.Protocol) uint16 {
	switch p {
	case access.ProtocolTelnet:
		return 23
	case access.ProtocolRDP:
		return 3389
	case access.ProtocolVNC:
		return 5900
	case access.ProtocolFTP:
		return 21
	case access.ProtocolTFTP:
		return 69
	}
	return 22
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// handleImportTargets: POST /api/targets/import (admin). Multipart
// "file" (CSV or JSON) with optional "dry_run" field, or a JSON body
// {"targets": [...], "dry_run": bool}.
func (a *App) handleImportTargets(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var rows []targetImportRow
	dryRun := false
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, targetImportMaxBytes)
		if err := r.ParseMultipartForm(4 << 20); err != nil { // #nosec G120 -- bounded by MaxBytesReader
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
		dryRun = r.FormValue("dry_run") == "1" || strings.EqualFold(r.FormValue("dry_run"), "true")
		file, hdr, err := r.FormFile("file")
		if err != nil {
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, targetImportMaxBytes))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		trimmed := bytes.TrimSpace(data)
		if strings.HasSuffix(strings.ToLower(hdr.Filename), ".json") || bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("[")) {
			rows, err = parseTargetJSON(trimmed)
		} else {
			rows, err = parseTargetCSV(data)
		}
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		var body struct {
			Targets []targetImportRow `json:"targets"`
			DryRun  bool              `json:"dry_run"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, targetImportMaxBytes)).Decode(&body); err != nil {
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
		rows, dryRun = body.Targets, body.DryRun
	}
	if len(rows) == 0 {
		writeJSONErrorKey(w, r, "targets.importEmpty", http.StatusBadRequest)
		return
	}
	if len(rows) > targetImportMaxRows {
		writeJSONErrorKey(w, r, "targets.importTooMany", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	results := make([]targetImportResult, 0, len(rows))
	created, skipped, failed := 0, 0, 0
	for i, row := range rows {
		res := targetImportResult{Row: i + 1, Name: row.Name}
		id, err := a.createTargetFromImport(ctx, row, dryRun)
		switch {
		case errors.Is(err, errTargetImportExists):
			res.Status, res.ID, res.Error = "skipped", id, err.Error()
			skipped++
		case err != nil:
			res.Status, res.Error = "error", err.Error()
			failed++
		case dryRun:
			res.Status = "valid"
		default:
			res.Status, res.ID = "created", id
			created++
		}
		results = append(results, res)
	}
	audit("targets_imported", auditFields{
		"user_id": a.currentUserID(r), "dry_run": dryRun, "rows": len(rows),
		"created": created, "skipped": skipped, "failed": failed,
	})
	writeJSON(w, map[string]interface{}{
		"dry_run": dryRun, "rows": len(rows), "created": created, "skipped": skipped, "failed": failed, "results": results,
	})
}

func parseTargetJSON(data []byte) ([]targetImportRow, error) {
	if bytes.HasPrefix(data, []byte("[")) {
		var rows []targetImportRow
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	var wrapper struct {
		Targets []targetImportRow `json:"targets"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Targets, nil
}

/* ------------------------------ reachability -------------------------- */

type targetCheckResult struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Port      uint16 `json:"port"`
	Reachable bool   `json:"reachable"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// checkTargets dials host:port over TCP for each target concurrently.
func checkTargets(ctx context.Context, targets []*access.Target) []targetCheckResult {
	out := make([]targetCheckResult, len(targets))
	sem := make(chan struct{}, targetCheckConcurrent)
	var wg sync.WaitGroup
	for i, t := range targets {
		if t == nil {
			continue
		}
		wg.Add(1)
		go func(i int, t *access.Target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := targetCheckResult{ID: string(t.ID), Host: t.Host, Port: t.Port}
			addr := net.JoinHostPort(t.Host, strconv.Itoa(int(t.Port)))
			start := time.Now()
			d := net.Dialer{Timeout: targetCheckTimeout}
			conn, err := d.DialContext(ctx, "tcp", addr)
			res.LatencyMS = time.Since(start).Milliseconds()
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Reachable = true
				_ = conn.Close()
			}
			out[i] = res
		}(i, t)
	}
	wg.Wait()
	return out
}

// handleCheckTargets: POST /api/targets/check {"ids": [...]} (admin;
// empty ids = every target, capped at 500).
func (a *App) handleCheckTargets(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
	}
	ctx := r.Context()
	var ids []access.TargetID
	if len(req.IDs) == 0 {
		all, err := a.TargetStore.AllIDs(ctx, &access.ListOpts{Limit: 500})
		if err != nil {
			writeInternalError(w, err)
			return
		}
		ids = all
	} else {
		if len(req.IDs) > 500 {
			req.IDs = req.IDs[:500]
		}
		for _, id := range req.IDs {
			ids = append(ids, access.TargetID(strings.TrimSpace(id)))
		}
	}
	targets, err := a.TargetStore.ListByIDs(ctx, ids, &access.ListOpts{Limit: 500})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// UDP-only protocols cannot be probed with a TCP dial.
	probeable := make([]*access.Target, 0, len(targets))
	skipped := []targetCheckResult{}
	for _, t := range targets {
		if t == nil {
			continue
		}
		if t.Protocol == access.ProtocolTFTP {
			skipped = append(skipped, targetCheckResult{ID: string(t.ID), Host: t.Host, Port: t.Port, Error: "udp protocol: not probed"})
			continue
		}
		probeable = append(probeable, t)
	}
	results := append(checkTargets(ctx, probeable), skipped...)
	reachable := 0
	for _, res := range results {
		if res.Reachable {
			reachable++
		}
	}
	audit("targets_checked", auditFields{"user_id": a.currentUserID(r), "count": len(results), "reachable": reachable})
	writeJSON(w, map[string]interface{}{"results": results, "checked_at": time.Now().Format(time.RFC3339)})
}
