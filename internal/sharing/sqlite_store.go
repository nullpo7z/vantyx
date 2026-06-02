package sharing

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// SQLiteStore persists invitations in the bundled SQLite database. It
// matches the schema added by internal/db/sqlite/migrate.go's
// session_invitations table.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore wraps db with the Store interface.
func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

// Create inserts a fresh invitation row.
func (s *SQLiteStore) Create(ctx context.Context, inv Invitation, tokenHash string) error {
	if s == nil || s.db == nil {
		return errors.New("invitation store not configured")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO session_invitations
		(id, token_hash, session_id, session_kind, target_id, owner_user_id, invitee_user_id,
		 invite_group_id, invite_tag, mode, max_uses, use_count, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID,
		tokenHash,
		inv.SessionID,
		string(inv.SessionKind),
		inv.TargetID,
		inv.OwnerUserID,
		nullableUser(inv.InviteeUserID),
		nullableUser(inv.InviteGroupID),
		nullableUser(inv.InviteTag),
		string(inv.Mode),
		nullableMaxUses(inv.MaxUses),
		inv.UseCount,
		inv.ExpiresAt.Unix(),
		inv.CreatedAt.Unix(),
	)
	return err
}

// GetByID loads the invitation with id. Returns ErrInvitationNotFound
// when missing.
func (s *SQLiteStore) GetByID(ctx context.Context, id string) (Invitation, string, error) {
	if s == nil || s.db == nil {
		return Invitation{}, "", errors.New("invitation store not configured")
	}
	row := s.db.QueryRowContext(ctx, selectInvitationCols+` WHERE id = ?`, id)
	return scanInvitation(row)
}

// GetByTokenHash loads the invitation whose token_hash matches.
func (s *SQLiteStore) GetByTokenHash(ctx context.Context, tokenHash string) (Invitation, error) {
	if s == nil || s.db == nil {
		return Invitation{}, errors.New("invitation store not configured")
	}
	row := s.db.QueryRowContext(ctx, selectInvitationCols+` WHERE token_hash = ?`, tokenHash)
	inv, _, err := scanInvitation(row)
	return inv, err
}

// ListPendingForInvitee returns active named invitations for inviteeUserID.
func (s *SQLiteStore) ListPendingForInvitee(ctx context.Context, inviteeUserID string, now time.Time) ([]Invitation, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invitation store not configured")
	}
	inviteeUserID = strings.TrimSpace(inviteeUserID)
	if inviteeUserID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, selectInvitationCols+`
		WHERE invitee_user_id = ?
		  AND used_at IS NULL
		  AND revoked_at IS NULL
		  AND expires_at > ?
		ORDER BY created_at DESC`, inviteeUserID, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		inv, _, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// ListBySession returns invitations attached to sessionID, newest first.
func (s *SQLiteStore) ListBySession(ctx context.Context, sessionID string) ([]Invitation, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("invitation store not configured")
	}
	rows, err := s.db.QueryContext(ctx, selectInvitationCols+` WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		inv, _, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RecordUse increments use_count and sets used_at when the invitation
// is fully consumed. The UPDATE is conditional so concurrent joins
// cannot exceed max_uses (TOCTOU safe).
func (s *SQLiteStore) RecordUse(ctx context.Context, id, consumerUserID string, when time.Time) error {
	if s == nil || s.db == nil {
		return errors.New("invitation store not configured")
	}
	whenUnix := when.Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE session_invitations
		 SET use_count = use_count + 1,
		     invitee_user_id = COALESCE(invitee_user_id, ?),
		     used_at = CASE
		         WHEN COALESCE(invitee_user_id, '') != '' THEN ?
		         WHEN max_uses IS NOT NULL AND use_count + 1 >= max_uses THEN ?
		         ELSE used_at
		     END
		 WHERE id = ?
		   AND revoked_at IS NULL
		   AND expires_at > ?
		   AND (
		     (COALESCE(invitee_user_id, '') != '' AND (used_at IS NULL OR used_at = 0))
		     OR
		     (COALESCE(invitee_user_id, '') = '' AND (max_uses IS NULL OR use_count < max_uses))
		   )`,
		nullableUser(consumerUserID), whenUnix, whenUnix, id, whenUnix,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvitationConsumed
	}
	if strings.TrimSpace(consumerUserID) != "" {
		_, err = s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO session_invitation_consumers (invitation_id, user_id, consumed_at) VALUES (?, ?, ?)`,
			id, consumerUserID, whenUnix,
		)
	}
	return err
}

// MarkRevoked records the revoked_at timestamp.
func (s *SQLiteStore) MarkRevoked(ctx context.Context, id string, when time.Time) error {
	if s == nil || s.db == nil {
		return errors.New("invitation store not configured")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE session_invitations SET revoked_at = ? WHERE id = ?`,
		when.Unix(), id,
	)
	return err
}

