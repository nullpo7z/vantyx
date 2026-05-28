package proxyerrors

import (
	"errors"
	"net"
	"os"
	"strings"
)

// UserFacingError wraps a dial/bridge error with a short hint for
// terminal UI. The optional MessageKey / Vars pair lets HTTP handlers
// look up a localized template in the i18n catalog instead of the
// pre-formatted English Message string.
//
// New code paths should populate MessageKey + Vars (the English text
// in Message becomes a fallback for callers that aren't request-aware,
// such as the CLI menu and non-HTTP loggers).
type UserFacingError struct {
	Err     error
	Message string
	// MessageKey is the i18n catalog key (e.g. "proxy.tcpTimeout"). If
	// empty, no localization is available and callers fall back to
	// Message.
	MessageKey string
	// Vars are the key/value substitution pairs passed to
	// i18n.TR(r, MessageKey, Vars...). The slice is laid out as
	// alternating string keys and any-typed values, matching the
	// existing i18n.T variadic API.
	Vars []any
}

func (e *UserFacingError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Err.Error()
}

func (e *UserFacingError) Unwrap() error { return e.Err }

// WrapTCPDialError returns a user-facing error for TCP dial failures to a remote terminal target.
func WrapTCPDialError(proto string, err error) error {
	if err == nil {
		return nil
	}
	if proto == "" {
		proto = "target"
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "timeout"):
		return newTCPTimeout(err, proto)
	case strings.Contains(lower, "connection refused"):
		return &UserFacingError{
			Err:        err,
			Message:    "Connection to " + proto + " was refused. Check that the service is running and the port number is correct.",
			MessageKey: "proxy.connectionRefused",
			Vars:       []any{"proto", proto},
		}
	case strings.Contains(lower, "no route to host"):
		return &UserFacingError{
			Err:        err,
			Message:    "No route to " + proto + ". Check IP address, VLAN, and routing.",
			MessageKey: "proxy.noRoute",
			Vars:       []any{"proto", proto},
		}
	case strings.Contains(lower, "network is unreachable"):
		return &UserFacingError{
			Err:        err,
			Message:    "Network is unreachable. Check the path from the Vantyx server to the target.",
			MessageKey: "proxy.networkUnreachable",
		}
	default:
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return newTCPTimeout(err, proto)
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return newTCPTimeout(err, proto)
		}
		return &UserFacingError{
			Err:        err,
			Message:    "Failed to connect to " + proto + ". Check host, port, and that the service is running.",
			MessageKey: "proxy.dialFailed",
			Vars:       []any{"proto", proto},
		}
	}
}

// newTCPTimeout constructs a UserFacingError for any of the timeout
// branches in [WrapTCPDialError]. Centralizing the body keeps the
// English fallback and the i18n key/vars in sync.
func newTCPTimeout(err error, proto string) *UserFacingError {
	return &UserFacingError{
		Err:        err,
		Message:    "TCP connection to " + proto + " timed out. Check that the service is enabled, firewall/ACL/port settings, and network path from Vantyx to the target.",
		MessageKey: "proxy.tcpTimeout",
		Vars:       []any{"proto", proto},
	}
}

// BridgeErrorMessage returns the user-facing English message for
// bridge errors, if wrapped. Prefer [BridgeErrorKey] when the response
// should respect the caller's locale.
func BridgeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var ufe *UserFacingError
	if errors.As(err, &ufe) {
		return ufe.Error()
	}
	return err.Error()
}

// BridgeErrorKey returns the i18n catalog key and substitution vars
// attached to a [UserFacingError], so handlers can call
// `i18n.TR(r, key, vars...)` for a localized response. The boolean
// reports whether the error was a UserFacingError with a populated
// MessageKey; callers should fall back to [BridgeErrorMessage] when it
// is false (e.g. for plain network errors that did not flow through
// [WrapTCPDialError]).
func BridgeErrorKey(err error) (key string, vars []any, ok bool) {
	if err == nil {
		return "", nil, false
	}
	var ufe *UserFacingError
	if errors.As(err, &ufe) && ufe.MessageKey != "" {
		return ufe.MessageKey, ufe.Vars, true
	}
	return "", nil, false
}

// UnwrapForAudit returns the underlying error string for audit logs when a UserFacingError is used.
func UnwrapForAudit(err error) string {
	if err == nil {
		return ""
	}
	var ufe *UserFacingError
	if errors.As(err, &ufe) && ufe.Err != nil {
		return ufe.Err.Error()
	}
	return err.Error()
}
