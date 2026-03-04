package access

import (
	"errors"
	"sync"
)

// AccessGroup represents a group that can be granted access to targets.
type AccessGroup struct {
	ID   string
	Name string
}

// InMemoryAccessGroupStore stores groups and user->groups / group->targets membership.
type InMemoryAccessGroupStore struct {
	mu           sync.RWMutex
	byID         map[string]*AccessGroup
	userGroups   map[string]map[string]struct{} // userID -> set of groupID
	groupTargets map[string]map[string]struct{} // groupID -> set of targetID
}

var (
	ErrGroupExists   = errors.New("access group already exists")
	ErrGroupNotFound = errors.New("access group not found")
)

// NewInMemoryAccessGroupStore creates an empty access group store.
func NewInMemoryAccessGroupStore() *InMemoryAccessGroupStore {
	return &InMemoryAccessGroupStore{
		byID:         make(map[string]*AccessGroup),
		userGroups:   make(map[string]map[string]struct{}),
		groupTargets: make(map[string]map[string]struct{}),
	}
}

// Create creates a new access group.
func (s *InMemoryAccessGroupStore) Create(id, name string) (*AccessGroup, error) {
	if id == "" || name == "" {
		return nil, errors.New("id and name must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[id]; ok {
		return nil, ErrGroupExists
	}

	g := &AccessGroup{ID: id, Name: name}
	s.byID[id] = g
	s.groupTargets[id] = make(map[string]struct{})
	return g, nil
}

// AddUserToGroup adds a user to an access group.
func (s *InMemoryAccessGroupStore) AddUserToGroup(userID, groupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[groupID]; !ok {
		return ErrGroupNotFound
	}
	if s.userGroups[userID] == nil {
		s.userGroups[userID] = make(map[string]struct{})
	}
	s.userGroups[userID][groupID] = struct{}{}
	return nil
}

// AddTargetToGroup grants the group access to the target.
func (s *InMemoryAccessGroupStore) AddTargetToGroup(groupID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[groupID]; !ok {
		return ErrGroupNotFound
	}
	if s.groupTargets[groupID] == nil {
		s.groupTargets[groupID] = make(map[string]struct{})
	}
	s.groupTargets[groupID][targetID] = struct{}{}
	return nil
}

// TargetIDsForUser returns the set of target IDs the user can access via any of their groups.
func (s *InMemoryAccessGroupStore) TargetIDsForUser(userID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	groupIDs := s.userGroups[userID]
	if len(groupIDs) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	for gid := range groupIDs {
		for tid := range s.groupTargets[gid] {
			seen[tid] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for tid := range seen {
		out = append(out, tid)
	}
	return out
}
