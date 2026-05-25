package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestAuditWritesToSlogAndSink(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	cap := NewCaptureBuffer(slog.LevelInfo)
	slog.SetDefault(slog.New(cap))

	sink := &CapturingAuditSink{}
	RegisterAuditSink(sink)
	t.Cleanup(func() { RegisterAuditSink(nil) })

	Audit(context.Background(), "test_event", map[string]any{
		KeyUserID:   "alice",
		KeyTargetID: "web-1",
	})

	if !cap.Contains("audit") || !cap.Contains("event=test_event") {
		t.Fatalf("expected slog output to contain audit + event=test_event, got %q", cap.String())
	}
	if !cap.Contains("user_id=alice") || !cap.Contains("target_id=web-1") {
		t.Fatalf("expected slog output to contain canonical keys, got %q", cap.String())
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 sink event, got %d", len(events))
	}
	if events[0].Event != "test_event" {
		t.Fatalf("unexpected event name %q", events[0].Event)
	}
	if events[0].Fields[KeyUserID] != "alice" {
		t.Fatalf("expected user_id=alice in sink, got %+v", events[0].Fields)
	}
}

func TestAuditWithoutSink(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	RegisterAuditSink(nil)

	cap := NewCaptureBuffer(slog.LevelInfo)
	slog.SetDefault(slog.New(cap))

	Audit(context.Background(), "no_sink", nil)
	if !cap.Contains("event=no_sink") {
		t.Fatalf("expected audit line, got %q", cap.String())
	}
}

func TestWithComponentTagsRecords(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	cap := NewCaptureBuffer(slog.LevelInfo)
	slog.SetDefault(slog.New(cap))

	log := WithComponent("test_component")
	log.Info("hello")
	if !cap.Contains("component=test_component") {
		t.Fatalf("expected component tag, got %q", cap.String())
	}
}
