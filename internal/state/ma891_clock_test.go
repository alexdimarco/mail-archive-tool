package state

import (
	"os"
	"path/filepath"
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

// covers: MA-891, R37
// A v1.5.0 source-loss record whose first time a wrong clock wrote recovers (the part A
// re-verification, 2026-10-28): v1.5.0 set first_unreachable once from whatever the clock read
// and kept no last_unreachable. Read back after the upgrade, a first time before MinPlausibleTime
// (2000-01-01) or after now (the clock was ahead) restarts the span at the next unreachable run, so
// 3 days of unreachable runs warn and 30 days auto-pause again. Positive twin first: a v1.5.0 record
// with a trustworthy first time keeps it, and the span is measured from it.
func TestAV150RecordWithAnUntrustworthyFirstTimeRecovers(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	read := func(first string) SourceStreak {
		t.Helper()
		out := t.TempDir()
		v150 := `{"version":1,"consecutive_fails":12,"first_unreachable":"` + first + `","last_kind":"m365-unreachable"}`
		if err := os.WriteFile(filepath.Join(out, SourceStreakName), []byte(v150), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := ReadSourceStreak(out)
		if err != nil || s.ConsecutiveFails != 12 || !s.LastUnreachable.IsZero() {
			t.Fatalf("fixture: the v1.5.0 record read as %+v (%v)", s, err)
		}
		return s
	}
	runs := func(s SourceStreak, days int) SourceStreak {
		for h := 0; h <= days*24; h += 6 {
			s = s.Observe(false, "m365-unreachable", t0.Add(time.Duration(h)*time.Hour))
		}
		return s
	}

	trusted := runs(read(t0.Add(-24*time.Hour).Format(time.RFC3339)), 2)
	if !trusted.FirstUnreachable.Equal(t0.Add(-24*time.Hour)) || !trusted.SurfaceLoudly() {
		t.Fatalf("the positive twin: a trustworthy v1.5.0 first time was not kept: %+v (warn %v)", trusted, trusted.SurfaceLoudly())
	}

	for _, first := range []string{"2000-01-01T00:00:00Z", t0.Add(90 * 24 * time.Hour).Format(time.RFC3339)} {
		s := runs(read(first), 3)
		if !s.FirstUnreachable.Equal(t0) || !s.SurfaceLoudly() {
			t.Errorf("a v1.5.0 first time of %s never recovered: %+v (warn %v)", first, s, s.SurfaceLoudly())
		}
		if s = runs(read(first), 30); !s.AutoPauseDue() {
			t.Errorf("a v1.5.0 first time of %s: 30 days of unreachable runs do not auto-pause: %+v", first, s)
		}
	}
}
