package util

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dateLayouts are the absolute date formats accepted by ParseSince, tried in
// order.
var dateLayouts = []string{
	"2006-01-02",
	"2006-01-02T15:04:05Z07:00",
	"2006/01/02",
}

// ParseSince interprets a --since value relative to now and returns the cutoff
// time: messages older than the returned time should be excluded.
//
// Accepted forms:
//   - relative windows: "30d" (days), "4w" (weeks), "12h", "45m", "90s"
//   - Go durations:     "720h", "1h30m"
//   - absolute dates:   "2026-07-01", "2026/07/01", RFC3339 timestamps
//
// "last month" is expressed as "30d".
func ParseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty --since value")
	}

	// Absolute dates first.
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	// Day/week suffixes that time.ParseDuration does not understand.
	if len(s) >= 2 {
		unit := s[len(s)-1]
		if unit == 'd' || unit == 'w' {
			n, err := strconv.Atoi(s[:len(s)-1])
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid --since %q: %w", s, err)
			}
			days := n
			if unit == 'w' {
				days = n * 7
			}
			return now.Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}

	// Fall back to Go duration syntax (h/m/s combinations).
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}

	return time.Time{}, fmt.Errorf("unrecognized --since %q (use e.g. 30d, 4w, 720h, or 2026-07-01)", s)
}
