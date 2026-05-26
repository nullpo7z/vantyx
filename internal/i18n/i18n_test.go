package i18n

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestNormalise(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Locale
	}{
		{"", LocaleDefault},
		{"en", LocaleEnglish},
		{"EN", LocaleEnglish},
		{" ja ", LocaleJapanese},
		{"fr", LocaleDefault},
		{"jp", LocaleDefault},
	} {
		if got := Normalise(tc.in); got != tc.want {
			t.Errorf("Normalise(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsSupported(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"en", true},
		{"ja", true},
		{"EN", true},
		{" ja ", true},
		{"", false},
		{"fr", false},
	} {
		if got := IsSupported(tc.in); got != tc.want {
			t.Errorf("IsSupported(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseAcceptLanguage(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   Locale
	}{
		{"", LocaleDefault},
		{"ja", LocaleJapanese},
		{"JA-JP", LocaleJapanese},
		{"en-US,en;q=0.8", LocaleEnglish},
		{"fr-FR,en;q=0.5,ja;q=0.9", LocaleJapanese},
		{"fr-FR,de", LocaleDefault},
		{"ja-JP, en-US;q=0.7, *;q=0.1", LocaleJapanese},
	} {
		if got := ParseAcceptLanguage(tc.header); got != tc.want {
			t.Errorf("ParseAcceptLanguage(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestWithAndFromContext(t *testing.T) {
	ctx := context.Background()
	if got := FromContext(ctx); got != LocaleDefault {
		t.Fatalf("default context: got %q, want %q", got, LocaleDefault)
	}
	// FromContext is documented to tolerate a nil ctx; this test pins
	// that contract so a future refactor does not accidentally panic
	// when callers from unusual code paths supply nil.
	//nolint:staticcheck // SA1012: intentional nil-ctx robustness check.
	if got := FromContext(nil); got != LocaleDefault {
		t.Fatalf("nil context: got %q, want %q", got, LocaleDefault)
	}
	ctx2 := WithLocale(ctx, LocaleJapanese)
	if got := FromContext(ctx2); got != LocaleJapanese {
		t.Fatalf("ja context: got %q, want %q", got, LocaleJapanese)
	}
	ctx3 := WithLocale(ctx, "FR")
	if got := FromContext(ctx3); got != LocaleDefault {
		t.Fatalf("unsupported context: got %q, want %q (should fall back)", got, LocaleDefault)
	}
}

func TestT_FallbackChain(t *testing.T) {
	if got := T(LocaleJapanese, "common.unauthorized"); got == "" || got == "common.unauthorized" {
		t.Fatalf("expected Japanese translation, got %q", got)
	}
	if got := T(LocaleEnglish, "common.unauthorized"); got != "unauthorized" {
		t.Fatalf("English: got %q", got)
	}
	if got := T(Locale("fr"), "common.unauthorized"); got != "unauthorized" {
		t.Fatalf("unknown locale should fall back to English, got %q", got)
	}
	if got := T(LocaleEnglish, "no.such.key"); got != "no.such.key" {
		t.Fatalf("missing key should be returned literally, got %q", got)
	}
}

func TestT_Interpolation(t *testing.T) {
	// Use a deliberately ad-hoc template by reaching into the
	// catalog map – this guards against placeholder parsing
	// regressing without depending on a real key.
	catalog[LocaleEnglish]["test.greeting"] = "Hello {name}, you have {count} messages"
	defer delete(catalog[LocaleEnglish], "test.greeting")

	got := T(LocaleEnglish, "test.greeting", "name", "Alice", "count", 3)
	want := "Hello Alice, you have 3 messages"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// Odd number of vars: trailing key is dropped silently.
	got2 := T(LocaleEnglish, "test.greeting", "name", "Bob", "count")
	want2 := "Hello Bob, you have {count} messages"
	if got2 != want2 {
		t.Fatalf("odd vars: got %q, want %q", got2, want2)
	}
}

func TestTR(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if got := TR(req, "common.unauthorized"); got != "unauthorized" {
		t.Fatalf("no-context request defaults to English, got %q", got)
	}
	req2 := req.Clone(WithLocale(req.Context(), LocaleJapanese))
	got := TR(req2, "common.unauthorized")
	if got == "" || got == "common.unauthorized" || got == "unauthorized" {
		t.Fatalf("expected Japanese translation, got %q", got)
	}
	if got := TR(nil, "common.unauthorized"); got != "unauthorized" {
		t.Fatalf("nil request falls back to default, got %q", got)
	}
}

func TestCatalogs_AllEnKeysHaveJaTranslation(t *testing.T) {
	en := catalog[LocaleEnglish]
	ja := catalog[LocaleJapanese]
	for key := range en {
		if _, ok := ja[key]; !ok {
			t.Errorf("missing Japanese translation for key %q", key)
		}
	}
}

func TestParseFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1", 1.0, true},
		{"0.9", 0.9, true},
		{"0.0", 0.0, true},
		{".5", 0.5, true},
		{"", 0, false},
		{"abc", 0, false},
		{"1.2.3", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseFloat(tc.in)
		if ok != tc.ok {
			t.Errorf("parseFloat(%q): ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("parseFloat(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
