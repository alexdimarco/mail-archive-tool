package state

import (
	"testing"
	"time"
)

// covers: MA-686
// The source-loss streak leaf (archive-manager Slice 3a, S3-C1/S3-C3): SourceStreak is the
// archive-local, headless-writable cross-run record of how long an archive's source has been
// unreachable. Observe extends the streak on an unreachable run and RESETS it on a reach; the
// "surface" and "auto-pause" thresholds trigger on CONSECUTIVE FAILED RUNS, never elapsed time
// (so a machine that was merely off never trips them). It round-trips atomically at 0600; an
// absent record is the zero streak.
func TestMA686_SourceStreakThresholdsAndRoundTrip(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	// A reach on a fresh archive: still reachable, last-success set.
	s := SourceStreak{}.Observe(true, "", t0)
	if s.Unreachable() || s.SurfaceLoudly() || !s.LastSuccess.Equal(t0) {
		t.Fatalf("a reached source must not be unreachable/surfaced: %+v", s)
	}

	// Three consecutive unreachable runs climb the count and anchor first-unreachable once.
	s = s.Observe(false, "local-gone", t0.Add(day))
	if !s.Unreachable() || s.ConsecutiveFails != 1 || !s.FirstUnreachable.Equal(t0.Add(day)) || s.LastKind != "local-gone" {
		t.Fatalf("first miss: %+v", s)
	}
	if s.SurfaceLoudly() {
		t.Fatal("one miss must not surface loudly (the threshold is 3 runs)")
	}
	s = s.Observe(false, "local-gone", t0.Add(2*day))
	if s.ConsecutiveFails != 2 || !s.FirstUnreachable.Equal(t0.Add(day)) {
		t.Fatalf("second miss must keep the original since-date: %+v", s)
	}
	if s.SurfaceLoudly() {
		t.Fatal("two misses must not surface loudly yet")
	}
	s = s.Observe(false, "local-gone", t0.Add(3*day))
	if s.ConsecutiveFails != 3 || !s.SurfaceLoudly() {
		t.Fatalf("the third consecutive miss must surface loudly: %+v", s)
	}
	if s.AutoPauseDue() {
		t.Fatal("auto-pause must not be due at 3 misses")
	}

	// A reach resets the whole streak.
	r := s.Observe(true, "", t0.Add(4*day))
	if r.Unreachable() || r.SurfaceLoudly() || !r.FirstUnreachable.IsZero() || r.LastKind != "" || !r.LastSuccess.Equal(t0.Add(4*day)) {
		t.Fatalf("a reach must reset the streak: %+v", r)
	}

	// S3-C3: elapsed time ALONE never triggers — a week-old first-unreachable with only one
	// failed run must not surface; only the run count does.
	stale := SourceStreak{ConsecutiveFails: 1, FirstUnreachable: t0.Add(-7 * day)}
	if stale.SurfaceLoudly() {
		t.Fatal("a week-old streak with <3 failed runs must NOT surface (run-count trigger, not wall-clock)")
	}

	// Auto-pause triggers only at its run count.
	if (SourceStreak{ConsecutiveFails: AutoPauseAfterRuns - 1}).AutoPauseDue() {
		t.Fatal("auto-pause must not be due one run early")
	}
	if !(SourceStreak{ConsecutiveFails: AutoPauseAfterRuns}).AutoPauseDue() {
		t.Fatal("auto-pause must be due at the threshold run count")
	}

	// Round-trip: an absent record reads as the zero streak; a written one reads back.
	out := t.TempDir()
	if got, err := ReadSourceStreak(out); err != nil || got.Unreachable() {
		t.Fatalf("absent record must be the zero streak with no error: %+v %v", got, err)
	}
	if err := WriteSourceStreak(out, s); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSourceStreak(out)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConsecutiveFails != 3 || !got.FirstUnreachable.Equal(t0.Add(day)) || got.LastKind != "local-gone" {
		t.Fatalf("round-trip lost state: %+v", got)
	}
}
