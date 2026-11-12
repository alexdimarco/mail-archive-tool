package state

import (
	"testing"
	"time"
)

// covers: MA-1188, MA-1184
// The lead's call of 2026-11-09, beside FALSE-01: the backups without progress that "stopped"
// counts must span at least a day (TBStoppedSpan), first to latest, so an hourly schedule after a
// long switch-off cannot call the account stopped a few hours after the computer is back on. After
// 4 days off, hourly backups that find no progress are not stopped for the first 23 hours (nor at
// 23h59), and are at 24 hours. A daily schedule is unchanged: its third backup without progress
// already spans 48 hours. Positive twins first: daily, then the hourly run at 24 hours.
func TestStoppedNeedsADayOfBackupsWithoutProgress(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	acct := func(s TBStreak) TBStreakAccount {
		t.Helper()
		a, ok := s.Account("k")
		if !ok {
			t.Fatal("no streak")
		}
		return a
	}
	run := func(s TBStreak, x int64, at time.Time) TBStreak { return s.Behind("k", "imap.k.example", x, at) }

	// Daily, unchanged: stopped exactly at the third backup without progress (3 days, 48 hours).
	d := run(TBStreak{}, 100, t0)
	for i := 1; i <= 3; i++ {
		d = run(d, 100, t0.Add(time.Duration(i)*day))
		if a := acct(d); a.Stopped() != (i == 3) {
			t.Errorf("daily, backup %d without progress: stopped %v (%+v)", i, a.Stopped(), a)
		}
	}
	if a := acct(d); !a.FirstNoProgress.Equal(t0.Add(day)) || a.NoProgressRuns != 3 {
		t.Errorf("positive twin, daily: the first backup without progress %v, count %d", a.FirstNoProgress, a.NoProgressRuns)
	}

	// Hourly after 4 days off: the first backup back (T) is the first without progress.
	back := t0.Add(4 * day)
	h := run(run(TBStreak{}, 100, t0), 100, back)
	if a := acct(h); !a.FirstNoProgress.Equal(back) || a.Stopped() {
		t.Fatalf("the first backup after the switch-off: %+v", a)
	}
	for i := 1; i <= 23; i++ {
		h = run(h, 100, back.Add(time.Duration(i)*time.Hour))
		if a := acct(h); a.Stopped() {
			t.Fatalf("hourly, %d hours after the computer came back: stopped (%+v)", i, a)
		}
	}
	h = run(h, 100, back.Add(23*time.Hour+59*time.Minute))
	if a := acct(h); a.Stopped() {
		t.Errorf("23h59 of backups without progress is stopped: %+v", a)
	}
	h = run(h, 100, back.Add(day))
	if a := acct(h); !a.Stopped() || !a.FirstNoProgress.Equal(back) || !a.LastGrew.Equal(t0) {
		t.Errorf("positive twin, hourly: 24 hours of backups without progress is not stopped: %+v", a)
	}

	// Progress ends the window; the next backup without progress starts a new one.
	p := run(run(TBStreak{}, 100, t0), 100, back)
	p = run(p, 100, back.Add(time.Hour))
	p = run(p, 150, back.Add(12*time.Hour))
	if a := acct(p); !a.FirstNoProgress.IsZero() {
		t.Errorf("progress kept the first backup without progress: %+v", a)
	}
	if a := acct(run(p, 150, back.Add(13*time.Hour))); !a.FirstNoProgress.Equal(back.Add(13 * time.Hour)) {
		t.Errorf("the window after progress starts at %v", a.FirstNoProgress)
	}
	// A clock that went back ends the window too.
	c := run(run(run(TBStreak{}, 100, t0), 100, t0.Add(day)), 100, t0.Add(2*day))
	c = run(c, 100, t0.Add(day+12*time.Hour))
	if a := acct(c); !a.FirstNoProgress.IsZero() || a.NoProgressRuns != 0 {
		t.Errorf("a clock that went back kept the window: %+v", a)
	}
	// MISS-01's restart (a progress time later than this backup) ends the window too.
	m := TBStreak{Serial: 1, Accounts: []TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1, FirstBehind: t0, LastBehind: t0,
		LastGrew: t0.Add(30 * day), FirstNoProgress: t0.Add(30 * day), FirstX: 100, GrewX: 100, LastX: 100, Runs: 2, NoProgressRuns: 1}}}
	if a := acct(run(m, 100, t0.Add(day))); !a.FirstNoProgress.IsZero() || !a.LastGrew.Equal(t0.Add(day)) {
		t.Errorf("MISS-01's restart kept the window: %+v", a)
	}
	// A backup with an implausible clock starts no window.
	i := run(run(TBStreak{}, 100, t0), 100, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	if a := acct(i); !a.FirstNoProgress.IsZero() {
		t.Errorf("an implausible clock started the window: %+v", a)
	}

	// A record written before the field (3 backups without progress, no first time): not stopped
	// until a day after its next backup without progress.
	old := TBStreak{Serial: 1, Accounts: []TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1,
		FirstBehind: t0, LastBehind: t0.Add(3 * day), LastGrew: t0, FirstX: 100, GrewX: 100, LastX: 100, Runs: 4, NoProgressRuns: 3}}}
	if acct(old).Stopped() {
		t.Error("a record with no first time is stopped")
	}
	old = run(old, 100, t0.Add(4*day))
	if a := acct(old); a.Stopped() || !a.FirstNoProgress.Equal(t0.Add(4*day)) {
		t.Errorf("an old record's next backup: %+v", a)
	}
	if a := acct(run(old, 100, t0.Add(5*day))); !a.Stopped() {
		t.Errorf("an old record a day later is not stopped: %+v", a)
	}
}

