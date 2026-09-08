package access

import (
	"errors"
	"strings"
)

// ErrCredentialSourceExclusive is returned when both credential_identity_id and ssh_key_id are set.
var ErrCredentialSourceExclusive = errors.New("credential identity and ssh key cannot both be set")

// validateCredentialLibraryID checks IDs for ssh_keys and credential_identities
// rows. tooLongErr/invalidErr let each caller report a validation failure
// against its own field name (e.g. "ssh_key_id") instead of the generic
// target-ID errors that used to be hardcoded here -- a copy-paste leftover
// that made an invalid SSH key ID or credential identity ID surface as
// "target_id に使用できない文字が含まれています" in the UI.
func validateCredentialLibraryID(id string, emptyErr, tooLongErr, invalidErr error) error {
	s := strings.TrimSpace(id)
	if s == "" {
		return emptyErr
	}
	if len(s) > maxIDLen {
		return tooLongErr
	}
	if !idPattern.MatchString(s) {
		return invalidErr
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
