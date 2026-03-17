package httpapi

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type commandLogStore struct {
	db *sql.DB
}

func newCommandLogStore(db *sql.DB) *commandLogStore {
	if db == nil {
		return nil
	}
	return &commandLogStore{db: db}
}

// appendLine inserts one logical input line into command_logs.
func (s *commandLogStore) appendLine(ctx context.Context, sessionID, userID, targetID, line string) {
	if s == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO command_logs (session_id,user_id,target_id,time,line_text) VALUES (?,?,?,?,?)`,
		sessionID, userID, targetID, time.Now().UTC(), line,
	)
}

type commandLogRecorder struct {
	store     *commandLogStore
	sessionID string
	userID    string
	targetID  string

	buf []byte
}

func newCommandLogRecorder(store *commandLogStore, sessionID, userID, targetID string) *commandLogRecorder {
	if store == nil {
		return nil
	}
	return &commandLogRecorder{
		store:     store,
		sessionID: sessionID,
		userID:    userID,
		targetID:  targetID,
		buf:       make([]byte, 0, 256),
	}
}

// RecordInput is called from sshproxy bridge when stdin bytes are sent to target.
func (r *commandLogRecorder) RecordInput(p []byte) {
	if r == nil || len(p) == 0 {
		return
	}
	for _, b := range p {
		if b == '\r' || b == '\n' {
			if len(r.buf) > 0 {
				line := string(r.buf)
				r.store.appendLine(context.Background(), r.sessionID, r.userID, r.targetID, line)
				r.buf = r.buf[:0]
			}
			continue
		}
		r.buf = append(r.buf, b)
	}
}

