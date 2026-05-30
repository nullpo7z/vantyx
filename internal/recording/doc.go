// Package recording writes interactive terminal sessions to disk in
// asciinema v2 cast format and RDP/VNC screen sessions as WebM video.
//
// Terminal cast writers are line-oriented and thread-safe. Screen capture
// uses ffmpeg x11grab (RDP via the bridge X display, VNC via Xvfb +
// vncviewer). Recordings are written only when [internal/httpapi] is
// configured with a recordings directory (VANTYX_RECORDINGS_DIR).
package recording
