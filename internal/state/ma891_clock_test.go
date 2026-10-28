package state

import (
	"testing"
	"time"
)

// covers: MA-891, R37
// The source-loss thresholds never trust a time a wrong clock wrote (the part A pass): a run
// stamped while the clock was far ahead and then put right restarts the span at the corrected
// time, so the span is never negative and the warning still comes 3 days of unreachable runs
// after it; a run stamped before MinPlausibleTime (a computer started with no clock set) is
// counted but stored as neither time, so a 1970 stamp followed an hour later by a true one trips
// nothing; and a record already holding such times spans nothing. Positive twin first: steady
// hourly runs over 3 days warn.
func TestTheSourceLossSpanIgnoresAWrongClock(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	steady := SourceStreak{}
	for h := 0; h <= 72; h++ {
		steady = steady.Observe(false, "local-source-unreachable", t0.Add(time.Duration(h)*time.Hour))
	}
	if !steady.SurfaceLoudly() {
		t.Fatal("the positive twin: an hourly source gone 3 days does not warn")
	}

	// Ahead, then put right: the span starts again at the corrected time and is never negative.
	ahead := SourceStreak{}.Observe(false, "local-source-unreachable", t0.Add(200*24*time.Hour))
	s := ahead
	for h := 0; h <= 72; h++ {
		s = s.Observe(false, "local-source-unreachable", t0.Add(time.Duration(h)*time.Hour))
		if d := s.unreachableFor(); d < 0 {
			t.Fatalf("the span ran negative after the clock was put right: %v", d)
		}
	}
	if !s.FirstUnreachable.Equal(t0) || !s.SurfaceLoudly() {
		t.Fatalf("after the clock was put right the span did not start again (first %v, warn %v)", s.FirstUnreachable, s.SurfaceLoudly())
	}

	// A clock that was not set: the run counts, its time is not kept, and nothing trips.
	unset := SourceStreak{}.Observe(false, "local-source-unreachable", time.Unix(0, 0).UTC())
	if unset.ConsecutiveFails != 1 || !unset.FirstUnreachable.IsZero() || !unset.LastUnreachable.IsZero() {
		t.Fatalf("a 1970 stamp was kept: %+v", unset)
	}
	unset = unset.Observe(false, "local-source-unreachable", t0)
	if unset.SurfaceLoudly() || unset.AutoPauseDue() {
		t.Fatalf("a 1970 stamp then a true one an hour later tripped a threshold: %+v", unset)
	}

	// A record written while a clock was wrong spans nothing.
	for _, r := range []SourceStreak{
		{ConsecutiveFails: 5, FirstUnreachable: time.Unix(0, 0).UTC(), LastUnreachable: t0},
		{ConsecutiveFails: 5, FirstUnreachable: t0.Add(60 * 24 * time.Hour), LastUnreachable: t0},
	} {
		if r.SurfaceLoudly() || r.AutoPauseDue() {
			t.Errorf("a record with an impossible span tripped a threshold: %+v", r)
		}
	}
}
