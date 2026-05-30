package httpapi

import "testing"

func TestRecordingMediaType(t *testing.T) {
	if got := recordingMediaType("rdp", ""); got != "video" {
		t.Fatalf("rdp: got %q", got)
	}
	if got := recordingMediaType("vnc", ""); got != "video" {
		t.Fatalf("vnc: got %q", got)
	}
	if got := recordingMediaType("browser", "/rec/x.cast"); got != "cast" {
		t.Fatalf("browser: got %q", got)
	}
	if got := recordingMediaType("browser", "/rec/x.webm"); got != "video" {
		t.Fatalf("webm path: got %q", got)
	}
}

func TestFinishVideoRecording_Idempotent(t *testing.T) {
	app := newTestApp(t)
	app.videoRecordings = newVideoRecordingRegistry()
	app.videoRecordings.started["sess-1"] = true
	app.finishVideoRecording("sess-1")
	app.finishVideoRecording("sess-1")
}
