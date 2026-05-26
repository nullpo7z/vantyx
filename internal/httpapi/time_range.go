package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nullpo7z/vantyx/internal/i18n"
)

const (
	defaultSearchRangeDays = 30
	maxSearchRangeDays     = 90
)

// Sentinel errors returned by [parseTimeRange] / [parseQueryTime].
// Handlers map these to localized HTTP responses via
// [writeTimeRangeError]; the sentinels themselves are deliberately
// English so logs and tests stay locale-independent.
var (
	// errTimeExpectedFormat is returned by [parseQueryTime] when the
	// input does not match RFC3339 nor YYYY-MM-DD.
	errTimeExpectedFormat = errors.New("expected RFC3339 or YYYY-MM-DD")
	// errFromBeforeTo is returned when `from >= to`.
	errFromBeforeTo = errors.New("from must be before to")
	// errRangeTooLarge is returned when `to - from` exceeds
	// [maxSearchRangeDays].
	errRangeTooLarge = errors.New("time range too large")
)

// invalidFromError wraps the underlying [parseQueryTime] reason for the
// `from` query parameter so [writeTimeRangeError] can localize the
// "invalid from: …" prefix while preserving the inner sentinel for
// errors.Is matching.
type invalidFromError struct{ Reason error }

func (e *invalidFromError) Error() string { return "invalid from: " + e.Reason.Error() }
func (e *invalidFromError) Unwrap() error { return e.Reason }

// invalidToError mirrors [invalidFromError] for the `to` parameter.
type invalidToError struct{ Reason error }

func (e *invalidToError) Error() string { return "invalid to: " + e.Reason.Error() }
func (e *invalidToError) Unwrap() error { return e.Reason }

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
			return time.Time{}, time.Time{}, &invalidFromError{Reason: err}
		}
		fromSet = true
	}
	if toStr != "" {
		to, err = parseQueryTime(toStr, true)
		if err != nil {
			return time.Time{}, time.Time{}, &invalidToError{Reason: err}
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
		return time.Time{}, time.Time{}, errFromBeforeTo
	}
	maxSpan := time.Duration(maxSearchRangeDays) * 24 * time.Hour
	if to.Sub(from) > maxSpan {
		return time.Time{}, time.Time{}, errRangeTooLarge
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
	return time.Time{}, errTimeExpectedFormat
}

// formatRecordingTime formats a time for recordings.started_at comparisons (stored as local UTC string).
func formatRecordingTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// writeTimeRangeError dispatches a parseTimeRange error to a localized
// 400 response. Unknown errors fall back to the raw English message so
// nothing is silently dropped, but all sentinels added by this file are
// covered.
func writeTimeRangeError(w http.ResponseWriter, r *http.Request, err error) {
	var fromErr *invalidFromError
	if errors.As(err, &fromErr) {
		writeJSONErrorKey(w, r, "time.invalidFrom", http.StatusBadRequest,
			"reason", timeReasonText(r, fromErr.Reason))
		return
	}
	var toErr *invalidToError
	if errors.As(err, &toErr) {
		writeJSONErrorKey(w, r, "time.invalidTo", http.StatusBadRequest,
			"reason", timeReasonText(r, toErr.Reason))
		return
	}
	switch {
	case errors.Is(err, errFromBeforeTo):
		writeJSONErrorKey(w, r, "time.fromBeforeTo", http.StatusBadRequest)
	case errors.Is(err, errRangeTooLarge):
		writeJSONErrorKey(w, r, "time.rangeTooLarge", http.StatusBadRequest,
			"days", maxSearchRangeDays)
	default:
		writeJSONError(w, err.Error(), http.StatusBadRequest)
	}
}

// timeReasonText returns the localized reason text for a known
// [parseQueryTime] sentinel, falling back to the English message so the
// caller can still surface unexpected wrapped errors.
func timeReasonText(r *http.Request, reason error) string {
	if errors.Is(reason, errTimeExpectedFormat) {
		return i18n.TR(r, "time.expectedFormat")
	}
	return reason.Error()
}
