package access

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"regexp"
	"time"
	"unicode"
)

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
)

var (
	idPattern       = regexp.MustCompile(`^[a-zA-Z0-9_\-./]+$`)
	hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)
)

func validateGroupID(id GroupID) error {
	s := string(id)
	if s == "" {
		return errors.New("group id must not be empty")
	}
	if len(s) > maxIDLen {
		return errors.New("group id too long")
	}
	if !idPattern.MatchString(s) {
		return errors.New("group id contains invalid characters")
	}
	return nil
}

func validateTargetID(id TargetID) error {
	s := string(id)
	if s == "" {
		return errors.New("target id must not be empty")
	}
	if len(s) > maxIDLen {
		return errors.New("target id too long")
	}
	if !idPattern.MatchString(s) {
		return errors.New("target id contains invalid characters")
	}
	return nil
}

func validateName(name string) error {
	if name == "" {
		return errors.New("name must not be empty")
	}
	if len(name) > maxNameLen {
		return errors.New("name too long")
	}
	for _, r := range name {
		if r != '\t' && unicode.IsControl(r) {
			return errors.New("name contains invalid characters")
		}
	}
	return nil
}

func validateHost(host string) error {
	if host == "" {
		return errors.New("host must not be empty")
	}
	if len(host) > maxHostLen {
		return errors.New("host too long")
	}
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	if !hostnamePattern.MatchString(host) {
		return errors.New("host must be a valid hostname or IP address")
	}
	return nil
}

// validateProtocol ensures protocol is in the allowed whitelist (ASVS V5.1.4).
func validateProtocol(protocol Protocol) error {
	switch protocol {
	case ProtocolSSH, ProtocolTelnet:
		return nil
	default:
		return errors.New("protocol must be ssh or telnet")
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
// If cfg is nil, QueryTimeout 5s and DefaultListLimit (see group.go defaultListLimit) are used.
func NewSQLiteAccessGroupStore(db *sql.DB, cfg *StoreConfig) *SQLiteAccessGroupStore {
	timeout := 5 * time.Second
	limit := defaultListLimit // defined in group.go
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

// GroupIDsForUser returns the set of access group IDs the user belongs to.
func (s *SQLiteAccessGroupStore) GroupIDsForUser(ctx context.Context, userID UserID, opts *ListOpts) ([]GroupID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	limit, offset := listLimit(opts, s.defaultListLimit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT group_id
		FROM user_groups
		WHERE user_id = ?
		LIMIT ? OFFSET ?
	`, string(userID), limit, offset)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// TargetIDsForGroup returns target IDs assigned to the group.
func (s *SQLiteAccessGroupStore) TargetIDsForGroup(ctx context.Context, groupID GroupID, opts *ListOpts) ([]TargetID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	limit, offset := listLimit(opts, s.defaultListLimit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT target_id
		FROM group_targets
		WHERE group_id = ?
		LIMIT ? OFFSET ?
	`, string(groupID), limit, offset)
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

// TargetIDsForUser returns the set of target IDs the user can access via any of their groups.
func (s *SQLiteAccessGroupStore) TargetIDsForUser(ctx context.Context, userID UserID, opts *ListOpts) ([]TargetID, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	limit, offset := listLimit(opts, s.defaultListLimit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT gt.target_id
		FROM user_groups ug
		JOIN group_targets gt ON ug.group_id = gt.group_id
		WHERE ug.user_id = ?
		LIMIT ? OFFSET ?
	`, string(userID), limit, offset)
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

// SQLiteTargetStore implements TargetStore backed by SQLite.
type SQLiteTargetStore struct {
	db               *sql.DB
	queryTimeout     time.Duration
	defaultListLimit int
}

// NewSQLiteTargetStore creates a new SQLite-backed target store.
// If cfg is nil, QueryTimeout 5s and DefaultListLimit (see group.go defaultListLimit) are used.
func NewSQLiteTargetStore(db *sql.DB, cfg *StoreConfig) *SQLiteTargetStore {
	timeout := 5 * time.Second
	limit := defaultListLimit // defined in group.go
	if cfg != nil {
		if cfg.QueryTimeout > 0 {
			timeout = cfg.QueryTimeout
		}
		if cfg.DefaultListLimit > 0 {
			limit = cfg.DefaultListLimit
		}
	}
	return &SQLiteTargetStore{db: db, queryTimeout: timeout, defaultListLimit: limit}
}

// Create inserts a new target without path (group_id and path empty; may fail if FK requires a group).
func (s *SQLiteTargetStore) Create(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol) (*Target, error) {
	return s.CreateWithPath(ctx, id, name, host, port, protocol, "", "")
}

// CreateWithPath inserts a new target with group_id and path.
func (s *SQLiteTargetStore) CreateWithPath(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol, groupID GroupID, path string) (*Target, error) {
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
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO targets (id, name, host, port, protocol, group_id, path)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, string(id), name, host, int(port), string(protocol), string(groupID), path)
	if err != nil {
		return nil, ErrTargetExists
	}
	return &Target{
		ID:       id,
		Name:     name,
		Host:     host,
		Port:     port,
		Protocol: protocol,
		Path:     path,
	}, nil
}

// Get returns a target by ID.
func (s *SQLiteTargetStore) Get(ctx context.Context, id TargetID) (*Target, error) {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()

	var t Target
	var idStr string
	var port int
	var proto string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, host, port, protocol, path
		FROM targets
		WHERE id = ?
	`, string(id)).Scan(&idStr, &t.Name, &t.Host, &port, &proto, &t.Path)
	if err == nil {
		t.ID = TargetID(idStr)
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
			rows, err := s.db.QueryContext(ctx, `
				SELECT id, name, host, port, protocol, path
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
				if err := rows.Scan(&idStr, &t.Name, &t.Host, &port, &proto, &t.Path); err != nil {
					return err
				}
				if port >= 0 && port <= 65535 {
					t.ID = TargetID(idStr)
					t.Port = uint16(port)
					t.Protocol = Protocol(proto)
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
