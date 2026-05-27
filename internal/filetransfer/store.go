package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Store persists file transfer jobs in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store. The caller must have already called sqlite.Migrate(db).
func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

// JobRecord is the persisted shape of a job. It mirrors the public Job fields
// that need to survive a process restart.
type JobRecord struct {
	ID         string
	UserID     string
	TargetID   string
	TargetName string
	Backend    Backend
	Direction  Direction
	RemotePath string
	FileName   string
	State      State
	Progress   int64
	Total      int64
	Error      string
	TempPath   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const storeQueryTimeout = 5 * time.Second

// Insert creates a new row. The id is expected to be already populated.
func (s *Store) Insert(ctx context.Context, rec JobRecord) error {
	if s == nil {
		return errors.New("filetransfer: nil store")
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO file_transfer_jobs
			(id, user_id, target_id, target_name, backend, direction, remote_path, file_name,
			 state, progress, total, error, temp_path, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		rec.ID, rec.UserID, rec.TargetID, rec.TargetName, string(rec.Backend), string(rec.Direction),
		rec.RemotePath, rec.FileName, string(rec.State), rec.Progress, rec.Total, rec.Error, rec.TempPath,
		rec.CreatedAt.UTC(), rec.UpdatedAt.UTC(),
	)
	return err
}

// UpdateState writes state + error message and bumps updated_at.
func (s *Store) UpdateState(ctx context.Context, id string, state State, errMsg string, updatedAt time.Time) error {
	if s == nil {
		return errors.New("filetransfer: nil store")
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_transfer_jobs SET state = ?, error = ?, updated_at = ? WHERE id = ?`,
		string(state), errMsg, updatedAt.UTC(), id,
	)
	return err
}

// UpdateProgress writes progress (and optionally total when total > 0) and bumps updated_at.
func (s *Store) UpdateProgress(ctx context.Context, id string, progress, total int64, updatedAt time.Time) error {
	if s == nil {
		return errors.New("filetransfer: nil store")
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	if total > 0 {
		_, err := s.db.ExecContext(ctx,
			`UPDATE file_transfer_jobs SET progress = ?, total = ?, updated_at = ? WHERE id = ?`,
			progress, total, updatedAt.UTC(), id,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_transfer_jobs SET progress = ?, updated_at = ? WHERE id = ?`,
		progress, updatedAt.UTC(), id,
	)
	return err
}

// UpdateTempPath stores the staging file path on disk.
func (s *Store) UpdateTempPath(ctx context.Context, id, tempPath string) error {
	if s == nil {
		return errors.New("filetransfer: nil store")
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`UPDATE file_transfer_jobs SET temp_path = ? WHERE id = ?`,
		tempPath, id,
	)
	return err
}

// Get loads one row by id. Returns ErrNotFound if missing.
func (s *Store) Get(ctx context.Context, id string) (JobRecord, error) {
	if s == nil {
		return JobRecord{}, errors.New("filetransfer: nil store")
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	row := s.db.QueryRowContext(ctx, selectColumns+` FROM file_transfer_jobs WHERE id = ?`, id)
	rec, err := scanJobRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return JobRecord{}, ErrNotFound
	}
	return rec, err
}

// ListByUser returns jobs for userID, newest updated first.
func (s *Store) ListByUser(ctx context.Context, userID string, limit int) ([]JobRecord, error) {
	if s == nil {
		return nil, errors.New("filetransfer: nil store")
	}
	if limit <= 0 {
		limit = 500
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		selectColumns+` FROM file_transfer_jobs WHERE user_id = ? ORDER BY updated_at DESC, id DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobRecord
	for rows.Next() {
		rec, err := scanJobRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ListFilter parameterises ListByUserFiltered. Zero-valued fields are treated as
// "no constraint" except for UserID which is required.
type ListFilter struct {
	UserID       string
	Query        string    // LIKE match against file_name OR remote_path OR target_name
	TargetID     string    // exact match
	Direction    Direction // "" = any
	Backend      Backend   // "" = any
	States       []State   // OR of given states; empty = any
	From, To     time.Time // updated_at range (zero = unbounded on that side)
	AfterUpdated time.Time // composite cursor; zero = first page
	AfterID      string
	Limit        int // caller may pass pageLimit+1 to detect more pages
}

// ListByUserFiltered returns jobs matching filter, newest updated_at first.
// The composite cursor (AfterUpdated, AfterID) lets callers paginate through
// jobs that share the same updated_at timestamp.
func (s *Store) ListByUserFiltered(ctx context.Context, filter ListFilter) ([]JobRecord, error) {
	if s == nil {
		return nil, errors.New("filetransfer: nil store")
	}
	if strings.TrimSpace(filter.UserID) == "" {
		return nil, errors.New("filetransfer: ListFilter.UserID is required")
	}
	sqlStr, args := buildFileTransferJobQuery(filter)
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobRecord
	for rows.Next() {
		rec, err := scanJobRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// buildFileTransferJobQuery assembles the SELECT used by ListByUserFiltered.
// It mirrors the style of buildCommandLogQuery / buildAuditLogQuery.
func buildFileTransferJobQuery(filter ListFilter) (string, []interface{}) {
	var conds []string
	var args []interface{}

	conds = append(conds, "user_id = ?")
	args = append(args, filter.UserID)

	if !filter.From.IsZero() {
		conds = append(conds, "updated_at >= ?")
		args = append(args, filter.From.UTC())
	}
	if !filter.To.IsZero() {
		conds = append(conds, "updated_at < ?")
		args = append(args, filter.To.UTC())
	}
	if filter.TargetID != "" {
		conds = append(conds, "target_id = ?")
		args = append(args, filter.TargetID)
	}
	if filter.Direction != "" {
		conds = append(conds, "direction = ?")
		args = append(args, string(filter.Direction))
	}
	if filter.Backend != "" {
		conds = append(conds, "backend = ?")
		args = append(args, string(filter.Backend))
	}
	if len(filter.States) > 0 {
		placeholders := make([]string, len(filter.States))
		for i, st := range filter.States {
			placeholders[i] = "?"
			args = append(args, string(st))
		}
		conds = append(conds, "state IN ("+strings.Join(placeholders, ",")+")")
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		// Escape LIKE wildcards so attacker-controlled fragments cannot
		// smuggle pattern operators or force a broad scan (CWE-400).
		pat := "%" + escapeLikeOperand(q) + "%"
		conds = append(conds, `(file_name LIKE ? ESCAPE '\' OR remote_path LIKE ? ESCAPE '\' OR target_name LIKE ? ESCAPE '\')`)
		args = append(args, pat, pat, pat)
	}
	if !filter.AfterUpdated.IsZero() {
		conds = append(conds, "(updated_at < ? OR (updated_at = ? AND id < ?))")
		args = append(args, filter.AfterUpdated.UTC(), filter.AfterUpdated.UTC(), filter.AfterID)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	sqlStr := selectColumns + " FROM file_transfer_jobs WHERE " + strings.Join(conds, " AND ") +
		" ORDER BY updated_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	return sqlStr, args
}

// escapeLikeOperand escapes LIKE wildcards (`%`, `_`) and the escape
// byte itself so caller-controlled substrings can be passed through
// `... LIKE ? ESCAPE '\'` safely.
func escapeLikeOperand(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Delete removes a single row. Returns the previous record and whether anything was deleted.
// userID is required: a mismatch returns ErrForbidden so callers don't leak existence.
func (s *Store) Delete(ctx context.Context, id, userID string) (JobRecord, bool, error) {
	if s == nil {
		return JobRecord{}, false, errors.New("filetransfer: nil store")
	}
	rec, err := s.Get(ctx, id)
	if err != nil {
		return JobRecord{}, false, err
	}
	if rec.UserID != userID {
		return JobRecord{}, false, ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `DELETE FROM file_transfer_jobs WHERE id = ?`, id)
	if err != nil {
		return JobRecord{}, false, err
	}
	n, _ := res.RowsAffected()
	return rec, n > 0, nil
}

// MarkOrphansFailed transitions any jobs in receiving/running state to failed.
// Intended to be called on startup to reap jobs whose owning goroutines died
// when the previous server process exited.
func (s *Store) MarkOrphansFailed(ctx context.Context, now time.Time, message string) (int64, error) {
	if s == nil {
		return 0, errors.New("filetransfer: nil store")
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_jobs
		SET state = ?, error = ?, updated_at = ?
		WHERE state IN (?, ?)
	`,
		string(StateFailed), message, now.UTC(),
		string(StateReceiving), string(StateRunning),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// TrimUserHistory keeps at most maxRows terminal-state rows per user, deleting the
// oldest ones (by updated_at). Active jobs (receiving/running) are never removed.
func (s *Store) TrimUserHistory(ctx context.Context, userID string, maxRows int) (int64, error) {
	if s == nil {
		return 0, errors.New("filetransfer: nil store")
	}
	if maxRows <= 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, storeQueryTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM file_transfer_jobs
		WHERE user_id = ?
		  AND state IN (?, ?, ?)
		  AND id NOT IN (
			SELECT id FROM file_transfer_jobs
			WHERE user_id = ?
			  AND state IN (?, ?, ?)
			ORDER BY updated_at DESC, id DESC
			LIMIT ?
		  )
	`,
		userID, string(StateCompleted), string(StateFailed), string(StateCancelled),
		userID, string(StateCompleted), string(StateFailed), string(StateCancelled),
		maxRows,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// scannable is the common interface implemented by *sql.Row and *sql.Rows.
type scannable interface {
	Scan(dest ...interface{}) error
}

const selectColumns = `SELECT id, user_id, target_id, target_name, backend, direction,
	remote_path, file_name, state, progress, total, error, temp_path, created_at, updated_at`

func scanJobRecord(s scannable) (JobRecord, error) {
	var rec JobRecord
	var backend, direction, state string
	if err := s.Scan(
		&rec.ID, &rec.UserID, &rec.TargetID, &rec.TargetName, &backend, &direction,
		&rec.RemotePath, &rec.FileName, &state, &rec.Progress, &rec.Total, &rec.Error,
		&rec.TempPath, &rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		return JobRecord{}, err
	}
	rec.Backend = Backend(backend)
	rec.Direction = Direction(direction)
	rec.State = State(state)
	return rec, nil
}
