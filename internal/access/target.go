package access

import (
	"context"
	"errors"
)

// Protocol is the connection protocol for a target.
type Protocol string

const (
	ProtocolSSH    Protocol = "ssh"
	ProtocolTelnet Protocol = "telnet"
)

// Target represents a device that users can connect to via SSH or Telnet.
type Target struct {
	ID       TargetID
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
	Create(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol) (*Target, error)
	CreateWithPath(ctx context.Context, id TargetID, name, host string, port uint16, protocol Protocol, groupID GroupID, path string) (*Target, error)
	Get(ctx context.Context, id TargetID) (*Target, error)
	ListByIDs(ctx context.Context, ids []TargetID, opts *ListOpts) ([]*Target, error)
}

var (
	ErrTargetExists   = errors.New("target already exists")
	ErrTargetNotFound = errors.New("target not found")
)
