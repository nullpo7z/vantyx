package httpapi

import (
	"testing"
	"time"
)

func TestParseTimeRange_Default30Days(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	from, to, err := parseTimeRange("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !to.Equal(now) {
		t.Fatalf("to: got %v want %v", to, now)
	}
	wantFrom := now.Add(-30 * 24 * time.Hour)
	if !from.Equal(wantFrom) {
		t.Fatalf("from: got %v want %v", from, wantFrom)
	}
}

func TestParseTimeRange_DateOnly(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	from, to, err := parseTimeRange("2026-05-01", "2026-05-10", now)
	if err != nil {
		t.Fatal(err)
	}
	if !from.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("from: %v", from)
	}
	if !to.Equal(time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("to (exclusive end of 2026-05-10): %v", to)
	}
}

func TestParseTimeRange_ExceedsMaxSpan(t *testing.T) {
	_, _, err := parseTimeRange("2026-01-01", "2026-06-01", time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for range > 90 days")
	}
}

func TestParseTimeRange_FromAfterTo(t *testing.T) {
	_, _, err := parseTimeRange("2026-05-10", "2026-05-01", time.Now().UTC())
	if err == nil {
		t.Fatal("expected error when from after to")
	}
}
