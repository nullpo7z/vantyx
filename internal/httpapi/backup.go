package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	dbsqlite "github.com/nullpo7z/vantyx/internal/db/sqlite"
)

// Backup / restore of the SQLite database.
//
//   - Backups are consistent online snapshots made with `VACUUM INTO`,
//     verified with PRAGMA integrity_check, stored under
//     <dbdir>/backups (VANTYX_BACKUP_DIR) as vantyx-YYYYMMDD-HHMMSS.db,
//     pruned to VANTYX_BACKUP_KEEP files, optionally on a schedule
//     (VANTYX_BACKUP_INTERVAL). Admins can create, list, download and
//     delete them.
//   - A restore (from a stored backup or an uploaded file) is validated
//     and then *staged* as <db>.restore-pending; it is applied on the
//     next start-up, when no connection is open, after moving the
//     current database aside as <db>.pre-restore-<timestamp>. Swapping a
//     live SQLite file under open connections is not safe, so the
//     restart is deliberate.
//
// Recordings, TLS certificates and .env are outside the database and are
// not part of a backup (documented in docs/configuration.md).

const (
	backupEnvDir      = "VANTYX_BACKUP_DIR"
	backupEnvKeep     = "VANTYX_BACKUP_KEEP"
	backupEnvInterval = "VANTYX_BACKUP_INTERVAL"
	backupDefaultKeep = 14
	backupMaxUpload   = 2 << 30 // 2 GiB
	restorePendingExt = ".restore-pending"
)

var backupNameRe = regexp.MustCompile(`^vantyx-\d{8}-\d{6}(-[a-z0-9]{1,16})?\.db$`)

type backupInfo struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	SHA256    string    `json:"sha256,omitempty"`
}

type backupPolicy struct {
	Dir      string        `json:"dir"`
	Keep     int           `json:"keep"`
	Interval time.Duration `json:"-"`
}

type backupState struct {
	mu      sync.Mutex
	policy  backupPolicy
	dbPath  string
	running bool
	last    *backupInfo
	lastErr string
}

func backupPolicyFromEnv(dbPath string) backupPolicy {
	dir := strings.TrimSpace(os.Getenv(backupEnvDir))
	if dir == "" {
		dir = filepath.Join(filepath.Dir(dbPath), "backups")
	}
	keep := backupDefaultKeep
	if v := strings.TrimSpace(os.Getenv(backupEnvKeep)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 1000 {
			keep = n
		}
	}
	var interval time.Duration
	if v := strings.TrimSpace(os.Getenv(backupEnvInterval)); v != "" && v != "0" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Hour {
			interval = d
		}
	}
	return backupPolicy{Dir: dir, Keep: keep, Interval: interval}
}

func (a *App) initBackups(dbPath string) {
	a.backups = &backupState{policy: backupPolicyFromEnv(dbPath), dbPath: dbPath}
	if a.backups.policy.Interval > 0 && dbPath != "" && dbPath != ":memory:" {
		go func() {
			t := time.NewTicker(a.backups.policy.Interval)
			defer t.Stop()
			for range t.C {
				if _, err := a.createBackup(context.Background(), "scheduled"); err != nil {
					audit("backup_failed", auditFields{"trigger": "scheduled", "error": err.Error()})
				}
			}
		}()
	}
}

func backupFileName(suffix string) string {
	name := "vantyx-" + time.Now().UTC().Format("20060102-150405")
	if suffix != "" {
		name += "-" + suffix
	}
	return name + ".db"
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- path built from the backup dir + validated name
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifySQLiteFile opens the file read-only and runs integrity_check plus
// a sanity query for the users table.
func verifySQLiteFile(path string) error {
	f, err := os.Open(path) // #nosec G304
	if err != nil {
		return err
	}
	hdr := make([]byte, 16)
	_, rerr := io.ReadFull(f, hdr)
	_ = f.Close()
	if rerr != nil || string(hdr[:15]) != "SQLite format 3" {
		return errors.New("not an SQLite database")
	}
	db, err := dbsqlite.Open(dbsqlite.Config{Path: path, MaxOpenConns: 1})
	if err != nil {
		return err
	}
	defer db.Close()
	var res string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return errors.New("integrity_check: " + res)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&n); err != nil || n != 1 {
		return errors.New("not a Vantyx database (users table missing)")
	}
	return nil
}

// scrubSnapshot removes data that must not leave the live database in a
// copy: the sessions table (a snapshot with live session IDs would let
// anyone holding the file hijack those sessions, and restoring an old
// snapshot would resurrect sessions revoked since). Runs on every backup
// and on every staged restore file.
func scrubSnapshot(path string) error {
	db, err := dbsqlite.Open(dbsqlite.Config{Path: path, MaxOpenConns: 1})
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	_, err = db.Exec(`DELETE FROM sessions`)
	return err
}

