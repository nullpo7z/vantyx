package access

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateSentinels pins the typed sentinels returned by the
// package-internal validate* helpers so the HTTP layer can use
// [errors.Is] to map them to localized responses.
func TestValidateSentinels(t *testing.T) {
	cases := []struct {
		name    string
		got     error
		wantErr error
	}{
		// validateGroupID
		{"groupID empty", validateGroupID(""), ErrGroupIDEmpty},
		{"groupID too long", validateGroupID(GroupID(strings.Repeat("a", maxIDLen+1))), ErrGroupIDTooLong},
		{"groupID invalid char", validateGroupID("bad id"), ErrGroupIDInvalid},
		{"groupID dot segment", validateGroupID("a/./b"), ErrGroupIDInvalid},

		// validateTargetID
		{"targetID empty", validateTargetID(""), ErrTargetIDEmpty},
		{"targetID too long", validateTargetID(TargetID(strings.Repeat("a", maxIDLen+1))), ErrTargetIDTooLong},
		{"targetID invalid char", validateTargetID("bad id"), ErrTargetIDInvalid},

		// validateName
		{"name empty", validateName(""), ErrNameEmpty},
		{"name too long", validateName(strings.Repeat("a", maxNameLen+1)), ErrNameTooLong},
		{"name control char", validateName("a\x00b"), ErrNameInvalid},

		// validateHost
		{"host empty", validateHost(""), ErrHostEmpty},
		{"host too long", validateHost(strings.Repeat("a", maxHostLen+1)), ErrHostTooLong},
		{"host invalid", validateHost("-bad"), ErrHostInvalid},

		// validateProtocol
		{"protocol invalid", validateProtocol("unsupported"), ErrProtocolInvalid},

		// validateTag
		{"tag empty", validateTag(""), ErrTagLength},
		{"tag too long", validateTag(strings.Repeat("a", maxTagLen+1)), ErrTagLength},
		{"tag invalid char", validateTag("bad tag"), ErrTagChars},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.got, tc.wantErr) {
				t.Fatalf("got %v, want errors.Is = %v", tc.got, tc.wantErr)
			}
		})
	}
}

// TestValidateOK confirms valid inputs do not return the sentinels.
func TestValidateOK(t *testing.T) {
	if err := validateGroupID("ok"); err != nil {
		t.Fatalf("validateGroupID: %v", err)
	}
	if err := validateGroupID("parent/child"); err != nil {
		t.Fatalf("validateGroupID hierarchical: %v", err)
	}
	if err := validateTargetID("t1"); err != nil {
		t.Fatalf("validateTargetID: %v", err)
	}
	if err := validateName("My Target"); err != nil {
		t.Fatalf("validateName: %v", err)
	}
	if err := validateHost("example.com"); err != nil {
		t.Fatalf("validateHost: %v", err)
	}
	if err := validateHost("192.0.2.1"); err != nil {
		t.Fatalf("validateHost ip: %v", err)
	}
	if err := validateProtocol(ProtocolSSH); err != nil {
		t.Fatalf("validateProtocol: %v", err)
	}
	if err := validateTag("ok_tag-1"); err != nil {
		t.Fatalf("validateTag: %v", err)
	}
}
