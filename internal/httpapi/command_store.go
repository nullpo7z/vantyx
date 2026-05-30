package httpapi

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"time"
)

// secretArgPatterns matches common credential-bearing CLI fragments so
// command_logs (and downstream audit log export) cannot accidentally
// record passwords typed at the shell (CWE-532).
//
// The replacement is conservative: rather than removing the entire
// invocation we replace just the secret portion with "***". That keeps
// the audit signal (which tool was invoked, with what flags) while
// not retaining a usable credential.
var secretArgPatterns = []*regexp.Regexp{
	// -psecret  /  -pSECRET (mysql/mysqldump short flag).
	regexp.MustCompile(`(\B-p)\S+`),
	// --password=secret  /  --password secret
	regexp.MustCompile(`(?i)(--password[=\s])\S+`),
	regexp.MustCompile(`(?i)(--token[=\s])\S+`),
	regexp.MustCompile(`(?i)(--api[-_]?key[=\s])\S+`),
	regexp.MustCompile(`(?i)(--secret[=\s])\S+`),
	// `Authorization: Bearer XYZ` typed at the shell (e.g. curl -H).
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._\-]+`),
	// FOO_PASSWORD=secret env-var preambles.
	regexp.MustCompile(`(?i)([A-Z0-9_]*(?:PASSWORD|SECRET|TOKEN|API_KEY)[A-Z0-9_]*=)\S+`),
}

// redactSecrets sanitizes a command line before persistence. The
// replacement keeps the flag / variable name so operators can still
// see what was run.
func redactSecrets(line string) string {
	for _, re := range secretArgPatterns {
		line = re.ReplaceAllString(line, "${1}***")
	}
	return line
}

type commandLogStore struct {
	db *sql.DB
}

func newCommandLogStore(db *sql.DB) *commandLogStore {
	if db == nil {
		return nil
	}
	return &commandLogStore{db: db}
}

// appendLine inserts one logical input line into command_logs after
// scrubbing well-known credential-bearing flags.
func (s *commandLogStore) appendLine(ctx context.Context, sessionID, userID, targetID, line string) {
	if s == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	line = redactSecrets(line)
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

	buf          []byte
	echo         commandLineTracker
	lastEchoLine string // preserved across PTY \\r prompt redraws
}

type commandLogStdoutWriter struct {
	rec *commandLogRecorder
}

func (w commandLogStdoutWriter) Write(p []byte) (int, error) {
	if w.rec != nil {
		w.rec.recordStdout(p)
	}
	return len(p), nil
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

func (r *commandLogRecorder) recordStdout(p []byte) {
	if r == nil || len(p) == 0 {
		return
	}
	r.echo.feed(p)
	if line := r.echo.currentLine(); line != "" {
		r.lastEchoLine = line
	}
}

// RecordInput is called from sshproxy bridge when stdin bytes are sent to target.
func (r *commandLogRecorder) RecordInput(p []byte) {
	if r == nil || len(p) == 0 {
		return
	}
	for _, b := range p {
		if b == '\r' || b == '\n' {
			r.flushLine()
			continue
		}
		if b == '\t' {
			// Tab triggers completion on the remote shell; the completed text arrives on stdout.
			continue
		}
		if b < 32 {
			continue
		}
		r.buf = append(r.buf, b)
	}
}

func (r *commandLogRecorder) flushLine() {
	stdinLine := strings.TrimSpace(string(r.buf))
	echoLine := r.echo.currentLine()
	if echoLine == "" {
		echoLine = r.lastEchoLine
	}
	line := mergeCommandLine(stdinLine, echoLine)
	r.buf = r.buf[:0]
	r.echo.reset()
	r.lastEchoLine = ""
	if line == "" || isNoiseCommandLogLine(line) {
		return
	}
	r.store.appendLine(context.Background(), r.sessionID, r.userID, r.targetID, line)
}