// UpdateTokenHash rotates the invitation token. Only pending rows
// should be updated; callers verify Active() first.
func (s *SQLiteStore) UpdateTokenHash(ctx context.Context, id, tokenHash string) error {
	if s == nil || s.db == nil {
		return errors.New("invitation store not configured")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return errors.New("token hash required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE session_invitations SET token_hash = ? WHERE id = ?`,
		tokenHash, id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvitationNotFound
	}
	return nil
}

const selectInvitationCols = `SELECT id, token_hash, session_id, session_kind, target_id,
	owner_user_id, invitee_user_id, invite_group_id, invite_tag, mode, max_uses, use_count,
	expires_at, used_at, revoked_at, created_at
	FROM session_invitations`

// rowScanner abstracts *sql.Row and *sql.Rows so scanInvitation can
// serve both call-sites without duplication.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanInvitation(row rowScanner) (Invitation, string, error) {
	var (
		inv         Invitation
		invitee     sql.NullString
		inviteGroup sql.NullString
		inviteTag   sql.NullString
		maxUses     sql.NullInt64
		useCount    int64
		used        sql.NullInt64
		revoked     sql.NullInt64
		expires     int64
		created     int64
		kind        string
		mode        string
		tokenHash   string
		targetID    string
		ownerUserID string
		sessionID   string
		id          string
	)
	if err := row.Scan(&id, &tokenHash, &sessionID, &kind, &targetID, &ownerUserID, &invitee, &inviteGroup, &inviteTag, &mode, &maxUses, &useCount, &expires, &used, &revoked, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Invitation{}, "", ErrInvitationNotFound
		}
		return Invitation{}, "", err
	}
	inv.ID = id
	inv.SessionID = sessionID
	inv.SessionKind = Kind(kind)
	inv.TargetID = targetID
	inv.OwnerUserID = ownerUserID
	if invitee.Valid {
		inv.InviteeUserID = strings.TrimSpace(invitee.String)
	}
	if inviteGroup.Valid {
		inv.InviteGroupID = strings.TrimSpace(inviteGroup.String)
	}
	if inviteTag.Valid {
		inv.InviteTag = strings.TrimSpace(inviteTag.String)
	}
	if maxUses.Valid {
		n := int(maxUses.Int64)
		inv.MaxUses = &n
	}
	inv.UseCount = int(useCount)
	inv.Mode = Mode(mode)
	if expires > 0 {
		inv.ExpiresAt = time.Unix(expires, 0).UTC()
	}
	if used.Valid {
		inv.UsedAt = time.Unix(used.Int64, 0).UTC()
	}
	if revoked.Valid {
		inv.RevokedAt = time.Unix(revoked.Int64, 0).UTC()
	}
	if created > 0 {
		inv.CreatedAt = time.Unix(created, 0).UTC()
	}
	return inv, tokenHash, nil
}

// nullableUser returns sql.NullString so blank invitee IDs end up as
// SQL NULL (matching the indexed column).
func nullableUser(s string) interface{} {
	v := strings.TrimSpace(s)
	if v == "" {
		return nil
	}
	return v
}

func nullableMaxUses(max *int) interface{} {
	if max == nil {
		return nil
	}
	return *max
}
