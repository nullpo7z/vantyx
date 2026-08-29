package access

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// AccessRequestStatus is the lifecycle state of an access request.
type AccessRequestStatus string

const (
	RequestPending   AccessRequestStatus = "pending"
	RequestApproved  AccessRequestStatus = "approved"
	RequestDenied    AccessRequestStatus = "denied"
	RequestCancelled AccessRequestStatus = "cancelled"
)

// AccessRequest is a user's ask for membership of a group, optionally
// time-limited, that an admin approves or denies.
type AccessRequest struct {
	ID      string
	UserID  UserID
	GroupID GroupID
	Reason  string
	// DurationSeconds the requester asked for; 0 = permanent.
	DurationSeconds int64
	Status          AccessRequestStatus
	CreatedAt       time.Time
	DecidedAt       *time.Time
	DecidedBy       string
	DecisionNote    string
	// ExpiresAt is the membership expiry an approval granted (nil =
	// permanent); unset for other statuses.
	ExpiresAt *time.Time
}

// AccessRequestFilter narrows List.
type AccessRequestFilter struct {
	UserID UserID
	Status AccessRequestStatus
	Limit  int
}

var (
	ErrRequestNotFound   = errors.New("access request not found")
	ErrRequestNotPending = errors.New("access request is not pending")
	ErrRequestInvalid    = errors.New("access request id and user id must not be empty")
)

// AccessRequestStore persists access requests.
type AccessRequestStore interface {
	Create(ctx context.Context, req *AccessRequest) error
	Get(ctx context.Context, id string) (*AccessRequest, error)
	List(ctx context.Context, f AccessRequestFilter) ([]AccessRequest, error)
	// HasPending reports whether the user already has a pending request
	// for the group.
	HasPending(ctx context.Context, userID UserID, groupID GroupID) (bool, error)
	// Decide moves a pending request to approved/denied/cancelled.
	Decide(ctx context.Context, id string, status AccessRequestStatus, decidedBy, note string, expiresAt *time.Time) error
	// CountPending returns the number of pending requests (nav badge).
	CountPending(ctx context.Context) (int, error)
}

// SQLiteAccessRequestStore implements AccessRequestStore on access_requests.
type SQLiteAccessRequestStore struct{ db *sql.DB }

// NewSQLiteAccessRequestStore creates a store.
func NewSQLiteAccessRequestStore(db *sql.DB) *SQLiteAccessRequestStore {
	return &SQLiteAccessRequestStore{db: db}
}

func (s *SQLiteAccessRequestStore) Create(ctx context.Context, req *AccessRequest) error {
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(string(req.UserID)) == "" {
		return ErrRequestInvalid
	}
	if err := validateGroupID(req.GroupID); err != nil {
		return err
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	if req.Status == "" {
		req.Status = RequestPending
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO access_requests (id, user_id, group_id, reason, duration_seconds, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, req.ID, string(req.UserID), string(req.GroupID), req.Reason, req.DurationSeconds, string(req.Status), req.CreatedAt.Unix())
	return err
}

const accessRequestCols = `id, user_id, group_id, reason, duration_seconds, status, created_at, decided_at, decided_by, decision_note, expires_at`

func scanAccessRequest(sc interface{ Scan(...interface{}) error }) (*AccessRequest, error) {
	var r AccessRequest
	var uid, gid, status string
	var created int64
	var decided, expires sql.NullInt64
	var decidedBy, note sql.NullString
	if err := sc.Scan(&r.ID, &uid, &gid, &r.Reason, &r.DurationSeconds, &status, &created, &decided, &decidedBy, &note, &expires); err != nil {
		return nil, err
	}
	r.UserID, r.GroupID, r.Status = UserID(uid), GroupID(gid), AccessRequestStatus(status)
	r.CreatedAt = time.Unix(created, 0).UTC()
	if decided.Valid {
		t := time.Unix(decided.Int64, 0).UTC()
		r.DecidedAt = &t
	}
	if expires.Valid {
		t := time.Unix(expires.Int64, 0).UTC()
		r.ExpiresAt = &t
	}
	r.DecidedBy, r.DecisionNote = decidedBy.String, note.String
	return &r, nil
}

func (s *SQLiteAccessRequestStore) Get(ctx context.Context, id string) (*AccessRequest, error) {
	r, err := scanAccessRequest(s.db.QueryRowContext(ctx, `SELECT `+accessRequestCols+` FROM access_requests WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRequestNotFound
	}
	return r, err
}

func (s *SQLiteAccessRequestStore) List(ctx context.Context, f AccessRequestFilter) ([]AccessRequest, error) {
	q := `SELECT ` + accessRequestCols + ` FROM access_requests WHERE 1=1`
	var args []interface{}
	if f.UserID != "" {
		q += ` AND user_id = ?`
		args = append(args, string(f.UserID))
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, string(f.Status))
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q += ` ORDER BY created_at DESC, id LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessRequest
	for rows.Next() {
		r, err := scanAccessRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *SQLiteAccessRequestStore) HasPending(ctx context.Context, userID UserID, groupID GroupID) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_requests WHERE user_id = ? AND group_id = ? AND status = ?`, string(userID), string(groupID), string(RequestPending)).Scan(&n)
	return n > 0, err
}

func (s *SQLiteAccessRequestStore) Decide(ctx context.Context, id string, status AccessRequestStatus, decidedBy, note string, expiresAt *time.Time) error {
	var exp interface{}
	if expiresAt != nil {
		exp = expiresAt.Unix()
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE access_requests
		SET status = ?, decided_at = ?, decided_by = ?, decision_note = ?, expires_at = ?
		WHERE id = ? AND status = ?
	`, string(status), time.Now().Unix(), decidedBy, note, exp, id, string(RequestPending))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		if _, gerr := s.Get(ctx, id); errors.Is(gerr, ErrRequestNotFound) {
			return ErrRequestNotFound
		}
		return ErrRequestNotPending
	}
	return nil
}

func (s *SQLiteAccessRequestStore) CountPending(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_requests WHERE status = ?`, string(RequestPending)).Scan(&n)
	return n, err
}
