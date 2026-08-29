package access

import (
	"context"
	"database/sql"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nullpo7z/vantyx/internal/logging"
	"github.com/nullpo7z/vantyx/internal/secret"
)

var logger = logging.WithComponent("access")

// isUniqueConstraintErr reports whether err is a SQLite UNIQUE constraint
// violation, as opposed to some other failure (disk full, DB locked,
// connection lost, etc.). Several Create() methods in this package used
// to map *any* INSERT error to an "already exists" response, which hides
// the real cause from operators and sends users on a pointless "try a
// different ID" retry loop for unrelated failures. modernc.org/sqlite
// doesn't export its result-code constants, so this matches on the
// stable, version-independent SQLite error text rather than a numeric
// code.
func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// StoreConfig holds store behavior parameters (timeout, list limit).
// If nil is passed to constructors, defaults are used (5s timeout, defaultListLimit).
type StoreConfig struct {
	QueryTimeout     time.Duration
	DefaultListLimit int
}

const (
	maxIDLen   = 512
	maxNameLen = 512
	maxHostLen = 253
	// maxFanoutRowsPerQuery caps each of the sub-queries that
	// GroupIDsForUser / TargetIDsForUser union together in Go before
	// paginating. Unlike every other list method in this package, the
	// per-user union can't push LIMIT/OFFSET down to a single SQL
	// query (the final ordering only exists after dedup across all
	// three branches), so this acts as a hard backstop against
	// unbounded memory use (ASVS V11.1.4) rather than a real page
	// size -- it's set far above any realistic per-user membership
	// count.
	maxFanoutRowsPerQuery = 20000
)

var (
	idPattern       = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)
)

// escapeLikePrefix escapes SQL LIKE wildcards ("%", "_") and the
// escape character itself so a literal ID can be safely used as a
// LIKE prefix (with `ESCAPE '\'`). Group ID segments allow "_", which
// is otherwise a single-character LIKE wildcard.
func escapeLikePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func validateGroupID(id GroupID) error {
	s := string(id)
	if s == "" {
		return ErrGroupIDEmpty
	}
	if len(s) > maxIDLen {
		return ErrGroupIDTooLong
	}
	// Access group IDs may contain a hierarchical path (e.g. "parent/child").
	// Each path segment must match idPattern to keep IDs predictable and safe.
	for _, seg := range strings.Split(s, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ErrGroupIDInvalid
		}
		if !idPattern.MatchString(seg) {
			return ErrGroupIDInvalid
		}
	}
	return nil
}

func validateTargetID(id TargetID) error {
	s := string(id)
	if s == "" {
		return ErrTargetIDEmpty
	}
	if len(s) > maxIDLen {
		return ErrTargetIDTooLong
	}
	if !idPattern.MatchString(s) {
		return ErrTargetIDInvalid
	}
	return nil
}

func validateName(name string) error {
	if name == "" {
		return ErrNameEmpty
	}
	if len(name) > maxNameLen {
		return ErrNameTooLong
	}
	for _, r := range name {
		if r != '\t' && unicode.IsControl(r) {
			return ErrNameInvalid
		}
	}
	return nil
}

func validateHost(host string) error {
	if host == "" {
		return ErrHostEmpty
	}
	if len(host) > maxHostLen {
		return ErrHostTooLong
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := checkRestrictedHostIP(ip); err != nil {
			return err
		}
		return nil
	}
	if !hostnamePattern.MatchString(host) {
		return ErrHostInvalid
	}
	return nil
}

// validateProtocol ensures protocol is in the allowed whitelist (ASVS V5.1.4).
func validateProtocol(protocol Protocol) error {
	switch protocol {
	case ProtocolSSH, ProtocolTelnet, ProtocolVNC, ProtocolTFTP, ProtocolRDP, ProtocolFTP:
		return nil
	default:
		return ErrProtocolInvalid
	}
}

// listLimit resolves limit/offset from opts; large offset may degrade performance (consider keyset pagination for scale).
func listLimit(opts *ListOpts, defaultLimit int) (limit, offset int) {
	limit = defaultLimit
	offset = 0
	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		if opts.Offset > 0 {
			offset = opts.Offset
		}
	}
	return limit, offset
}

// SQLiteAccessGroupStore implements AccessGroupStore backed by SQLite.
// For multi-replica scale-out, use a shared PV/PVC or a network RDBMS (e.g. PostgreSQL).
type SQLiteAccessGroupStore struct {
	db               *sql.DB
	queryTimeout     time.Duration
	defaultListLimit int
}

