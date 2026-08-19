package util

import (
	"testing"
	"time"
)

// covers: MA-06
func TestParseSinceRelative(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"30d":  now.Add(-30 * 24 * time.Hour),
		"4w":   now.Add(-28 * 24 * time.Hour),
		"12h":  now.Add(-12 * time.Hour),
		"90m":  now.Add(-90 * time.Minute),
		"720h": now.Add(-720 * time.Hour),
	}
	for in, want := range cases {
		got, err := ParseSince(in, now)
		if err != nil {
			t.Errorf("ParseSince(%q) error: %v", in, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("ParseSince(%q) = %v, want %v", in, got, want)
		}
	}
}

// covers: MA-07
func TestParseSinceAbsolute(t *testing.T) {
	now := time.Now()
	got, err := ParseSince("2026-07-01", now)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	want := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// covers: MA-08
func TestParseSinceErrors(t *testing.T) {
	now := time.Now()
	for _, in := range []string{"", "nonsense", "10x", "2026-13-40"} {
		if _, err := ParseSince(in, now); err == nil {
			t.Errorf("ParseSince(%q) expected error, got nil", in)
		}
	}
}
