package httpapi

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/filetransfer"
)

// fileTransferCursorSeparator separates the RFC3339Nano timestamp and
// the job id inside an opaque (base64) cursor. The "space pipe space"
// pair is unlikely to appear in either part.
const fileTransferCursorSeparator = " | "

// fileTransfersListResponse is the GET /api/file-transfers payload.
type fileTransfersListResponse struct {
	Items      []filetransfer.JobSnapshot `json:"items"`
	NextCursor string                     `json:"next_cursor,omitempty"`
}

// progressCopy copies from src to dst while updating job progress. It
// is the byte pump used by every file transfer runner so the SPA
// progress bar updates uniformly.
func progressCopy(job *filetransfer.Job, dst io.Writer, src io.Reader, total int64) error {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			written += int64(nw)
			job.SetProgress(written, total)
			if ew != nil {
				return ew
			}
			if nr != nw {
				return io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return nil
			}
			return er
		}
	}
}

// encodeFileTransferCursor returns a base64-encoded "updated_at|id"
// cursor used by the listing endpoint.
func encodeFileTransferCursor(updated time.Time, id string) string {
	if id == "" {
		return ""
	}
	raw := updated.UTC().Format(time.RFC3339Nano) + fileTransferCursorSeparator + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeFileTransferCursor parses a cursor produced by
// [encodeFileTransferCursor]. Invalid cursors yield a generic error so
// callers don't leak the cursor format to clients.
func decodeFileTransferCursor(s string) (time.Time, string, error) {
	if s == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid after_cursor")
	}
	parts := strings.SplitN(string(raw), fileTransferCursorSeparator, 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid after_cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid after_cursor")
	}
	return t.UTC(), parts[1], nil
}

// parseFileTransferStates parses repeated state query values into
// typed [filetransfer.State] slices.
func parseFileTransferStates(raw []string) ([]filetransfer.State, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]filetransfer.State, 0, len(raw))
	for _, v := range raw {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			switch filetransfer.State(part) {
			case filetransfer.StateReceiving, filetransfer.StateRunning,
				filetransfer.StateCompleted, filetransfer.StateFailed, filetransfer.StateCancelled:
				out = append(out, filetransfer.State(part))
			default:
				return nil, fmt.Errorf("invalid state: %s", part)
			}
		}
	}
	return out, nil
}

// parseFileTransferDirection returns the typed direction or "" for
// empty input.
func parseFileTransferDirection(s string) (filetransfer.Direction, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	switch filetransfer.Direction(s) {
	case filetransfer.DirectionUpload, filetransfer.DirectionDownload:
		return filetransfer.Direction(s), nil
	}
	return "", fmt.Errorf("invalid direction: %s", s)
}

// parseFileTransferBackend returns the typed backend or "" for empty
// input.
func parseFileTransferBackend(s string) (filetransfer.Backend, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	switch filetransfer.Backend(s) {
	case filetransfer.BackendRemote, filetransfer.BackendTFTPServer:
		return filetransfer.Backend(s), nil
	}
	return "", fmt.Errorf("invalid backend: %s", s)
}