// NewSQLiteAccessGroupStore creates a new SQLite-backed access group store.
// If cfg is nil, QueryTimeout 5s and DefaultListLimit (see group.go) are used.
func NewSQLiteAccessGroupStore(db *sql.DB, cfg *StoreConfig) *SQLiteAccessGroupStore {
	timeout := 5 * time.Second
	limit := DefaultListLimit
	if cfg != nil {
		if cfg.QueryTimeout > 0 {
			timeout = cfg.QueryTimeout
		}
		if cfg.DefaultListLimit > 0 {
			limit = cfg.DefaultListLimit
		}
	}
	return &SQLiteAccessGroupStore{db: db, queryTimeout: timeout, defaultListLimit: limit}
}

// Create creates a new access group.
func (s *SQLiteAccessGroupStore) Create(ctx context.Context, id GroupID, name string) (*AccessGroup, error) {
	if err := validateGroupID(id); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO access_groups (id, name, description)
		VALUES (?, ?, '')
	`, string(id), name)
	if err != nil {
		return nil, ErrGroupExists
	}
	return &AccessGroup{ID: id, Name: name}, nil
}

// Get returns an access group by ID.
func (s *SQLiteAccessGroupStore) Get(ctx context.Context, id GroupID) (*AccessGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	var idStr string
	var g AccessGroup
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name
		FROM access_groups
		WHERE id = ?
	`, string(id)).Scan(&idStr, &g.Name)
	if err == nil {
		g.ID = GroupID(idStr)
	}
	if err == sql.ErrNoRows {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Update updates an access group's name.
func (s *SQLiteAccessGroupStore) Update(ctx context.Context, id GroupID, name string) (*AccessGroup, error) {
	if err := validateGroupID(id); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	res, err := s.db.ExecContext(ctx, `UPDATE access_groups SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, name, string(id))
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrGroupNotFound
	}
	return &AccessGroup{ID: id, Name: name}, nil
}

// Delete removes an access group. Related rows (e.g. user_groups, group_tags, group_targets)
// are expected to be removed by foreign key cascades if configured.
func (s *SQLiteAccessGroupStore) Delete(ctx context.Context, id GroupID) error {
	if err := validateGroupID(id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	// group_targets.group_id cascades on delete, so deleting a group
	// that still has targets (or child groups, by the "parent/child"
	// ID naming convention) would silently orphan them -- removed from
	// every tree view without being deleted themselves. Refuse instead.
	var targetCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_targets WHERE group_id = ?`, string(id)).Scan(&targetCount); err != nil {
		return err
	}
	if targetCount > 0 {
		return ErrGroupNotEmpty
	}
	var childCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_groups WHERE id LIKE ? ESCAPE '\'`, escapeLikePrefix(string(id))+"/%").Scan(&childCount); err != nil {
		return err
	}
	if childCount > 0 {
		return ErrGroupNotEmpty
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM access_groups WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrGroupNotFound
	}
	return nil
}

// AddUserToGroup adds a user to an access group (within a transaction to avoid TOCTOU).
func (s *SQLiteAccessGroupStore) AddUserToGroup(ctx context.Context, userID UserID, groupID GroupID) error {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM access_groups WHERE id = ?`, string(groupID)).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrGroupNotFound
		}
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_groups (user_id, group_id)
		VALUES (?, ?)
	`, string(userID), string(groupID))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveUserFromGroup removes a user from an access group.
func (s *SQLiteAccessGroupStore) RemoveUserFromGroup(ctx context.Context, userID UserID, groupID GroupID) error {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM access_groups WHERE id = ?`, string(groupID)).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrGroupNotFound
		}
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_groups WHERE user_id = ? AND group_id = ?`, string(userID), string(groupID))
	return err
}

