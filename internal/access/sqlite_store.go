package access

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SQLiteAccessGroupStore implements AccessGroupStore backed by SQLite.
type SQLiteAccessGroupStore struct {
	db *sql.DB
}

// NewSQLiteAccessGroupStore creates a new SQLite-backed access group store.
func NewSQLiteAccessGroupStore(db *sql.DB) *SQLiteAccessGroupStore {
	return &SQLiteAccessGroupStore{db: db}
}

// Create creates a new access group.
func (s *SQLiteAccessGroupStore) Create(id, name string) (*AccessGroup, error) {
	if id == "" || name == "" {
		return nil, errors.New("id and name must not be empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO access_groups (id, name, description)
		VALUES (?, ?, '')
	`, id, name)
	if err != nil {
		// Duplicate primary key results in SQL constraint error; normalize to ErrGroupExists.
		return nil, ErrGroupExists
	}
	return &AccessGroup{ID: id, Name: name}, nil
}

// Get returns an access group by ID.
func (s *SQLiteAccessGroupStore) Get(id string) (*AccessGroup, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var g AccessGroup
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name
		FROM access_groups
		WHERE id = ?
	`, id).Scan(&g.ID, &g.Name)
	if err == sql.ErrNoRows {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// AddUserToGroup adds a user to an access group.
func (s *SQLiteAccessGroupStore) AddUserToGroup(userID, groupID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ensure group exists to mirror in-memory behavior.
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM access_groups WHERE id = ?`, groupID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrGroupNotFound
		}
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_groups (user_id, group_id)
		VALUES (?, ?)
	`, userID, groupID)
	return err
}

// AddTargetToGroup grants the group access to the target.
func (s *SQLiteAccessGroupStore) AddTargetToGroup(groupID, targetID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM access_groups WHERE id = ?`, groupID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return ErrGroupNotFound
		}
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO group_targets (group_id, target_id)
		VALUES (?, ?)
	`, groupID, targetID)
	return err
}

// GroupIDsForUser returns the set of access group IDs the user belongs to.
func (s *SQLiteAccessGroupStore) GroupIDsForUser(userID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT group_id
		FROM user_groups
		WHERE user_id = ?
	`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			return nil
		}
		out = append(out, gid)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TargetIDsForGroup returns target IDs assigned to the group.
func (s *SQLiteAccessGroupStore) TargetIDsForGroup(groupID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT target_id
		FROM group_targets
		WHERE group_id = ?
	`, groupID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil
		}
		out = append(out, tid)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TargetIDsForUser returns the set of target IDs the user can access via any of their groups.
func (s *SQLiteAccessGroupStore) TargetIDsForUser(userID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT gt.target_id
		FROM user_groups ug
		JOIN group_targets gt ON ug.group_id = gt.group_id
		WHERE ug.user_id = ?
	`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil
		}
		out = append(out, tid)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SQLiteTargetStore implements TargetStore backed by SQLite.
type SQLiteTargetStore struct {
	db *sql.DB
}

// NewSQLiteTargetStore creates a new SQLite-backed target store.
func NewSQLiteTargetStore(db *sql.DB) *SQLiteTargetStore {
	return &SQLiteTargetStore{db: db}
}

// Create inserts a new target without an explicit path.
func (s *SQLiteTargetStore) Create(id, name, host string, port uint16, protocol Protocol) (*Target, error) {
	return s.CreateWithPath(id, name, host, port, protocol, "")
}

// CreateWithPath inserts a new target with an optional hierarchical path.
func (s *SQLiteTargetStore) CreateWithPath(id, name, host string, port uint16, protocol Protocol, path string) (*Target, error) {
	if id == "" || name == "" || host == "" {
		return nil, errors.New("id, name and host must not be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO targets (id, name, host, port, protocol, group_id, path)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, name, host, int(port), string(protocol), path, path)
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
func (s *SQLiteTargetStore) Get(id string) (*Target, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var t Target
	var port int
	var proto string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, host, port, protocol, path
		FROM targets
		WHERE id = ?
	`, id).Scan(&t.ID, &t.Name, &t.Host, &port, &proto, &t.Path)
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

// ListByIDs returns targets for the given IDs; missing IDs are skipped.
func (s *SQLiteTargetStore) ListByIDs(ids []string) []*Target {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out := make([]*Target, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		var t Target
		var port int
		var proto string
		err := s.db.QueryRowContext(ctx, `
			SELECT id, name, host, port, protocol, path
			FROM targets
			WHERE id = ?
		`, id).Scan(&t.ID, &t.Name, &t.Host, &port, &proto, &t.Path)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil
		}
		if port < 0 || port > 65535 {
			continue
		}
		// #nosec G115 -- port range validated above (0-65535)
		t.Port = uint16(port)
		t.Protocol = Protocol(proto)
		// copy value
		tc := t
		out = append(out, &tc)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
