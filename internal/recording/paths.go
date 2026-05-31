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

// MP4Path returns the screen recording file path for a session ID under dir.
func MP4Path(dir, sessionID string) string {
	return filepath.Join(dir, SanitizeSessionID(sessionID)+".mp4")
}

// CastPath returns the asciinema recording file path for a session ID under dir.
func CastPath(dir, sessionID string) string {
	return filepath.Join(dir, SanitizeSessionID(sessionID)+".cast")
}

// IsMP4Path reports whether path points to an MP4 screen recording.
func IsMP4Path(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".mp4")
}

// StorageFormat returns "cast", "mp4", or "" for a recording file path.
func StorageFormat(path string) string {
	if IsMP4Path(path) {
		return "mp4"
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".cast") {
		return "cast"
	}
	return ""
}
