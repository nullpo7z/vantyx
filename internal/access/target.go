package access

import "errors"

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
	// Path is an optional hierarchical folder path (e.g. "prod/network").
	// Empty means root.
	Path string
}

// TargetStore defines the behavior required for managing targets.
type TargetStore interface {
	Create(id, name, host string, port uint16, protocol Protocol) (*Target, error)
	CreateWithPath(id, name, host string, port uint16, protocol Protocol, path string) (*Target, error)
	Get(id string) (*Target, error)
	ListByIDs(ids []string) []*Target
}

var (
	ErrTargetExists   = errors.New("target already exists")
	ErrTargetNotFound = errors.New("target not found")
)