// UserIDsForGroup returns user IDs that belong to the group.
func (s *SQLiteAccessGroupStore) UserIDsForGroup(ctx context.Context, groupID GroupID, opts *ListOpts) ([]UserID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	limit, offset := listLimit(opts, s.defaultListLimit)
	var rows *sql.Rows
	var err error
	if opts != nil && opts.AfterID != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT user_id
			FROM user_groups
			WHERE group_id = ? AND user_id > ?
			ORDER BY user_id
			LIMIT ?
		`, string(groupID), opts.AfterID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT user_id
			FROM user_groups
			WHERE group_id = ?
			ORDER BY user_id
			LIMIT ? OFFSET ?
		`, string(groupID), limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserID
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, UserID(uid))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UserIDsForTarget returns distinct user IDs that can access targetID via
// group membership or tag-based ACL (mirrors TargetIDsForUser paths).
func (s *SQLiteAccessGroupStore) UserIDsForTarget(ctx context.Context, targetID TargetID, opts *ListOpts) ([]UserID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	tid := string(targetID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT uid FROM (
			SELECT ug.user_id AS uid
			FROM user_groups ug
			INNER JOIN group_targets gt ON gt.group_id = ug.group_id
			WHERE gt.target_id = ?
			UNION
			SELECT ut.user_id AS uid
			FROM user_tags ut
			INNER JOIN target_tags tt ON tt.tag = ut.tag
			WHERE tt.target_id = ?
			UNION
			SELECT ut.user_id AS uid
			FROM user_tags ut
			INNER JOIN group_tags gtag ON gtag.tag = ut.tag
			INNER JOIN group_targets gt ON gt.group_id = gtag.group_id
			WHERE gt.target_id = ?
		)
		ORDER BY uid
	`, tid, tid, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []UserID
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		all = append(all, UserID(uid))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	limit, offset := listLimit(opts, s.defaultListLimit)
	if opts != nil && opts.AfterID != "" {
		i := 0
		for i < len(all) && string(all[i]) <= opts.AfterID {
			i++
		}
		all = all[i:]
	}
	if offset > 0 {
		if offset >= len(all) {
			return nil, nil
		}
		all = all[offset:]
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// TagsGrantingTargetAccess returns distinct tags that grant access to targetID.
func (s *SQLiteAccessGroupStore) TagsGrantingTargetAccess(ctx context.Context, targetID TargetID) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT tag FROM (
			SELECT tag FROM target_tags WHERE target_id = ?
			UNION
			SELECT gtag.tag FROM group_tags gtag
			INNER JOIN group_targets gt ON gt.group_id = gtag.group_id
			WHERE gt.target_id = ?
		)
		ORDER BY tag
	`, string(targetID), string(targetID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

const maxTagLen = 64

// validateTag returns nil if tag is valid (1–64 chars, alphanumeric + hyphen/underscore).
func validateTag(tag string) error {
	if tag == "" || len(tag) > maxTagLen {
		return ErrTagLength
	}
	for _, r := range tag {
		if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return ErrTagChars
		}
	}
	return nil
}

// TagsForGroup returns tags assigned to the group.
func (s *SQLiteAccessGroupStore) TagsForGroup(ctx context.Context, groupID GroupID) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT tag FROM group_tags WHERE group_id = ? ORDER BY tag`, string(groupID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// SetGroupTags replaces all tags for the group. Pass nil or empty to clear.
func (s *SQLiteAccessGroupStore) SetGroupTags(ctx context.Context, groupID GroupID, tags []string) error {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	for _, tag := range tags {
		if err := validateTag(tag); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM access_groups WHERE id = ?`, string(groupID)).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrGroupNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_tags WHERE group_id = ?`, string(groupID)); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO group_tags (group_id, tag) VALUES (?, ?)`, string(groupID), tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddTargetToGroup grants the group access to the target (within a transaction to avoid TOCTOU).
func (s *SQLiteAccessGroupStore) AddTargetToGroup(ctx context.Context, groupID GroupID, targetID TargetID) error {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM access_groups WHERE id = ?`, string(groupID)).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrGroupNotFound
		}
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO group_targets (group_id, target_id)
		VALUES (?, ?)
	`, string(groupID), string(targetID))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveTargetFromGroup revokes the group's access to the target.
func (s *SQLiteAccessGroupStore) RemoveTargetFromGroup(ctx context.Context, groupID GroupID, targetID TargetID) error {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM access_groups WHERE id = ?`, string(groupID)).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrGroupNotFound
		}
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM group_targets
		WHERE group_id = ? AND target_id = ?
	`, string(groupID), string(targetID))
	return err
}

