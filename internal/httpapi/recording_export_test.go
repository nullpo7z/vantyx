package httpapi

import "testing"

func TestRecordingNeedsAsyncExport(t *testing.T) {
	if !recordingNeedsAsyncExport("gif", "/rec/x.cast") {
		t.Fatal("gif should be async")
	}
	if recordingNeedsAsyncExport("webm", "/rec/x.webm") {
		t.Fatal("native webm should not be async")
	}
	if !recordingNeedsAsyncExport("webm", "/rec/x.cast") {
		t.Fatal("cast to webm should be async")
	}
}
