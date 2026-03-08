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
// If nil, a default limit is applied by the implementation.
// Use AfterID for keyset (cursor) pagination; when set, returns items with id > AfterID. Offset is ignored when AfterID is set.
type ListOpts struct {
	Limit   int    // max number of items to return; <= 0 means implementation default
	Offset  int    // number of items to skip (ignored when AfterID is set)
	AfterID string // optional cursor: return items after this ID (keyset pagination)
}

const defaultListLimit = 10000

// AccessGroup represents a group that can be granted access to targets.
type AccessGroup struct {
	ID   GroupID
	Name string
}

// AccessGroupStore defines the behavior required for managing access groups
// and their relationships to users and targets.
type AccessGroupStore interface {
	Create(ctx context.Context, id GroupID, name string) (*AccessGroup, error)
	Get(ctx context.Context, id GroupID) (*AccessGroup, error)
	AddUserToGroup(ctx context.Context, userID UserID, groupID GroupID) error
	RemoveUserFromGroup(ctx context.Context, userID UserID, groupID GroupID) error
	UserIDsForGroup(ctx context.Context, groupID GroupID, opts *ListOpts) ([]UserID, error)
	AddTargetToGroup(ctx context.Context, groupID GroupID, targetID TargetID) error
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
