package access

import (
	"errors"
	"sync"
)

// Protocol is the connection protocol for a target.
type Protocol string

const (
	ProtocolSSH    Protocol = "ssh"
	ProtocolTelnet Protocol = "telnet"
)

// Target represents a device that users can connect to via SSH or Telnet.
type Target struct {
	ID       string
	Name     string
	Host     string
	Port     uint16
	Protocol Protocol
}

// InMemoryTargetStore is a thread-safe in-memory store for targets.
type InMemoryTargetStore struct {
	mu   sync.RWMutex
	byID map[string]*Target
}

var (
	ErrTargetExists   = errors.New("target already exists")
	ErrTargetNotFound = errors.New("target not found")
)

// NewInMemoryTargetStore creates an empty target store.
func NewInMemoryTargetStore() *InMemoryTargetStore {
	return &InMemoryTargetStore{
		byID: make(map[string]*Target),
	}
}

// Create inserts a new target.
func (s *InMemoryTargetStore) Create(id, name, host string, port uint16, protocol Protocol) (*Target, error) {
	if id == "" || name == "" || host == "" {
		return nil, errors.New("id, name and host must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byID[id]; ok {
		return nil, ErrTargetExists
	}

	t := &Target{
		ID:       id,
		Name:     name,
		Host:     host,
		Port:     port,
		Protocol: protocol,
	}
	s.byID[id] = t
	return t, nil
}

// Get returns a target by ID.
func (s *InMemoryTargetStore) Get(id string) (*Target, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.byID[id]
	if !ok {
		return nil, ErrTargetNotFound
	}
	return t, nil
}

// ListByIDs returns targets for the given IDs; missing IDs are skipped.
func (s *InMemoryTargetStore) ListByIDs(ids []string) []*Target {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*Target
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if t, ok := s.byID[id]; ok {
			out = append(out, t)
		}
	}
	return out
}
