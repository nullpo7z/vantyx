package access

import "errors"

// AccessGroup represents a group that can be granted access to targets.
type AccessGroup struct {
	ID   string
	Name string
}

// AccessGroupStore defines the behavior required for managing access groups
// and their relationships to users and targets.
type AccessGroupStore interface {
	Create(id, name string) (*AccessGroup, error)
	Get(id string) (*AccessGroup, error)
	AddUserToGroup(userID, groupID string) error
	AddTargetToGroup(groupID, targetID string) error
	GroupIDsForUser(userID string) []string
	TargetIDsForGroup(groupID string) []string
	TargetIDsForUser(userID string) []string
}

var (
	ErrGroupExists   = errors.New("access group already exists")
	ErrGroupNotFound = errors.New("access group not found")
)
