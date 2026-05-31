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

func TestIsMP4Path(t *testing.T) {
	if !IsMP4Path("/app/recordings/sess.mp4") {
		t.Fatal("expected .mp4")
	}
	if IsMP4Path("/app/recordings/sess.cast") {
		t.Fatal("expected not mp4")
	}
}

func TestStorageFormat(t *testing.T) {
	if StorageFormat("/rec/a.mp4") != "mp4" {
		t.Fatal("expected mp4")
	}
	if StorageFormat("/rec/a.cast") != "cast" {
		t.Fatal("expected cast")
	}
	if StorageFormat("/rec/a.webm") != "" {
		t.Fatal("webm is not a supported storage format")
	}
}

func TestMP4Path(t *testing.T) {
	got := MP4Path("/rec", "abc:def")
	want := filepath.Join("/rec", "abc-def.mp4")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
