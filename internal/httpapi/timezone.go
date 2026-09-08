package httpapi

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// TimezoneEnv names the single timezone every human-facing timestamp in
// Vantyx uses: container logs, the audit log file and syslog forward,
// the web UI and the CLI gateway. Storage stays UTC. Default: UTC.
const TimezoneEnv = "VANTYX_TIMEZONE"

// LoadTimezoneFromEnv parses VANTYX_TIMEZONE (an IANA zone name such as
// "Asia/Tokyo"). Empty means UTC; an unknown name is an error so a typo
// cannot silently leave the deployment on a different clock than the
// operator expects.
func LoadTimezoneFromEnv() (*time.Location, error) {
	name := strings.TrimSpace(os.Getenv(TimezoneEnv))
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%s=%q: %w", TimezoneEnv, name, err)
	}
	return loc, nil
}

// serverLocation is the configured zone (UTC when the App was built
// without one, e.g. in tests) for formatting API timestamps.
func (a *App) serverLocation() *time.Location {
	if a == nil || a.Location == nil {
		return time.UTC
	}
	return a.Location
}

// displayTimezone is the zone name handed to clients (login / GET
// /api/me `timezone`) so the UI formats timestamps like the server logs.
func (a *App) displayTimezone() string {
	if a == nil || a.Location == nil {
		return time.UTC.String()
	}
	return a.Location.String()
}