// GroupIDsForTarget returns every group the target is directly assigned to via group_targets.
func (s *SQLiteAccessGroupStore) GroupIDsForTarget(ctx context.Context, targetID TargetID) ([]GroupID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT group_id
		FROM group_targets
		WHERE target_id = ?
		ORDER BY group_id
	`, string(targetID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GroupID
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			return nil, err
		}
		out = append(out, GroupID(gid))
	}
	return out, rows.Err()
}

// GroupIDsForUser returns the set of access group IDs the user can see: direct membership (user_groups)
// and tag-based (groups that have a target with matching user tag, or groups with matching tag).
func (s *SQLiteAccessGroupStore) GroupIDsForUser(ctx context.Context, userID UserID, opts *ListOpts) ([]GroupID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	seen := make(map[GroupID]bool)

	// 1) Via direct group membership
	rows1, err := s.db.QueryContext(ctx, `
		SELECT group_id FROM user_groups WHERE user_id = ? LIMIT ?
	`, string(userID), maxFanoutRowsPerQuery)
	if err != nil {
		return nil, err
	}
	for rows1.Next() {
		var gid string
		if err := rows1.Scan(&gid); err != nil {
			rows1.Close()
			return nil, err
		}
		seen[GroupID(gid)] = true
	}
	rows1.Close()
	if err := rows1.Err(); err != nil {
		return nil, err
	}

	// 2) Via user tag = target tag (groups that contain such targets)
	rows2, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT gt.group_id
		FROM group_targets gt
		INNER JOIN target_tags tt ON gt.target_id = tt.target_id
		INNER JOIN user_tags ut ON ut.tag = tt.tag AND ut.user_id = ?
		LIMIT ?
	`, string(userID), maxFanoutRowsPerQuery)
	if err != nil {
		return nil, err
	}
	for rows2.Next() {
		var gid string
		if err := rows2.Scan(&gid); err != nil {
			rows2.Close()
			return nil, err
		}
		seen[GroupID(gid)] = true
	}
	rows2.Close()
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// 3) Via user tag = group tag
	rows3, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT gtag.group_id
		FROM group_tags gtag
		INNER JOIN user_tags ut ON gtag.tag = ut.tag AND ut.user_id = ?
		LIMIT ?
	`, string(userID), maxFanoutRowsPerQuery)
	if err != nil {
		return nil, err
	}
	for rows3.Next() {
		var gid string
		if err := rows3.Scan(&gid); err != nil {
			rows3.Close()
			return nil, err
		}
		seen[GroupID(gid)] = true
	}
	rows3.Close()
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	var all []GroupID
	for id := range seen {
		all = append(all, id)
	}
	sort.Slice(all, func(i, j int) bool { return string(all[i]) < string(all[j]) })

	limit, offset := listLimit(opts, s.defaultListLimit)
	if opts != nil && opts.AfterID != "" {
		i := 0
		for i < len(all) && string(all[i]) <= opts.AfterID {
			i++
		}
		all = all[i:]
	}
	if offset > 0 {
		if offset >= len(all) {
			return nil, nil
		}
		all = all[offset:]
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// TargetIDsForGroup returns target IDs assigned to the group.
func (s *SQLiteAccessGroupStore) TargetIDsForGroup(ctx context.Context, groupID GroupID, opts *ListOpts) ([]TargetID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	limit, offset := listLimit(opts, s.defaultListLimit)
	var rows *sql.Rows
	var err error
	if opts != nil && opts.AfterID != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT target_id
			FROM group_targets
			WHERE group_id = ? AND target_id > ?
			ORDER BY target_id
			LIMIT ?
		`, string(groupID), opts.AfterID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT target_id
			FROM group_targets
			WHERE group_id = ?
			ORDER BY target_id
			LIMIT ? OFFSET ?
		`, string(groupID), limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TargetID
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, err
		}
		out = append(out, TargetID(tid))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// TargetIDsForUser returns the set of target IDs the user can access via group membership or tag match.
// Tag-based: user has tag T and (target has tag T or target's group has tag T).
func (s *SQLiteAccessGroupStore) TargetIDsForUser(ctx context.Context, userID UserID, opts *ListOpts) ([]TargetID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	// Collect all accessible target IDs: from groups and from tags (then dedup, sort, paginate in Go).
	seen := make(map[TargetID]bool)

	// 1) Via group membership
	rows1, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT gt.target_id
		FROM user_groups ug
		JOIN group_targets gt ON ug.group_id = gt.group_id
		WHERE ug.user_id = ?
		LIMIT ?
	`, string(userID), maxFanoutRowsPerQuery)
	if err != nil {
		return nil, err
	}
	for rows1.Next() {
		var tid string
		if err := rows1.Scan(&tid); err != nil {
			rows1.Close()
			return nil, err
		}
		seen[TargetID(tid)] = true
	}
	rows1.Close()
	if err := rows1.Err(); err != nil {
		return nil, err
	}

	// 2) Via user tag = target tag
	rows2, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT tt.target_id
		FROM target_tags tt
		INNER JOIN user_tags ut ON ut.tag = tt.tag AND ut.user_id = ?
		LIMIT ?
	`, string(userID), maxFanoutRowsPerQuery)
	if err != nil {
		return nil, err
	}
	for rows2.Next() {
		var tid string
		if err := rows2.Scan(&tid); err != nil {
			rows2.Close()
			return nil, err
		}
		seen[TargetID(tid)] = true
	}
	rows2.Close()
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	// 3) Via user tag = group tag (target belongs to that group)
	rows3, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT gt.target_id
		FROM user_tags ut
		INNER JOIN group_tags gtag ON gtag.tag = ut.tag AND ut.user_id = ?
		INNER JOIN group_targets gt ON gtag.group_id = gt.group_id
		LIMIT ?
	`, string(userID), maxFanoutRowsPerQuery)
	if err != nil {
		return nil, err
	}
	for rows3.Next() {
		var tid string
		if err := rows3.Scan(&tid); err != nil {
			rows3.Close()
			return nil, err
		}
		seen[TargetID(tid)] = true
	}
	rows3.Close()
	if err := rows3.Err(); err != nil {
		return nil, err
	}

	var all []TargetID
	for id := range seen {
		all = append(all, id)
	}
	sortSliceTargetID(all)

	limit, offset := listLimit(opts, s.defaultListLimit)
	if opts != nil && opts.AfterID != "" {
		// Keyset: skip until after AfterID, then take limit
		i := 0
		for i < len(all) && string(all[i]) <= opts.AfterID {
			i++
		}
		all = all[i:]
	}
	if offset > 0 {
		if offset >= len(all) {
			return nil, nil
		}
		all = all[offset:]
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// sortSliceTargetID sorts in place by string(id).
func sortSliceTargetID(s []TargetID) {
	sort.Slice(s, func(i, j int) bool { return string(s[i]) < string(s[j]) })
}

// SQLiteTargetStore implements TargetStore backed by SQLite.
// encKey is optional; when non-nil (32 bytes), ssh_password is encrypted at rest (ASVS L2).
type SQLiteTargetStore struct {
	db               *sql.DB
	queryTimeout     time.Duration
	defaultListLimit int
	encKey           []byte
}

// NewSQLiteTargetStore creates a new SQLite-backed target store.
// If cfg is nil, QueryTimeout 5s and DefaultListLimit (see group.go) are used.
// encKey is the 32-byte key for encrypting ssh_password at rest (ASVS 7.12); if nil, storing a non-empty SSH password will return ErrEncryptionKeyRequired.
func NewSQLiteTargetStore(db *sql.DB, cfg *StoreConfig, encKey []byte) *SQLiteTargetStore {
	timeout := 5 * time.Second
	limit := DefaultListLimit
	if cfg != nil {
		if cfg.QueryTimeout > 0 {
			timeout = cfg.QueryTimeout
		}
		if cfg.DefaultListLimit > 0 {
			limit = cfg.DefaultListLimit
		}
	}
	return &SQLiteTargetStore{db: db, queryTimeout: timeout, defaultListLimit: limit, encKey: encKey}
}

// credentialAAD returns the Associated Authenticated Data used for the
// given target's column. Binding the ciphertext to "<id>:<field>"
// prevents an attacker with write access to the SQLite file from
// swapping ciphertexts between rows or columns (CWE-345).
func credentialAAD(targetID TargetID, field string) []byte {
	return []byte("vantyx/target/" + string(targetID) + "/" + field)
}

func (s *SQLiteTargetStore) encryptCredentials(targetID TargetID, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string) (storedPassword, storedKey, storedKeyPass string, err error) {
	if (sshPassword != "" || sshPrivateKey != "") && (s.encKey == nil || len(s.encKey) != secret.KeySize) {
		return "", "", "", ErrEncryptionKeyRequired
	}

	storedPassword = sshPassword
	if sshPassword != "" {
		storedPassword, err = secret.EncryptWithAAD(s.encKey, sshPassword, credentialAAD(targetID, "ssh_password"))
		if err != nil {
			return "", "", "", err
		}
	}

	storedKey = sshPrivateKey
	if sshPrivateKey != "" {
		storedKey, err = secret.EncryptWithAAD(s.encKey, sshPrivateKey, credentialAAD(targetID, "ssh_private_key"))
		if err != nil {
			return "", "", "", err
		}
	}

	storedKeyPass = sshPrivateKeyPassphrase
	if sshPrivateKeyPassphrase != "" {
		storedKeyPass, err = secret.EncryptWithAAD(s.encKey, sshPrivateKeyPassphrase, credentialAAD(targetID, "ssh_private_key_passphrase"))
		if err != nil {
			return "", "", "", err
		}
	}

	return storedPassword, storedKey, storedKeyPass, nil
}

// Create inserts a new target without path (group_id and path empty; may fail if FK requires a group).
func (s *SQLiteTargetStore) Create(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol) (*Target, error) {
	return s.CreateWithPath(ctx, id, name, host, port, protocol, "", "", "", "", "", "", true, false, false)
}

// CreateWithPath inserts a new target with group_id, path, optional SSH credentials, and file transfer protocol toggles.
func (s *SQLiteTargetStore) CreateWithPath(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol, groupID GroupID, path string, sshUsername, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string, sftpEnabled, ftpEnabled, tftpEnabled bool) (*Target, error) {
	if err := validateTargetID(id); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateHost(host); err != nil {
		return nil, err
	}
	if err := validateProtocol(protocol); err != nil {
		return nil, err
	}
	if groupID != "" {
		if err := validateGroupID(groupID); err != nil {
			return nil, err
		}
	}
	storedPassword, storedKey, storedKeyPass, err := s.encryptCredentials(id, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	sftpVal, ftpVal, tftpVal := 0, 0, 0
	if sftpEnabled {
		sftpVal = 1
	}
	if ftpEnabled {
		ftpVal = 1
	}
	if tftpEnabled {
		tftpVal = 1
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO targets (id, name, host, port, protocol, group_id, path, ssh_username, ssh_password, ssh_private_key, ssh_private_key_passphrase, sftp_enabled, ftp_enabled, tftp_enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, string(id), name, host, int(port), string(protocol), string(groupID), path, sshUsername, storedPassword, storedKey, storedKeyPass, sftpVal, ftpVal, tftpVal)
	if err != nil {
		return nil, ErrTargetExists
	}
	return &Target{
		ID:                      id,
		Name:                    name,
		Host:                    host,
		Port:                    port,
		Protocol:                protocol,
		Path:                    path,
		SSHUsername:             sshUsername,
		SSHPassword:             sshPassword,
		SSHPrivateKey:           sshPrivateKey,
		SSHPrivateKeyPassphrase: sshPrivateKeyPassphrase,
		SFTPEnabled:             sftpEnabled,
		FTPEnabled:              ftpEnabled,
		TFTPEnabled:             tftpEnabled,
	}, nil
}

// Update updates a target's name, host, port, protocol, path, optional SSH credentials, and file transfer toggles.
// Target ID and group_id are not changed. Non-empty sshPassword or sshPrivateKey require encKey.
func (s *SQLiteTargetStore) Update(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol, path, sshUsername, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase string, sftpEnabled, ftpEnabled, tftpEnabled bool) (*Target, error) {
	if err := validateTargetID(id); err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateHost(host); err != nil {
		return nil, err
	}
	if err := validateProtocol(protocol); err != nil {
		return nil, err
	}
	storedPassword, storedKey, storedKeyPass, err := s.encryptCredentials(id, sshPassword, sshPrivateKey, sshPrivateKeyPassphrase)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	sftpVal, ftpVal, tftpVal := 0, 0, 0
	if sftpEnabled {
		sftpVal = 1
	}
	if ftpEnabled {
		ftpVal = 1
	}
	if tftpEnabled {
		tftpVal = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE targets SET name = ?, host = ?, port = ?, protocol = ?, path = ?, ssh_username = ?, ssh_password = ?, ssh_private_key = ?, ssh_private_key_passphrase = ?, sftp_enabled = ?, ftp_enabled = ?, tftp_enabled = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, host, int(port), string(protocol), path, strings.TrimSpace(sshUsername), storedPassword, storedKey, storedKeyPass, sftpVal, ftpVal, tftpVal, string(id))
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrTargetNotFound
	}
	return s.Get(ctx, id)
}

// validHostKeyFingerprint accepts "SHA256:<base64>" (with or without padding)
// or empty. Returns the canonical form (padding stripped) or an error.
func validHostKeyFingerprint(fp string) (string, error) {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return "", nil
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		return "", ErrHostKeyFingerprintInvalid
	}
	body := strings.TrimRight(fp[len("SHA256:"):], "=")
	if len(body) < 40 || len(body) > 60 {
		return "", ErrHostKeyFingerprintInvalid
	}
	for _, r := range body {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '+' || r == '/':
		default:
			return "", ErrHostKeyFingerprintInvalid
		}
	}
	return "SHA256:" + body, nil
}

