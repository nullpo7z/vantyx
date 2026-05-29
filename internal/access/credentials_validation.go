package access

import (
	"errors"
	"strings"
)

// ErrCredentialSourceExclusive is returned when both credential_identity_id and ssh_key_id are set.
var ErrCredentialSourceExclusive = errors.New("credential identity and ssh key cannot both be set")

// validateCredentialLibraryID checks IDs for ssh_keys and credential_identities rows.
func validateCredentialLibraryID(id string, emptyErr error) error {
	s := strings.TrimSpace(id)
	if s == "" {
		return emptyErr
	}
	if len(s) > maxIDLen {
		return ErrTargetIDTooLong
	}
	if !idPattern.MatchString(s) {
		return ErrTargetIDInvalid
	}
	return nil
}

// validateCredentialLibraryLabel checks human-readable labels in the credential library.
func validateCredentialLibraryLabel(label string, emptyErr error) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return emptyErr
	}
	if len(label) > maxNameLen {
		return ErrNameTooLong
	}
	for _, r := range label {
		if r != '\t' && isControlLike(r) {
			return ErrNameInvalid
		}
	}
	return nil
}

// isControlLike matches target name validation: reject control characters.
func isControlLike(r rune) bool {
	return r < 0x20 || r == 0x7f
}
