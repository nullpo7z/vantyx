package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nullpo7z/vantyx/internal/access"
	"github.com/nullpo7z/vantyx/internal/i18n"
)

// TestWriteAccessValidationError walks every branch of
// [writeAccessValidationError] to keep the coverage threshold met and
// to pin the i18n key mapping. The helper is invoked directly with a
// request whose context carries a Japanese locale so we can confirm
// the localized templates are looked up correctly.
func TestWriteAccessValidationError(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantSub string
	}{
		{"groupID empty", access.ErrGroupIDEmpty, "group_id"},
		{"groupID too long", access.ErrGroupIDTooLong, "group_id"},
		{"groupID invalid", access.ErrGroupIDInvalid, "group_id"},
		{"targetID empty", access.ErrTargetIDEmpty, "target_id"},
		{"targetID too long", access.ErrTargetIDTooLong, "target_id"},
		{"targetID invalid", access.ErrTargetIDInvalid, "target_id"},
		{"name empty", access.ErrNameEmpty, "name"},
		{"name too long", access.ErrNameTooLong, "name"},
		{"name invalid", access.ErrNameInvalid, "name"},
		{"host empty", access.ErrHostEmpty, "host"},
		{"host too long", access.ErrHostTooLong, "host"},
		{"host invalid", access.ErrHostInvalid, "host"},
		{"protocol invalid", access.ErrProtocolInvalid, "ssh"},
		{"tag length", access.ErrTagLength, "タグ"},
		{"tag chars", access.ErrTagChars, "タグ"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(i18n.WithLocale(req.Context(), i18n.LocaleJapanese))
			w := httptest.NewRecorder()
			if !writeAccessValidationError(w, req, tc.err) {
				t.Fatalf("writeAccessValidationError did not match %v", tc.err)
			}
			if w.Result().StatusCode != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400", w.Result().StatusCode)
			}
			if !strings.Contains(w.Body.String(), tc.wantSub) {
				t.Fatalf("body %q does not contain %q", w.Body.String(), tc.wantSub)
			}
		})
	}

	t.Run("unknown error returns false", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		if writeAccessValidationError(w, req, errors.New("some other error")) {
			t.Fatal("expected false for unknown error")
		}
		if w.Body.Len() != 0 {
			t.Fatalf("expected no response body, got %q", w.Body.String())
		}
	})
}
