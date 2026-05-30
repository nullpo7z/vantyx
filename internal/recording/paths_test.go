package recording

import (
	"path/filepath"
	"testing"
)

func TestSanitizeSessionID(t *testing.T) {
	got := SanitizeSessionID("2026-05-30T08:00:00.123456789Z")
	want := "2026-05-30T08-00-00-123456789Z"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIsWebMPath(t *testing.T) {
	if !IsWebMPath("/app/recordings/sess.webm") {
		t.Fatal("expected .webm")
	}
	if IsWebMPath("/app/recordings/sess.cast") {
		t.Fatal("expected not webm")
	}
}

func TestWebMPath(t *testing.T) {
	got := WebMPath("/rec", "abc:def")
	want := filepath.Join("/rec", "abc-def.webm")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
