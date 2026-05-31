package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordingNeedsAsyncExport(t *testing.T) {
	if !recordingNeedsAsyncExport("gif", "/rec/x.cast") {
		t.Fatal("cast to gif should be async")
	}
	if !recordingNeedsAsyncExport("mp4", "/rec/x.cast") {
		t.Fatal("cast to mp4 should be async")
	}
	if !recordingNeedsAsyncExport("gif", "/rec/x.mp4") {
		t.Fatal("mp4 to gif should be async")
	}
	if recordingNeedsAsyncExport("mp4", "/rec/x.mp4") {
		t.Fatal("native mp4 should not be async")
	}
}

func TestExportStageError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	err := exportStageError("ffmpeg mp4", ctx, nil, context.DeadlineExceeded)
	if err == nil || err.Error() != "export ffmpeg mp4 timed out" {
		t.Fatalf("got %v", err)
	}
	err = exportStageError("agg", context.Background(), []byte("bad cast stderr"), errors.New("exit 1"))
	if err == nil || err.Error() != "export agg failed" {
		t.Fatalf("got %v", err)
	}
}

func TestEnqueueRecordingExportDoesNotDeadlock(t *testing.T) {
	app := newTestApp(t)
	dir := t.TempDir()
	app.RecordingExports = newRecordingExportRegistry(dir)
	ctx := context.Background()
	castPath := filepath.Join(dir, "rec-deadlock.cast")
	if err := os.WriteFile(castPath, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.InsertRecording(ctx, "rec-deadlock", "admin", "t1", "s1", "browser", castPath, time.Now().UTC().Format(time.RFC3339), "sl", ""); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := app.enqueueRecordingExport("admin", "rec-deadlock", "mp4", castPath)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("enqueueRecordingExport deadlocked")
	}

	app.RecordingExports.mu.RLock()
	count := len(app.RecordingExports.jobs)
	app.RecordingExports.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 job, got %d", count)
	}
}

func TestFindActiveLockedDedupesStaleRunningJob(t *testing.T) {
	reg := newRecordingExportRegistry(t.TempDir())
	stale := &recordingExportJob{
		ID:          "job-stale",
		UserID:      "u1",
		RecordingID: "rec1",
		Format:      "gif",
		State:       recordingExportRunning,
		UpdatedAt:   time.Now().UTC().Add(-2 * time.Hour),
	}
	reg.jobs[stale.ID] = stale

	dup := reg.findActive("u1", "rec1", "gif")
	if dup == nil || dup.ID != stale.ID {
		t.Fatalf("expected stale running job to block duplicate enqueue, got %#v", dup)
	}
}

func TestCleanupOldGlobTempsRespectsAge(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "rec-old.gif")
	newPath := filepath.Join(dir, "rec-new.gif")
	if err := os.WriteFile(oldPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	cleanupOldGlobTemps(dir, []string{"rec-*.gif"}, 24*time.Hour)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("expected old temp to be removed")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected recent temp to remain: %v", err)
	}
}

func TestOpenExportPathRejectsEscape(t *testing.T) {
	exportDir := t.TempDir()
	outside := t.TempDir()
	outFile := filepath.Join(outside, "secret.gif")
	if err := os.WriteFile(outFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openExportPath(exportDir, outFile); err == nil {
		t.Fatal("expected path outside export dir to be rejected")
	}

	inside := filepath.Join(exportDir, "rec-inside.gif")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := openExportPath(exportDir, inside)
	if err != nil {
		t.Fatalf("openExportPath: %v", err)
	}
	if got != inside {
		t.Fatalf("got %q want %q", got, inside)
	}
}

func TestRecordingExportConvertTimeoutDuration(t *testing.T) {
	t.Setenv(recordingExportConvertTimeoutEnv, "90m")
	if d := recordingExportConvertTimeoutDuration(); d != 90*time.Minute {
		t.Fatalf("got %v", d)
	}
	t.Setenv(recordingExportConvertTimeoutEnv, "bad")
	if d := recordingExportConvertTimeoutDuration(); d != recordingExportConvertTimeout {
		t.Fatalf("got %v", d)
	}
}

func TestRecordingExportCompletedTTLDuration(t *testing.T) {
	t.Setenv(recordingExportCompletedTTLEnv, "48h")
	if d := recordingExportCompletedTTLDuration(); d != 48*time.Hour {
		t.Fatalf("got %v", d)
	}
	t.Setenv(recordingExportCompletedTTLEnv, "bad")
	if d := recordingExportCompletedTTLDuration(); d != recordingExportCompletedTTL {
		t.Fatalf("got %v", d)
	}
}

func TestUserVisibleExportError(t *testing.T) {
	msg := userVisibleExportError(errRecordingVideoTools)
	if msg != "Video export requires agg and ffmpeg on the server" {
		t.Fatalf("got %q", msg)
	}
}
