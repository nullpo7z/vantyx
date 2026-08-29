package recording

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// IsFragmentedMP4 reports whether the MP4 at path is a fragmented file
// (has a top-level "moof" box). The live recorders write
// `-movflags +frag_keyframe+empty_moov` so a crash never loses the whole
// recording, but such files carry no total duration in their (empty)
// moov, so browsers only learn the length as they read. It walks the
// top-level box headers only, so it is cheap even for large files.
func IsFragmentedMP4(path string) (bool, error) {
	f, err := os.Open(path) // #nosec G304 -- path comes from the recordings table / caller-validated.
	if err != nil {
		return false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	size := fi.Size()
	var off int64
	for off+8 <= size {
		var hdr [8]byte
		if _, err := f.ReadAt(hdr[:], off); err != nil {
			return false, err
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[:4]))
		typ := string(hdr[4:8])
		hdrLen := int64(8)
		switch boxSize {
		case 1: // 64-bit largesize follows the header
			var ext [8]byte
			if _, err := f.ReadAt(ext[:], off+8); err != nil {
				return false, err
			}
			boxSize = int64(binary.BigEndian.Uint64(ext[:])) // #nosec G115 -- box sizes are bounded by the file size checked below.
			hdrLen = 16
		case 0: // box extends to end of file
			boxSize = size - off
		}
		if typ == "moof" {
			return true, nil
		}
		if boxSize < hdrLen {
			return false, fmt.Errorf("remux: malformed mp4 box %q at offset %d", typ, off)
		}
		off += boxSize
	}
	return false, nil
}

// RemuxFaststart rewrites the MP4 at path in place as a regular
// (non-fragmented) file with the moov atom up front, so the total
// duration is known immediately and the timeline is fully seekable.
// Streams are copied, not re-encoded (`-c copy`), so this is I/O bound
// and lossless. The output is written to a temp file next to path and
// only swapped in after it verified as a non-fragmented MP4 with data,
// so a failure leaves the original untouched. threads <= 0 lets ffmpeg
// pick.
func RemuxFaststart(ctx context.Context, path string, threads int) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	tmp := path + ".remux.tmp.mp4"
	defer os.Remove(tmp)
	args := []string{"-nostdin", "-y", "-loglevel", "error"}
	if threads > 0 {
		args = append(args, "-threads", strconv.Itoa(threads))
	}
	args = append(args, "-i", path, "-c", "copy", "-movflags", "+faststart", "-f", "mp4", tmp)
	cmd := exec.CommandContext(ctx, ffmpeg, args...) // #nosec G204 -- fixed tool, validated paths.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return fmt.Errorf("ffmpeg remux: %w: %s", err, msg)
	}
	fi, err := os.Stat(tmp)
	if err != nil {
		return fmt.Errorf("remux output missing: %w", err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("remux output is empty")
	}
	if frag, err := IsFragmentedMP4(tmp); err != nil {
		return fmt.Errorf("remux output check: %w", err)
	} else if frag {
		return fmt.Errorf("remux output is still fragmented")
	}
	if err := os.Rename(tmp, path); err != nil {
		// Some network filesystems refuse to rename over an existing
		// file; retry after removing the original (the output has
		// already been verified above).
		if rmErr := os.Remove(path); rmErr != nil {
			return fmt.Errorf("remux swap: %w (and remove original: %v)", err, rmErr)
		}
		if err := os.Rename(tmp, path); err != nil {
			return fmt.Errorf("remux swap after remove: %w", err)
		}
	}
	return nil
}
