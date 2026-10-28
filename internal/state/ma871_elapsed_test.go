package state

import (
	"testing"
	"time"
)

// covers: MA-871, R37
// The source-loss thresholds by time between runs (design-desktop-settings-card 3.7, R3C-11):
// the warning at 3 days and the auto-pause at 30 days of LastUnreachable minus FirstUnreachable,
// with at least two failures, never compared with now. Positive twins first: an hourly source
// gone for 3 days warns, gone for 30 days auto-pauses. A computer off 30 days after one failure
// trips nothing; a weekly backup that fails once, then the computer is off a month, auto-pauses at
// its next failed run (and warns at once) — the edge the owner accepted on 2026-10-28.
func TestTheSourceLossThresholdsAreTimeBetweenRuns(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	hourly := SourceStreak{}
	for h := 0; h <= 72; h++ {
		hourly = hourly.Observe(false, "local-source-unreachable", t0.Add(time.Duration(h)*time.Hour))
	}
	if !hourly.SurfaceLoudly() || hourly.AutoPauseDue() {
		t.Fatalf("an hourly source gone 3 days: warn=%v pause=%v", hourly.SurfaceLoudly(), hourly.AutoPauseDue())
	}
	for h := 73; h <= 30*24; h++ {
		hourly = hourly.Observe(false, "local-source-unreachable", t0.Add(time.Duration(h)*time.Hour))
	}
	if !hourly.AutoPauseDue() {
		t.Fatal("an hourly source gone 30 days must auto-pause")
	}

	// One failure, then the computer off for 30 days: nothing trips, whenever it is read.
	once := SourceStreak{}.Observe(false, "m365-unreachable", t0)
	if once.SurfaceLoudly() || once.AutoPauseDue() {
		t.Fatal("one failed run, then the computer off, tripped a threshold")
	}

	// A weekly backup failing once, then off a month: the next failed run auto-pauses and warns.
	weekly := once.Observe(false, "m365-unreachable", t0.Add(35*24*time.Hour))
	if !weekly.AutoPauseDue() || !weekly.SurfaceLoudly() {
		t.Fatalf("the accepted edge: %+v", weekly)
	}

	// At least two failures: a record of one failed run trips nothing, whatever its dates say.
	if (SourceStreak{ConsecutiveFails: 1, FirstUnreachable: t0, LastUnreachable: t0.Add(40 * 24 * time.Hour)}).AutoPauseDue() {
		t.Fatal("a record of one failed run auto-paused")
	}

	// Many failures inside a short span trip nothing (24 runs a day is not 24 days).
	burst := SourceStreak{}
	for m := 0; m < 100; m++ {
		burst = burst.Observe(false, "local-source-unreachable", t0.Add(time.Duration(m)*time.Minute))
	}
	if burst.SurfaceLoudly() || burst.AutoPauseDue() {
		t.Fatal("100 failed runs in 100 minutes tripped a threshold")
	}
}
