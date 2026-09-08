package recording

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// box builds a minimal ISO-BMFF box: 32-bit size + fourcc + payload.
func box(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b[:4], uint32(len(b))) // #nosec G115 -- test payloads are tiny.
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

func writeTempMP4(t *testing.T, name string, boxes ...[]byte) string {
	t.Helper()
	var data []byte
	for _, b := range boxes {
		data = append(data, b...)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestIsFragmentedMP4(t *testing.T) {
	frag := writeTempMP4(t, "frag.mp4",
		box("ftyp", []byte("isom")),
		box("moov", nil),
		box("moof", []byte{0, 0, 0, 0}),
		box("mdat", []byte("xxxx")),
	)
	plain := writeTempMP4(t, "plain.mp4",
		box("ftyp", []byte("isom")),
		box("moov", []byte("meta")),
		box("mdat", []byte("xxxx")),
	)
	if got, err := IsFragmentedMP4(frag); err != nil || !got {
		t.Fatalf("frag: got (%v, %v), want (true, nil)", got, err)
	}
	if got, err := IsFragmentedMP4(plain); err != nil || got {
		t.Fatalf("plain: got (%v, %v), want (false, nil)", got, err)
	}
}

func TestIsFragmentedMP4_LargesizeAndToEOF(t *testing.T) {
	// A box with size==1 carries a 64-bit largesize; size==0 runs to EOF.
	large := make([]byte, 16+4)
	binary.BigEndian.PutUint32(large[:4], 1)
	copy(large[4:8], "free")
	binary.BigEndian.PutUint64(large[8:16], uint64(len(large)))
	toEOF := make([]byte, 8+3)
	copy(toEOF[4:8], "mdat") // size 0 -> to end of file
	p := writeTempMP4(t, "large.mp4", box("ftyp", nil), large, box("moof", nil), toEOF)
	if got, err := IsFragmentedMP4(p); err != nil || !got {
		t.Fatalf("largesize walk: got (%v, %v), want (true, nil)", got, err)
	}
}

func TestIsFragmentedMP4_Malformed(t *testing.T) {
	bad := make([]byte, 8)
	binary.BigEndian.PutUint32(bad[:4], 4) // smaller than its own header
	copy(bad[4:8], "junk")
	p := writeTempMP4(t, "bad.mp4", bad)
	if _, err := IsFragmentedMP4(p); err == nil {
		t.Fatal("expected an error for a box smaller than its header")
	}
}

// TestRemuxFaststart_RoundTrip needs a real ffmpeg (present in the runtime
// image, absent in the builder); it generates a short fragmented MP4 and
// checks the in-place remux produces a non-fragmented file.
func TestRemuxFaststart_RoundTrip(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	p := filepath.Join(t.TempDir(), "rec.mp4")
	gen := exec.Command(ffmpeg, "-nostdin", "-y", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=8", "-t", "1",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-movflags", "+frag_keyframe+empty_moov", p)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("could not generate a test recording: %v: %s", err, out)
	}
	if frag, err := IsFragmentedMP4(p); err != nil || !frag {
		t.Fatalf("precondition: expected a fragmented input, got (%v, %v)", frag, err)
	}
	if err := RemuxFaststart(context.Background(), p, 1); err != nil {
		t.Fatalf("RemuxFaststart: %v", err)
	}
	if frag, err := IsFragmentedMP4(p); err != nil || frag {
		t.Fatalf("after remux: expected non-fragmented, got (%v, %v)", frag, err)
	}
	if _, err := os.Stat(p + ".remux.tmp.mp4"); !os.IsNotExist(err) {
		t.Fatalf("temp file should be gone, stat err=%v", err)
	}
}
