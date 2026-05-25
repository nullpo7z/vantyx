package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	defaultSearchRangeDays = 30
	maxSearchRangeDays     = 90
)

// parseTimeRange parses from/to query parameters for audit/command/recording search.
// Dates (YYYY-MM-DD) use UTC day boundaries; to is exclusive (next day start for date-only to).
// When both are empty, returns [now-defaultDays, now).
func parseTimeRange(fromStr, toStr string, now time.Time) (from, to time.Time, err error) {
	now = now.UTC()
	fromStr = strings.TrimSpace(fromStr)
	toStr = strings.TrimSpace(toStr)

	var fromSet, toSet bool
	if fromStr != "" {
		from, err = parseQueryTime(fromStr, false)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid from: %w", err)
		}
		fromSet = true
	}
	if toStr != "" {
		to, err = parseQueryTime(toStr, true)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid to: %w", err)
		}
		toSet = true
	}

	switch {
	case !fromSet && !toSet:
		to = now
		from = to.Add(-defaultSearchRangeDays * 24 * time.Hour)
	case fromSet && !toSet:
		to = now
	case !fromSet && toSet:
		from = to.Add(-defaultSearchRangeDays * 24 * time.Hour)
	}

	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be before to")
	}
	maxSpan := time.Duration(maxSearchRangeDays) * 24 * time.Hour
	if to.Sub(from) > maxSpan {
		return time.Time{}, time.Time{}, fmt.Errorf("time range must not exceed %d days", maxSearchRangeDays)
	}
	return from, to, nil
}

// parseQueryTime parses RFC3339 or YYYY-MM-DD. For date-only end (isEnd), returns start of next UTC day.
func parseQueryTime(s string, isEnd bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		t = t.UTC()
		if isEnd {
			return t.Add(24 * time.Hour), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD")
}

// formatRecordingTime formats a time for recordings.started_at comparisons (stored as local UTC string).
func formatRecordingTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func writeTimeRangeError(w http.ResponseWriter, err error) {
	writeJSONError(w, err.Error(), http.StatusBadRequest)
}