// createBackup writes a verified snapshot and prunes old ones.
func (a *App) createBackup(ctx context.Context, trigger string) (*backupInfo, error) {
	if a.DB == nil || a.backups == nil || a.backups.dbPath == "" || a.backups.dbPath == ":memory:" {
		return nil, errors.New("backups are unavailable for an in-memory database")
	}
	st := a.backups
	st.mu.Lock()
	if st.running {
		st.mu.Unlock()
		return nil, errors.New("a backup is already running")
	}
	st.running = true
	dir := st.policy.Dir
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		st.running = false
		st.mu.Unlock()
	}()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	name := backupFileName("")
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); err == nil {
		name = backupFileName(strconv.FormatInt(time.Now().UnixNano()%1000, 10))
		dest = filepath.Join(dir, name)
	}
	if _, err := a.DB.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		_ = os.Remove(dest)
		return nil, fmt.Errorf("vacuum into: %w", err)
	}
	if err := scrubSnapshot(dest); err != nil {
		_ = os.Remove(dest)
		return nil, fmt.Errorf("scrub backup: %w", err)
	}
	if err := verifySQLiteFile(dest); err != nil {
		_ = os.Remove(dest)
		return nil, fmt.Errorf("verify backup: %w", err)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		return nil, err
	}
	sum, _ := fileSHA256(dest)
	info := &backupInfo{Name: name, SizeBytes: fi.Size(), CreatedAt: fi.ModTime(), SHA256: sum}
	pruned := a.pruneBackups()
	st.mu.Lock()
	st.last = info
	st.lastErr = ""
	st.mu.Unlock()
	audit("backup_created", auditFields{"trigger": trigger, "name": name, "size_bytes": info.SizeBytes, "sha256": sum, "pruned": pruned})
	return info, nil
}

func (a *App) listBackups() ([]backupInfo, error) {
	if a.backups == nil {
		return nil, nil
	}
	entries, err := os.ReadDir(a.backups.policy.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []backupInfo{}, nil
		}
		return nil, err
	}
	out := make([]backupInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !backupNameRe.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, backupInfo{Name: e.Name(), SizeBytes: fi.Size(), CreatedAt: fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// pruneBackups keeps the newest policy.Keep files.
func (a *App) pruneBackups() int {
	list, err := a.listBackups()
	if err != nil || a.backups == nil {
		return 0
	}
	keep := a.backups.policy.Keep
	pruned := 0
	for i := keep; i < len(list); i++ {
		if os.Remove(filepath.Join(a.backups.policy.Dir, list[i].Name)) == nil {
			pruned++
		}
	}
	return pruned
}

func (a *App) backupPath(name string) (string, bool) {
	if a.backups == nil || !backupNameRe.MatchString(name) {
		return "", false
	}
	return filepath.Join(a.backups.policy.Dir, name), true
}

func (a *App) pendingRestorePath() string {
	if a.backups == nil || a.backups.dbPath == "" {
		return ""
	}
	return a.backups.dbPath + restorePendingExt
}

// stageRestore validates src and copies it to <db>.restore-pending.
func (a *App) stageRestore(src, adminID, origin string) error {
	if err := verifySQLiteFile(src); err != nil {
		return err
	}
	pending := a.pendingRestorePath()
	if pending == "" {
		return errors.New("restore unavailable for an in-memory database")
	}
	in, err := os.Open(src) // #nosec G304
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := pending + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := scrubSnapshot(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, pending); err != nil {
		return err
	}
	sum, _ := fileSHA256(pending)
	audit("restore_staged", auditFields{"user_id": adminID, "origin": origin, "sha256": sum})
	return nil
}

// ApplyPendingRestore is called before the database is opened: if a
// staged restore exists, the current database (and its WAL/SHM) is moved
// aside and the staged file takes its place. Returns the path the old
// database was moved to when a restore was applied.
func ApplyPendingRestore(dbPath string) (movedTo string, applied bool, err error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "", false, nil
	}
	pending := dbPath + restorePendingExt
	if _, err := os.Stat(pending); err != nil {
		return "", false, nil
	}
	if err := verifySQLiteFile(pending); err != nil {
		_ = os.Rename(pending, pending+".rejected")
		return "", false, fmt.Errorf("staged restore rejected: %w", err)
	}
	// Belt and braces: never let a staged file bring sessions back.
	if err := scrubSnapshot(pending); err != nil {
		_ = os.Rename(pending, pending+".rejected")
		return "", false, fmt.Errorf("staged restore rejected: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	movedTo = dbPath + ".pre-restore-" + stamp
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, movedTo); err != nil {
			return "", false, err
		}
	} else {
		movedTo = ""
	}
	for _, ext := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + ext)
	}
	if err := os.Rename(pending, dbPath); err != nil {
		return movedTo, false, err
	}
	return movedTo, true, nil
}

