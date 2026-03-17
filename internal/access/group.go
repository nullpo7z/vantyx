package access

import (
	"context"
	"errors"
)

// UserID identifies a user (e.g. session user).
type UserID string

// GroupID identifies an access group.
type GroupID string

// TargetID identifies a target (device).
type TargetID string

// ListOpts limits and paginates list results to avoid DoS from huge result sets.
// If nil, a default limit is applied by the implementation (see DefaultListLimit).
// Use AfterID for keyset (cursor) pagination; when set, returns items with id > AfterID. Offset is ignored when AfterID is set.
// Callers and implementations MUST still perform strict input validation (type/length/format)
// on IDs and tags according to OWASP ASVS L2 (e.g. see SQLiteAccessGroupStore in sqlite_store.go).
type ListOpts struct {
	Limit   int    // max number of items to return; <= 0 means implementation default
	Offset  int    // number of items to skip (ignored when AfterID is set)
	AfterID string // optional cursor: return items after this ID (keyset pagination)
}

// DefaultListLimit is the default maximum number of items returned by list operations
// when ListOpts.Limit is not specified. It is intentionally conservative to reduce the
// risk of application-level DoS from huge result sets (OWASP ASVS V11.1.4).
const DefaultListLimit = 100

// AccessGroup represents a group that can be granted access to targets.
type AccessGroup struct {
	ID   GroupID
	Name string
}

// AccessGroupStore defines the behavior required for managing access groups
// and their relationships to users and targets.
// Implementations may derive the acting principal (who performed the change)
// from context.Context for authorization and audit logging; callers are expected
// to attach authenticated user information to ctx (ASVS V4.1, V7.1.1).
type AccessGroupStore interface {
	Create(ctx context.Context, id GroupID, name string) (*AccessGroup, error)
	Get(ctx context.Context, id GroupID) (*AccessGroup, error)
	// Delete removes an access group. Implementations should define whether this is
	// a physical delete or a soft delete, and document the behavior.
	Delete(ctx context.Context, id GroupID) error
	AddUserToGroup(ctx context.Context, userID UserID, groupID GroupID) error
	RemoveUserFromGroup(ctx context.Context, userID UserID, groupID GroupID) error
	UserIDsForGroup(ctx context.Context, groupID GroupID, opts *ListOpts) ([]UserID, error)
	AddTargetToGroup(ctx context.Context, groupID GroupID, targetID TargetID) error
	// RemoveTargetFromGroup revokes the group's access to the target.
	RemoveTargetFromGroup(ctx context.Context, groupID GroupID, targetID TargetID) error
	GroupIDsForUser(ctx context.Context, userID UserID, opts *ListOpts) ([]GroupID, error)
	TargetIDsForGroup(ctx context.Context, groupID GroupID, opts *ListOpts) ([]TargetID, error)
	TargetIDsForUser(ctx context.Context, userID UserID, opts *ListOpts) ([]TargetID, error)
	// Tags: グループに付与されたタグ。タグ一致でもアクセス権を付与する。
	TagsForGroup(ctx context.Context, groupID GroupID) ([]string, error)
	SetGroupTags(ctx context.Context, groupID GroupID, tags []string) error
}

var (
	ErrGroupExists   = errors.New("access group already exists")
	ErrGroupNotFound = errors.New("access group not found")
)
