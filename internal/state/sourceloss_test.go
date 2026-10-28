package state

import (
	"testing"
	"time"
)

// covers: MA-686
// The source-loss streak leaf (archive-manager Slice 3a, S3-C1/S3-C3): SourceStreak is the
// archive-local, headless-writable cross-run record of how long an archive's source has been
// unreachable. Observe extends the streak on an unreachable run and RESETS it on a reach; the
// "surface" and "auto-pause" thresholds trigger on the time BETWEEN the first and the latest
// unreachable run (3 days / 30 days, at least two runs), never measured to now (so a machine
// that was merely off never trips them). Amended 2026-10-28 (design-desktop-settings-card
// 3.7, slice A4, owner-accepted): until then they counted runs (3 / 30). It round-trips
// atomically at 0600; an absent record is the zero streak.
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
		t.Fatal("one miss must not surface loudly (at least two unreachable runs, 3 days apart)")
	}
	s = s.Observe(false, "local-gone", t0.Add(2*day))
	if s.ConsecutiveFails != 2 || !s.FirstUnreachable.Equal(t0.Add(day)) || !s.LastUnreachable.Equal(t0.Add(2*day)) {
		t.Fatalf("second miss must keep the original since-date and record the latest: %+v", s)
	}
	if s.SurfaceLoudly() {
		t.Fatal("two misses one day apart must not surface loudly yet")
	}
	s = s.Observe(false, "local-gone", t0.Add(4*day))
	if s.ConsecutiveFails != 3 || !s.SurfaceLoudly() {
		t.Fatalf("misses 3 days apart must surface loudly: %+v", s)
	}
	if s.AutoPauseDue() {
		t.Fatal("auto-pause must not be due after 3 days")
	}

	// A reach resets the whole streak.
	r := s.Observe(true, "", t0.Add(5*day))
	if r.Unreachable() || r.SurfaceLoudly() || !r.FirstUnreachable.IsZero() || !r.LastUnreachable.IsZero() || r.LastKind != "" || !r.LastSuccess.Equal(t0.Add(5*day)) {
		t.Fatalf("a reach must reset the streak: %+v", r)
	}

	// S3-C3: elapsed time to NOW never triggers — a week-old first-unreachable with only one
	// failed run must not surface; nor do many runs inside a short span (hourly runs).
	stale := SourceStreak{ConsecutiveFails: 1, FirstUnreachable: t0.Add(-7 * day), LastUnreachable: t0.Add(-7 * day)}
	if stale.SurfaceLoudly() {
		t.Fatal("a week-old streak of one failed run must NOT surface (measured between runs, not to now)")
	}
	if (SourceStreak{ConsecutiveFails: 72, FirstUnreachable: t0, LastUnreachable: t0.Add(71 * time.Hour)}).SurfaceLoudly() {
		t.Fatal("72 hourly misses spanning 71 hours must not surface (the span is under 3 days)")
	}

	// Auto-pause triggers only at its span between runs.
	if (SourceStreak{ConsecutiveFails: 700, FirstUnreachable: t0, LastUnreachable: t0.Add(AutoPauseAfter - time.Hour)}).AutoPauseDue() {
		t.Fatal("auto-pause must not be due an hour early")
	}
	if !(SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(AutoPauseAfter)}).AutoPauseDue() {
		t.Fatal("auto-pause must be due once the unreachable runs span 30 days")
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
	if got.ConsecutiveFails != 3 || !got.FirstUnreachable.Equal(t0.Add(day)) || !got.LastUnreachable.Equal(t0.Add(4*day)) || got.LastKind != "local-gone" {
		t.Fatalf("round-trip lost state: %+v", got)
	}
}