/* ------------------------------ handlers ----------------------------- */

func (a *App) backupPolicyResponse() map[string]interface{} {
	p := a.backups.policy
	st := a.backups
	st.mu.Lock()
	last, lastErr := st.last, st.lastErr
	st.mu.Unlock()
	pending := false
	if pp := a.pendingRestorePath(); pp != "" {
		if _, err := os.Stat(pp); err == nil {
			pending = true
		}
	}
	return map[string]interface{}{
		"dir":             p.Dir,
		"keep":            p.Keep,
		"interval":        durationOrEmpty(p.Interval),
		"last":            last,
		"last_error":      lastErr,
		"restore_pending": pending,
	}
}

// handleListBackups: GET /api/settings/backups (admin).
func (a *App) handleListBackups(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if a.backups == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}
	list, err := a.listBackups()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"policy": a.backupPolicyResponse(), "backups": list})
}

// handleCreateBackup: POST /api/settings/backups (admin).
func (a *App) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	info, err := a.createBackup(r.Context(), "manual:"+a.currentUserID(r))
	if err != nil {
		if a.backups != nil {
			a.backups.mu.Lock()
			a.backups.lastErr = err.Error()
			a.backups.mu.Unlock()
		}
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusCreated, info)
}

// handleDownloadBackup: GET /api/settings/backups/{name} (admin).
func (a *App) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	name := chi.URLParam(r, "name")
	p, ok := a.backupPath(name)
	if !ok {
		writeJSONErrorKey(w, r, "settings.backupNotFound", http.StatusNotFound)
		return
	}
	f, err := os.Open(p) // #nosec G304 -- validated name inside the backup dir
	if err != nil {
		writeJSONErrorKey(w, r, "settings.backupNotFound", http.StatusNotFound)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	audit("backup_downloaded", auditFields{"user_id": a.currentUserID(r), "name": name})
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// handleDeleteBackup: DELETE /api/settings/backups/{name} (admin).
func (a *App) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	name := chi.URLParam(r, "name")
	p, ok := a.backupPath(name)
	if !ok {
		writeJSONErrorKey(w, r, "settings.backupNotFound", http.StatusNotFound)
		return
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			writeJSONErrorKey(w, r, "settings.backupNotFound", http.StatusNotFound)
			return
		}
		writeInternalError(w, err)
		return
	}
	audit("backup_deleted", auditFields{"user_id": a.currentUserID(r), "name": name})
	w.WriteHeader(http.StatusNoContent)
}

// handleStageRestore: POST /api/settings/backups/restore (admin). Either
// JSON {"name": "<stored backup>"} or multipart form field "file".
func (a *App) handleStageRestore(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if a.backups == nil {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}
	adminID := a.currentUserID(r)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, backupMaxUpload)
		if err := r.ParseMultipartForm(8 << 20); err != nil { // #nosec G120 -- bounded by MaxBytesReader
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
		defer file.Close()
		tmp, err := os.CreateTemp(filepath.Dir(a.backups.dbPath), "upload-*.db")
		if err != nil {
			writeInternalError(w, err)
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := io.Copy(tmp, file); err != nil {
			_ = tmp.Close()
			writeInternalError(w, err)
			return
		}
		_ = tmp.Close()
		if err := a.stageRestore(tmpPath, adminID, "upload:"+hdr.Filename); err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONErrorKey(w, r, "common.invalidRequestBody", http.StatusBadRequest)
			return
		}
		p, ok := a.backupPath(strings.TrimSpace(req.Name))
		if !ok {
			writeJSONErrorKey(w, r, "settings.backupNotFound", http.StatusNotFound)
			return
		}
		if _, err := os.Stat(p); err != nil {
			writeJSONErrorKey(w, r, "settings.backupNotFound", http.StatusNotFound)
			return
		}
		if err := a.stageRestore(p, adminID, "backup:"+req.Name); err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	writeJSON(w, map[string]interface{}{"restore_pending": true, "applies_on": "restart"})
}

// handleCancelRestore: DELETE /api/settings/backups/restore (admin).
func (a *App) handleCancelRestore(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	pending := a.pendingRestorePath()
	if pending == "" {
		writeJSONErrorKey(w, r, "common.serviceUnavailable", http.StatusServiceUnavailable)
		return
	}
	if err := os.Remove(pending); err != nil && !os.IsNotExist(err) {
		writeInternalError(w, err)
		return
	}
	audit("restore_cancelled", auditFields{"user_id": a.currentUserID(r)})
	w.WriteHeader(http.StatusNoContent)
}

// closeQuietly is a tiny helper for deferred closes in this file.
func closeQuietly(c io.Closer) { _ = c.Close() }

var _ = closeQuietly
var _ = sql.ErrNoRows
