package recording

import (
	"path/filepath"
	"strings"
)

// SanitizeSessionID replaces characters that are unsafe in file names.
func SanitizeSessionID(sessionID string) string {
	s := strings.ReplaceAll(sessionID, ":", "-")
	return strings.ReplaceAll(s, ".", "-")
}

// WebMPath returns the recording file path for a session ID under dir.
func WebMPath(dir, sessionID string) string {
	return filepath.Join(dir, SanitizeSessionID(sessionID)+".webm")
}

// CastPath returns the asciinema recording file path for a session ID under dir.
func CastPath(dir, sessionID string) string {
	return filepath.Join(dir, SanitizeSessionID(sessionID)+".cast")
}

// IsWebMPath reports whether path points to a WebM screen recording.
func IsWebMPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".webm")
}
