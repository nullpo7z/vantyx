// Package i18n localizes HTTP API error messages for the Vantyx
// backend.
//
// Design goals:
//   - Keep the surface small: handlers call [T] with a stable key and
//     an [http.Request] (or [context.Context]). Locale resolution is
//     hidden in the request context.
//   - Stay backwards compatible: legacy `writeJSONError(w, "literal",
//     code)` call sites continue to work unchanged. The new
//     `writeJSONErrorKey(w, r, key, code, ...)` wrapper in the
//     httpapi package delegates here.
//   - Be dependency-free of the HTTP layer so the catalogs can be
//     reused by other surfaces (CLI gateway, scripts) later.
//
// Currently supported locales: English (default) and Japanese.
// Unsupported codes fall back to English, then to the literal key.
package i18n

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	enLocale "github.com/nullpo7z/vantyx/internal/i18n/locales/en"
	jaLocale "github.com/nullpo7z/vantyx/internal/i18n/locales/ja"
)

// Locale is a short BCP 47-style language code (e.g. "en", "ja").
// An empty string is treated as the default locale.
type Locale string

// Supported locales bundled with the binary.
const (
	LocaleEnglish  Locale = "en"
	LocaleJapanese Locale = "ja"
	// LocaleDefault is used when no locale could be resolved.
	LocaleDefault = LocaleEnglish
)

// catalog maps a locale code to its flat key→template dictionary.
// Templates use `{name}` style placeholders that [interpolate] resolves
// against the variadic key/value pairs passed to [T].
var catalog = map[Locale]map[string]string{
	LocaleEnglish:  enLocale.Messages,
	LocaleJapanese: jaLocale.Messages,
}

// IsSupported reports whether the given locale code has a dictionary
// shipped with the binary.
func IsSupported(loc string) bool {
	_, ok := catalog[Locale(strings.ToLower(strings.TrimSpace(loc)))]
	return ok
}

// Normalise returns the canonical Locale for the supplied code, or
// [LocaleDefault] when the code is empty or unsupported. The check is
// case-insensitive and trims surrounding whitespace, mirroring how
// browsers tend to send Accept-Language fragments.
func Normalise(loc string) Locale {
	canon := Locale(strings.ToLower(strings.TrimSpace(loc)))
	if _, ok := catalog[canon]; ok {
		return canon
	}
	return LocaleDefault
}

type ctxKey struct{}

// WithLocale returns a new context that carries the supplied locale.
// Middleware should call this once per request after resolving the
// user's preference.
func WithLocale(ctx context.Context, loc Locale) context.Context {
	return context.WithValue(ctx, ctxKey{}, Normalise(string(loc)))
}

// FromContext returns the locale stored in ctx, or [LocaleDefault] when
// none is set. Safe to call on any context (the zero value is the
// default locale).
func FromContext(ctx context.Context) Locale {
	if ctx == nil {
		return LocaleDefault
	}
	if v, ok := ctx.Value(ctxKey{}).(Locale); ok && v != "" {
		return v
	}
	return LocaleDefault
}

// ParseAcceptLanguage picks the best supported locale from an
// Accept-Language header value (RFC 9110). Quality factors are
// honored and unknown languages are skipped. Returns LocaleDefault
// when nothing matches (including an empty header).
//
// This is intentionally lightweight: enough to honor `ja`, `ja-JP`,
// `en;q=0.5, ja;q=0.9`, but not a full RFC implementation.
func ParseAcceptLanguage(header string) Locale {
	if header == "" {
		return LocaleDefault
	}
	type candidate struct {
		loc Locale
		q   float64
	}
	var best candidate
	for _, raw := range strings.Split(header, ",") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag := strings.TrimSpace(part[:i])
			// Cheap q-value parser: only recognizes `q=` parameters.
			for _, kv := range strings.Split(part[i+1:], ";") {
				kv = strings.TrimSpace(kv)
				if strings.HasPrefix(kv, "q=") {
					if parsed, ok := parseFloat(kv[2:]); ok {
						q = parsed
					}
				}
			}
			part = tag
		}
		// Honor the primary subtag (e.g. "ja-JP" → "ja").
		if i := strings.Index(part, "-"); i > 0 {
			part = part[:i]
		}
		loc := Locale(strings.ToLower(part))
		if _, ok := catalog[loc]; !ok {
			continue
		}
		if best.loc == "" || q > best.q {
			best = candidate{loc: loc, q: q}
		}
	}
	if best.loc == "" {
		return LocaleDefault
	}
	return best.loc
}

// parseFloat parses a non-negative float without bringing in strconv's
// full surface; used by [ParseAcceptLanguage] for q-values like "0.9".
func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var (
		out       float64
		frac      float64 = 1
		seenDot   bool
		seenDigit bool
	)
	for _, r := range s {
		switch {
		case r == '.' && !seenDot:
			seenDot = true
		case r >= '0' && r <= '9':
			seenDigit = true
			if seenDot {
				frac /= 10
				out += float64(r-'0') * frac
			} else {
				out = out*10 + float64(r-'0')
			}
		default:
			return 0, false
		}
	}
	if !seenDigit {
		return 0, false
	}
	return out, true
}

// T looks up a message key in the requested locale and substitutes
// placeholders from vars. Vars must be supplied as alternating
// name/value pairs; odd counts are tolerated by dropping the trailing
// key. Lookup order is locale → English → the key itself.
//
// Callers in the HTTP layer normally use the [TR] convenience that
// reads the locale from the request context.
func T(loc Locale, key string, vars ...any) string {
	msg := lookup(loc, key)
	if msg == "" {
		msg = lookup(LocaleDefault, key)
	}
	if msg == "" {
		return key
	}
	return interpolate(msg, vars)
}

// TR is the request-aware shortcut: it resolves the locale from the
// request context.
func TR(r *http.Request, key string, vars ...any) string {
	if r == nil {
		return T(LocaleDefault, key, vars...)
	}
	return T(FromContext(r.Context()), key, vars...)
}

// TC mirrors [TR] for code paths that only have a context.
func TC(ctx context.Context, key string, vars ...any) string {
	return T(FromContext(ctx), key, vars...)
}

func lookup(loc Locale, key string) string {
	dict, ok := catalog[loc]
	if !ok {
		return ""
	}
	return dict[key]
}

func interpolate(template string, vars []any) string {
	if len(vars) < 2 {
		return template
	}
	out := template
	for i := 0; i+1 < len(vars); i += 2 {
		name, ok := vars[i].(string)
		if !ok {
			continue
		}
		placeholder := "{" + name + "}"
		if !strings.Contains(out, placeholder) {
			continue
		}
		out = strings.ReplaceAll(out, placeholder, toString(vars[i+1]))
	}
	return out
}

// toString avoids pulling in fmt for the common case (string/int) to
// keep the hot path light. Falls back to fmt-style formatting for the
// rare struct argument.
func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int:
		return itoa(int64(x))
	case int64:
		return itoa(x)
	case error:
		if x == nil {
			return ""
		}
		return x.Error()
	default:
		return sprint(v)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// sprint is used as a fallback formatter for unusual placeholder
// argument types (e.g. structs supplied by tests).
func sprint(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(interface{ String() string }); ok {
		return s.String()
	}
	return fmt.Sprint(v)
}
