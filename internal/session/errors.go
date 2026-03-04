package session

import "errors"

var (
	// ErrSessionExists indicates the requested session ID is already in use.
	ErrSessionExists = errors.New("session already exists")
)
