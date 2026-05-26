package auth

import "errors"

// Validation sentinels returned by user CRUD / tag helpers. Handlers
// use [errors.Is] to map these onto localized HTTP responses without
// parsing the English text below.
//
// The wording mirrors the matching `internal/access.Err*` sentinels so
// the response bodies remain identical across the two packages; the
// values are intentionally kept here (rather than re-exported from
// `access`) to avoid an import cycle with `internal/access/*_test.go`.
var (
	// ErrIDOrUsernameEmpty is returned by CreateUser when the supplied
	// `id` or `username` is empty after trimming.
	ErrIDOrUsernameEmpty = errors.New("id and username must not be empty")

	// ErrTagLength is returned by validateUserTag when the tag length
	// is outside the 1–64 character range.
	ErrTagLength = errors.New("tag must be 1–64 characters")
	// ErrTagChars is returned by validateUserTag when the tag contains
	// characters other than [A-Za-z0-9_-].
	ErrTagChars = errors.New("tag may only contain letters, numbers, hyphen, underscore")
)
