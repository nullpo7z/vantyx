package httpapi

import "testing"

func TestSanitizeTransferErrorMessage(t *testing.T) {
	got := sanitizeTransferErrorMessage(`"><script>alert(1)</script>`)
	if got == "" || got == `"><script>alert(1)</script>` {
		t.Fatalf("expected sanitized message, got %q", got)
	}
	long := stringsRepeat("a", 300)
	got = sanitizeTransferErrorMessage(long)
	if len(got) > 250 {
		t.Fatalf("expected truncation, len=%d", len(got))
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, s...)
	}
	return string(out[:n])
}
