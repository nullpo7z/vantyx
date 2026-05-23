package proxyerrors

import (
	"errors"
	"net"
	"os"
	"strings"
)

// UserFacingError wraps a dial/bridge error with a short hint for terminal UI.
type UserFacingError struct {
	Err     error
	Message string
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
		return &UserFacingError{
			Err:     err,
			Message: "TCP connection to " + proto + " timed out. Check that the service is enabled, firewall/ACL/port settings, and network path from Vantyx to the target.",
		}
	case strings.Contains(lower, "connection refused"):
		return &UserFacingError{
			Err:     err,
			Message: "Connection to " + proto + " was refused. Check that the service is running and the port number is correct.",
		}
	case strings.Contains(lower, "no route to host"):
		return &UserFacingError{
			Err:     err,
			Message: "No route to " + proto + ". Check IP address, VLAN, and routing.",
		}
	case strings.Contains(lower, "network is unreachable"):
		return &UserFacingError{
			Err:     err,
			Message: "Network is unreachable. Check the path from the Vantyx server to the target.",
		}
	default:
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return &UserFacingError{
				Err:     err,
				Message: "TCP connection to " + proto + " timed out. Check that the service is enabled, firewall/ACL/port settings, and network path from Vantyx to the target.",
			}
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return &UserFacingError{
				Err:     err,
				Message: "TCP connection to " + proto + " timed out. Check that the service is enabled, firewall/ACL/port settings, and network path from Vantyx to the target.",
			}
		}
		return &UserFacingError{
			Err:     err,
			Message: "Failed to connect to " + proto + ": " + msg + " (check host, port, and that the service is running).",
		}
	}
}

// BridgeErrorMessage returns the user-facing message for bridge errors, if wrapped.
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