// SetSSHHostKeyFingerprint records (or clears) the expected SHA-256
// fingerprint of the upstream SSH host key for the target.
func (s *SQLiteTargetStore) SetSSHHostKeyFingerprint(ctx context.Context, id TargetID, fingerprint string) error {
	if err := validateTargetID(id); err != nil {
		return err
	}
	canonical, err := validHostKeyFingerprint(fingerprint)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `UPDATE targets SET ssh_host_key_fingerprint = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, canonical, string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTargetNotFound
	}
	return nil
}

// SetSSHHostKeyInsecureSkipVerify toggles per-target host-key
// verification bypass. The fingerprint column is *not* cleared so the
// operator can flip back later, but bridges that see skip=1 will
// accept any host key on the next connection.
func (s *SQLiteTargetStore) SetSSHHostKeyInsecureSkipVerify(ctx context.Context, id TargetID, skip bool) error {
	if err := validateTargetID(id); err != nil {
		return err
	}
	val := 0
	if skip {
		val = 1
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `UPDATE targets SET ssh_host_key_insecure_skip_verify = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, val, string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTargetNotFound
	}
	return nil
}

// SetCredentialSource records (or clears) which Identity / SSH Key
// library entry the target's credentials were last set from.
func (s *SQLiteTargetStore) SetCredentialSource(ctx context.Context, id TargetID, credentialIdentityID CredentialIdentityID, sshKeyID SSHKeyID) error {
	if err := validateTargetID(id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	res, err := s.db.ExecContext(ctx, `UPDATE targets SET credential_identity_id = ?, ssh_key_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		string(credentialIdentityID), string(sshKeyID), string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTargetNotFound
	}
	return nil
}

// Delete removes a target. Group assignments (group_targets) are removed by FK CASCADE.
func (s *SQLiteTargetStore) Delete(ctx context.Context, id TargetID) error {
	if err := validateTargetID(id); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	res, err := s.db.ExecContext(ctx, `DELETE FROM targets WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrTargetNotFound
	}
	return nil
}

// Get returns a target by ID.
func (s *SQLiteTargetStore) Get(ctx context.Context, id TargetID) (*Target, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	var t Target
	var idStr string
	var port int
	var proto string
	var storedPassword, storedKey, storedKeyPass string
	var sftpVal, ftpVal, tftpVal, insecureSkipVal int
	var hostKeyFP string
	var credentialIdentityID, sshKeyID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, host, port, protocol, path, COALESCE(ssh_username,''), COALESCE(ssh_password,''), COALESCE(ssh_private_key,''), COALESCE(ssh_private_key_passphrase,''), COALESCE(sftp_enabled,1), COALESCE(ftp_enabled,0), COALESCE(tftp_enabled,0), COALESCE(ssh_host_key_fingerprint,''), COALESCE(ssh_host_key_insecure_skip_verify,0), COALESCE(credential_identity_id,''), COALESCE(ssh_key_id,'')
		FROM targets
		WHERE id = ?
	`, string(id)).Scan(&idStr, &t.Name, &t.Host, &port, &proto, &t.Path, &t.SSHUsername, &storedPassword, &storedKey, &storedKeyPass, &sftpVal, &ftpVal, &tftpVal, &hostKeyFP, &insecureSkipVal, &credentialIdentityID, &sshKeyID)
	if err == nil {
		t.ID = TargetID(idStr)
		t.SSHPassword = decryptOrPlain(s.encKey, storedPassword, t.ID, "ssh_password")
		t.SSHPrivateKey = decryptOrPlain(s.encKey, storedKey, t.ID, "ssh_private_key")
		t.SSHPrivateKeyPassphrase = decryptOrPlain(s.encKey, storedKeyPass, t.ID, "ssh_private_key_passphrase")
		t.SFTPEnabled = sftpVal != 0
		t.FTPEnabled = ftpVal != 0
		t.TFTPEnabled = tftpVal != 0
		t.SSHHostKeyFingerprint = hostKeyFP
		t.SSHHostKeyInsecureSkipVerify = insecureSkipVal != 0
		t.CredentialIdentityID = CredentialIdentityID(credentialIdentityID)
		t.SSHKeyID = SSHKeyID(sshKeyID)
	}
	if err == sql.ErrNoRows {
		return nil, ErrTargetNotFound
	}
	if err != nil {
		return nil, err
	}
	if port < 0 || port > 65535 {
		return nil, ErrTargetNotFound
	}
	// #nosec G115 -- port range validated above (0-65535)
	t.Port = uint16(port)
	t.Protocol = Protocol(proto)
	return &t, nil
}

// decryptOrPlain decrypts a v2 credential. The caller-supplied targetID and field bind the AAD.
func decryptOrPlain(encKey []byte, stored string, targetID TargetID, field string) string {
	if stored == "" {
		return ""
	}
	if !secret.IsEncrypted(stored) {
		// Plaintext rows (should not occur when encryption is enabled). Refuse when
		// strict mode is on so a DB write that bypasses Encrypt()
		// (e.g. a leaked backup re-imported by an attacker) can no
		// longer be used to inject plaintext credentials
		// (CWE-326 / CWE-757).
		if os.Getenv("VANTYX_STRICT_CIPHERTEXT") == "1" {
			logger.Warn("rejected non-versioned credential in strict ciphertext mode")
			return ""
		}
		logger.Warn("stored credential is not encrypted; enable VANTYX_STRICT_CIPHERTEXT=1 to reject these")
		return stored
	}
	if len(encKey) != secret.KeySize {
		logger.Warn("encrypted credential present but encryption key is not configured correctly")
		return ""
	}
	dec, err := secret.DecryptWithAAD(encKey, stored, credentialAAD(targetID, field))
	if err != nil {
		logger.Warn("failed to decrypt stored credential", "error", err, "target_id", string(targetID), "field", field)
		return ""
	}
	return dec
}

// ListByIDs returns targets for the given IDs in one query (preserves order, skips missing).
func (s *SQLiteTargetStore) ListByIDs(ctx context.Context, ids []TargetID, opts *ListOpts) ([]*Target, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	limit, offset := listLimit(opts, s.defaultListLimit)
	if offset > 0 || limit < len(ids) {
		if offset >= len(ids) {
			return []*Target{}, nil
		}
		end := offset + limit
		if end > len(ids) {
			end = len(ids)
		}
		ids = ids[offset:end]
	}

	seen := make(map[TargetID]bool)
	var unique []TargetID
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []*Target{}, nil
	}

	// SQLite default max variable number is 999; chunk if needed.
	// Each chunk runs in a closure so defer rows.Close() runs and avoids connection leak on panic.
	const maxVars = 999
	byID := make(map[TargetID]*Target)
	for i := 0; i < len(unique); i += maxVars {
		end := i + maxVars
		if end > len(unique) {
			end = len(unique)
		}
		chunk := unique[i:end]
		placeholders := ""
		args := make([]interface{}, 0, len(chunk))
		for j, id := range chunk {
			if j > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, string(id))
		}
		err := func() error {
			// #nosec G202 -- placeholders is "?,?,?" from len(chunk); args are validated TargetIDs
			rows, err := s.db.QueryContext(ctx, `
				SELECT id, name, host, port, protocol, path, COALESCE(ssh_username,''), COALESCE(ssh_password,''), COALESCE(ssh_private_key,''), COALESCE(ssh_private_key_passphrase,''), COALESCE(sftp_enabled,1), COALESCE(ftp_enabled,0), COALESCE(tftp_enabled,0), COALESCE(ssh_host_key_fingerprint,''), COALESCE(ssh_host_key_insecure_skip_verify,0), COALESCE(credential_identity_id,''), COALESCE(ssh_key_id,'')
				FROM targets
				WHERE id IN (`+placeholders+`)`, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var t Target
				var idStr string
				var port int
				var proto string
				var storedPassword, storedKey, storedKeyPass string
				var sftpVal, ftpVal, tftpVal, insecureSkipVal int
				var hostKeyFP string
				var credentialIdentityID, sshKeyID string
				if err := rows.Scan(&idStr, &t.Name, &t.Host, &port, &proto, &t.Path, &t.SSHUsername, &storedPassword, &storedKey, &storedKeyPass, &sftpVal, &ftpVal, &tftpVal, &hostKeyFP, &insecureSkipVal, &credentialIdentityID, &sshKeyID); err != nil {
					return err
				}
				if port >= 0 && port <= 65535 {
					t.ID = TargetID(idStr)
					t.Port = uint16(port)
					t.Protocol = Protocol(proto)
					t.SSHPassword = decryptOrPlain(s.encKey, storedPassword, t.ID, "ssh_password")
					t.SSHPrivateKey = decryptOrPlain(s.encKey, storedKey, t.ID, "ssh_private_key")
					t.SSHPrivateKeyPassphrase = decryptOrPlain(s.encKey, storedKeyPass, t.ID, "ssh_private_key_passphrase")
					t.SFTPEnabled = sftpVal != 0
					t.FTPEnabled = ftpVal != 0
					t.TFTPEnabled = tftpVal != 0
					t.SSHHostKeyFingerprint = hostKeyFP
					t.SSHHostKeyInsecureSkipVerify = insecureSkipVal != 0
					t.CredentialIdentityID = CredentialIdentityID(credentialIdentityID)
					t.SSHKeyID = SSHKeyID(sshKeyID)
					byID[TargetID(idStr)] = &t
				}
			}
			return rows.Err()
		}()
		if err != nil {
			return nil, err
		}
	}

	out := make([]*Target, 0, len(unique))
	for _, id := range unique {
		if t := byID[id]; t != nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// ListByProtocol returns targets for the given protocol. Credentials are not populated.
func (s *SQLiteTargetStore) ListByProtocol(ctx context.Context, protocol Protocol) ([]*Target, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, host, port, protocol, path,
			COALESCE(sftp_enabled, 1), COALESCE(ftp_enabled, 0), COALESCE(tftp_enabled, 0)
		FROM targets
		WHERE protocol = ?
	`, string(protocol))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Target
	for rows.Next() {
		var t Target
		var idStr string
		var port int
		var proto string
		var sftpVal, ftpVal, tftpVal int
		if err := rows.Scan(&idStr, &t.Name, &t.Host, &port, &proto, &t.Path, &sftpVal, &ftpVal, &tftpVal); err != nil {
			return nil, err
		}
		if port >= 0 && port <= 65535 {
			t.ID = TargetID(idStr)
			t.Port = uint16(port)
			t.Protocol = Protocol(proto)
			t.SFTPEnabled = sftpVal != 0
			t.FTPEnabled = ftpVal != 0
			t.TFTPEnabled = tftpVal != 0
			out = append(out, &t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// TagsForTarget returns tags assigned to the target.
func (s *SQLiteTargetStore) TagsForTarget(ctx context.Context, targetID TargetID) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT tag FROM target_tags WHERE target_id = ? ORDER BY tag`, string(targetID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// SetTargetTags replaces all tags for the target. Pass nil or empty to clear.
func (s *SQLiteTargetStore) SetTargetTags(ctx context.Context, targetID TargetID, tags []string) error {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	for _, tag := range tags {
		if err := validateTag(tag); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM targets WHERE id = ?`, string(targetID)).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrTargetNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM target_tags WHERE target_id = ?`, string(targetID)); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO target_tags (target_id, tag) VALUES (?, ?)`, string(targetID), tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}
