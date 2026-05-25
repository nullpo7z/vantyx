// Package recording writes interactive terminal sessions to disk in
// asciinema v2 cast format.
//
// The writer is line-oriented and thread-safe: each chunk is timestamped
// against the cast start time and appended as a JSON record. Recordings
// are written only when [internal/httpapi] is configured with a
// recordings directory (VANTYX_RECORDINGS_DIR); the package itself does
// not consult the environment.
package recording