// covers: MA-1281, MA-1188
// The new date is checked as the others (PLANT-01): a first backup without progress before 2020,
// before the progress time or after the latest behind backup is damaged. Amended 2026-11-09 (the
// C5 round-2 pass's CLOCK-R2-A, the lead's call): one more than a day ahead, in order, is a clock
// behind (AheadOf), not damage. Positive twin first: a planted record whose window sits between
// its progress and latest times.
func TestAPlantedNoProgressDateIsDamaged(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	f := func(t time.Time) string { return t.Format(time.RFC3339) }
	grew, last := now.Add(-3*day), now.Add(-time.Hour)
	// Each plant holds one fault only: a missing time is left out, so no other check sees it.
	plant := func(grew, last, first time.Time) string {
		opt := func(k string, v time.Time) string {
			if v.IsZero() {
				return ""
			}
			return `,"` + k + `":"` + f(v) + `"`
		}
		return `{"version":1,"serial":1,"run":4,"last_run_at":"` + f(now.Add(-time.Hour)) + `","accounts":[{"store":"k","serial":1,"runs":4,"no_progress_runs":3` +
			opt("last_behind", last) + opt("last_grew", grew) + opt("first_no_progress", first) + `}]}`
	}
	out := t.TempDir()
	mid := now.Add(-2 * day)
	for name, p := range map[string]string{
		"a window between progress and latest": plant(grew, last, mid),
		"a window with no latest time":         plant(grew, time.Time{}, mid),
		// "a window with no progress time" left the positive twins 2026-11-09: PLANT-R2's pair rule
		// (TestAnInconsistentPairIsDamaged) makes it damaged, since no backup writes one without the other.
	} {
		if r := mustReadPlant(t, out, p); r.Damaged() {
			t.Fatalf("positive twin, %s: damaged: %+v", name, r)
		}
	}
	for name, p := range map[string]string{
		"before 2020":                 plant(grew, last, time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC)),
		"before the progress time":    plant(grew, last, grew.Add(-time.Hour)),
		"after the latest behind run": plant(grew, last, last.Add(time.Hour)),
	} {
		if r := mustReadPlant(t, out, p); !r.Damaged() {
			t.Errorf("a first backup without progress %s: not damaged", name)
		}
	}
	if r := mustReadPlant(t, out, plant(grew, time.Time{}, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))); r.Damaged() {
		t.Error("a first backup without progress in 2099 (no latest time) is damaged; it is a clock behind")
	} else if _, ahead := r.AheadOf(now); !ahead {
		t.Error("a first backup without progress in 2099 is not ahead of the clock")
	}
}
