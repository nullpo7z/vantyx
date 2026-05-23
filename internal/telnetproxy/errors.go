package telnetproxy

import "github.com/nullpo7z/vantyx/internal/proxyerrors"

// UserFacingError is an alias for proxyerrors.UserFacingError (Telnet dial failures).
type UserFacingError = proxyerrors.UserFacingError

// WrapDialError returns a user-facing error for Telnet TCP dial failures.
func WrapDialError(err error) error {
	return proxyerrors.WrapTCPDialError("Telnet", err)
}
