package access

import "errors"

// Validation sentinels returned by the package-internal validate*
// helpers. They are exported so HTTP handlers can use [errors.Is] to
// map them onto localized response bodies without parsing English
// `errors.New(...)` strings (i18n-residual PR 3).
//
// The English text on the underlying [errors.New] values is preserved
// as a fallback for logs and tests: clients receive a localized
// message when the request originates from the HTTP layer, but plain
// `Error()` output remains identical to the pre-refactor wording.
var (
	// Group ID validation.
	ErrGroupIDEmpty   = errors.New("group id must not be empty")
	ErrGroupIDTooLong = errors.New("group id too long")
	ErrGroupIDInvalid = errors.New("group id contains invalid characters")

	// Target ID validation.
	ErrTargetIDEmpty   = errors.New("target id must not be empty")
	ErrTargetIDTooLong = errors.New("target id too long")
	ErrTargetIDInvalid = errors.New("target id contains invalid characters")

	// Display name validation.
	ErrNameEmpty   = errors.New("name must not be empty")
	ErrNameTooLong = errors.New("name too long")
	ErrNameInvalid = errors.New("name contains invalid characters")

	// Host validation.
	ErrHostEmpty   = errors.New("host must not be empty")
	ErrHostTooLong = errors.New("host too long")
	ErrHostInvalid = errors.New("host must be a valid hostname or IP address")

	// Protocol whitelist.
	ErrProtocolInvalid = errors.New("protocol must be ssh, telnet, vnc, tftp, ftp, or rdp")

	// Tag validation (shared by group / target tags here and re-used by
	// `internal/auth` for user tags so handlers can match against a
	// single sentinel pair).
	ErrTagLength = errors.New("tag must be 1–64 characters")
	ErrTagChars  = errors.New("tag may only contain letters, numbers, hyphen, underscore")
)
