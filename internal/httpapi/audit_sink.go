package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nullpo7z/vantyx/internal/logging"
)

type auditSink struct {
	db *sql.DB

	mu   sync.Mutex
	file *os.File

	forwarder *auditForwarder
}

func newAuditSink(db *sql.DB, filePath string, forwarder *auditForwarder) (*auditSink, error) {
	s := &auditSink{db: db, forwarder: forwarder}
	if filePath == "" {
		return s, nil
	}
	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	s.file = f
	return s, nil
}

func (s *auditSink) write(entry AuditEntry) {
	if s == nil {
		return
	}

	// Insert into DB (best-effort).
	if s.db != nil {
		userID := ""
		method := ""
		path := ""
		status := 0
		remote := ""
		duration := int64(0)
		if v, ok := entry.Fields["user_id"].(string); ok {
			userID = strings.TrimSpace(v)
		}
		if v, ok := entry.Fields["method"].(string); ok {
			method = v
		}
		if v, ok := entry.Fields["path"].(string); ok {
			path = v
		}
		switch v := entry.Fields["status"].(type) {
		case int:
			status = v
		case int64:
			status = int(v)
		case float64:
			status = int(v)
		}
		if v, ok := entry.Fields["remote"].(string); ok {
			remote = v
		}
		switch v := entry.Fields["duration_ms"].(type) {
		case int:
			duration = int64(v)
		case int64:
			duration = v
		case float64:
			duration = int64(v)
		}
		fieldsJSON, _ := json.Marshal(entry.Fields)
		_, _ = s.db.Exec(
			`INSERT INTO audit_logs(time,event,user_id,method,path,status,remote,duration_ms,fields_json)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			entry.Time.UTC(), entry.Event, userID, method, path, status, remote, duration, string(fieldsJSON),
		)
	}

	// Append JSONL to file (best-effort).
	if s.file != nil {
		line, err := json.Marshal(entry)
		if err == nil {
			s.mu.Lock()
			_, _ = s.file.Write(append(line, '\n'))
			s.mu.Unlock()
		}
	}

	// Forward to external SIEM (best-effort).
	s.mu.Lock()
	fwd := s.forwarder
	s.mu.Unlock()
	if fwd != nil {
		if line, err := json.Marshal(entry); err == nil {
			fwd.sendJSONL(line)
		}
	}
}

var (
	globalAuditSinkMu sync.RWMutex
	globalAuditSink   *auditSink
)

func setGlobalAuditSink(s *auditSink) {
	globalAuditSinkMu.Lock()
	defer globalAuditSinkMu.Unlock()
	globalAuditSink = s
}

func getGlobalAuditSink() *auditSink {
	globalAuditSinkMu.RLock()
	defer globalAuditSinkMu.RUnlock()
	return globalAuditSink
}

func initAuditSink(db *sql.DB) {
	// Persist to a file when configured. In production set
	// VANTYX_AUDIT_LOG_FILE (for example /app/data/audit.log). Tests do
	// not get a default file path so tempdir cleanup stays clean.
	path := os.Getenv("VANTYX_AUDIT_LOG_FILE")
	if path == "" {
		if !strings.HasSuffix(os.Args[0], ".test") {
			path = "data/audit.log"
		}
	}
	// Prefer admin-configured settings; fall back to env.
	cfg, hasCfg := loadAuditForwarderConfigFromDB(db)
	var fwd *auditForwarder
	if hasCfg {
		fwd = newAuditForwarder(cfg)
	} else {
		fwd = newAuditForwarderFromEnv()
	}

	s, err := newAuditSink(db, path, fwd)
	if err != nil {
		// If file open fails, still keep the DB sink.
		s = &auditSink{db: db}
	}
	setGlobalAuditSink(s)
	logging.RegisterAuditSink(loggingAuditAdapter{})
}

// loggingAuditAdapter bridges [logging.AuditSink] into the legacy
// auditSink type so that any caller that uses [logging.Audit] ends up in
// the DB / file / forwarder pipeline owned by the HTTP API.
type loggingAuditAdapter struct{}

// Write implements [logging.AuditSink].
func (loggingAuditAdapter) Write(_ context.Context, evt logging.AuditEvent) {
	sink := getGlobalAuditSink()
	if sink == nil {
		return
	}
	sink.write(AuditEntry{
		Time:   time.Now(), // server zone (VANTYX_TIMEZONE); the DB insert above converts to UTC
		Event:  evt.Event,
		Fields: auditFields(evt.Fields),
	})
}

func setAuditForwarder(cfg auditForwarderConfig) {
	s := getGlobalAuditSink()
	if s == nil {
		return
	}
	newFwd := newAuditForwarder(cfg)
	s.mu.Lock()
	old := s.forwarder
	s.forwarder = newFwd
	s.mu.Unlock()
	if old != nil {
		old.close()
	}
}

// closeAuditSink is used in tests; server doesn't currently call this on shutdown.
func closeAuditSink() error {
	s := getGlobalAuditSink()
	if s == nil {
		return nil
	}
	// Prevent any late writers from using a closing sink.
	setGlobalAuditSink(nil)
	if s.forwarder != nil {
		s.forwarder.close()
		s.forwarder = nil
	}
	if s.file == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.file.Close()
	s.file = nil
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}
