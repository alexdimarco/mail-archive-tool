package state

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The C5 adversarial pass's fixes in the streak itself (docs/review-thunderbird-completeness-c5-
// adversarial.md; the lead's calls of 2026-11-09).

// covers: MA-1184
// "Stopped downloading" needs BOTH 3 days since the last progress AND at least 3 backups in that
// span that saw no progress (FALSE-01: a computer switched off for days trips nothing); progress is
// the on-disk count above the PREVIOUS behind backup's, not the streak's highest (FALSE-02); a
// streak whose first backup had a clock before MinPlausibleTime starts its progress time at the
// first plausible backup, and one whose progress time is later than the current backup's (a wrong
// clock) restarts it there (MISS-01). Positive twin first: X stuck across 3 daily backups after
// the first is stopped.
func TestStoppedNeedsThreeDaysAndThreeBackupsWithoutProgress(t *testing.T) {
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
	runs := func(s TBStreak, x int64, at ...time.Time) TBStreak {
		for _, when := range at {
			s = s.Behind("k", "imap.k.example", x, when)
		}
		return s
	}

	stuck := runs(TBStreak{}, 100, t0, t0.Add(day), t0.Add(2*day), t0.Add(3*day))
	if a := acct(stuck); !a.Stopped() || a.NoProgressRuns != 3 || !a.LastGrew.Equal(t0) {
		t.Fatalf("positive twin: X stuck for 3 days over 3 backups: %+v", a)
	}

	// FALSE-01: off for 4 days; the first backup after it saw no progress: not stopped (the
	// still-downloading email is due, MA-891's rule); nor at the second; the third is.
	off := runs(TBStreak{}, 100, t0, t0.Add(4*day))
	if a := acct(off); a.Stopped() {
		t.Errorf("FALSE-01: one backup after a 4-day switch-off is stopped: %+v", a)
	} else if due, stopped := a.Due(); !due || stopped {
		t.Errorf("FALSE-01: the gap's still-downloading email: due %v stopped %v", due, stopped)
	}
	off = runs(off, 100, t0.Add(4*day+time.Hour))
	if a := acct(off); a.Stopped() {
		t.Errorf("two backups without progress are stopped: %+v", a)
	}
	// Amended 2026-11-09 (the lead's call): the third, 2 hours after the gap, is not stopped either;
	// those backups must span a day (TestStoppedNeedsADayOfBackupsWithoutProgress); a day later it is.
	off = runs(off, 100, t0.Add(4*day+2*time.Hour))
	if a := acct(off); a.Stopped() {
		t.Errorf("three backups without progress within 2 hours of a 4-day gap are stopped: %+v", a)
	}
	off = runs(off, 100, t0.Add(5*day))
	if a := acct(off); !a.Stopped() {
		t.Errorf("backups without progress across 4 days and a day after the gap are not stopped: %+v", a)
	}
	// A backup whose clock is implausible counts no backup without progress.
	if a := acct(runs(runs(TBStreak{}, 100, t0, t0.Add(day), time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)), 100, t0.Add(3*day))); a.NoProgressRuns != 2 || a.Stopped() {
		t.Errorf("an implausible clock's backup counted without progress: %+v", a)
	}
	// A clock that went back restarts the progress time and its count.
	if a := acct(runs(TBStreak{}, 100, t0, t0.Add(day), t0.Add(2*day), t0.Add(day+12*time.Hour))); a.NoProgressRuns != 0 || !a.LastGrew.Equal(t0.Add(day+12*time.Hour)) {
		t.Errorf("a clock that went back kept the count: %+v", a)
	}
	// Three backups without progress inside 3 days are not enough either.
	if a := acct(runs(TBStreak{}, 100, t0, t0.Add(time.Hour), t0.Add(2*time.Hour), t0.Add(3*time.Hour))); a.Stopped() {
		t.Errorf("4 hourly backups without progress are stopped: %+v", a)
	}

	// FALSE-02: X falls, then grows backup to backup while below its old highest: progress.
	drop := runs(TBStreak{}, 1000, t0)
	drop = runs(drop, 200, t0.Add(day))
	drop = runs(drop, 500, t0.Add(2*day))
	drop = runs(drop, 800, t0.Add(3*day))
	if a := acct(drop); a.Stopped() || !a.LastGrew.Equal(t0.Add(3*day)) || a.NoProgressRuns != 0 || a.LastX != 800 || a.GrewX != 800 {
		t.Errorf("FALSE-02: X fell and then grew each day: %+v", a)
	}
	// A fall is no progress: X falls and stays, 3 days and 3 backups: stopped.
	fall := runs(runs(TBStreak{}, 1000, t0), 200, t0.Add(day), t0.Add(2*day), t0.Add(3*day))
	if a := acct(fall); !a.Stopped() || a.NoProgressRuns != 3 {
		t.Errorf("a fall that stays: %+v", a)
	}

	// MISS-01, the dead clock battery: the streak's first backup read 2001; the first plausible
	// backup starts the progress time; X stuck afterwards reaches the stopped email.
	dead := runs(TBStreak{}, 100, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	if a := acct(dead); !a.LastGrew.IsZero() || !a.FirstBehind.IsZero() {
		t.Fatalf("an implausible first clock kept a time: %+v", a)
	}
	dead = runs(dead, 100, t0)
	if a := acct(dead); !a.LastGrew.Equal(t0) || !a.FirstBehind.Equal(t0) || a.NoProgressRuns != 0 {
		t.Errorf("MISS-01: the first plausible backup did not start the progress time: %+v", a)
	}
	dead = runs(dead, 100, t0.Add(day), t0.Add(2*day), t0.Add(3*day))
	if a := acct(dead); !a.Stopped() {
		t.Errorf("MISS-01: X stuck for 3 days after a dead-battery start is not stopped: %+v", a)
	}
	// A clock that read a month ahead at the first backup, then put right.
	ahead := runs(TBStreak{}, 100, t0.Add(30*day), t0)
	if a := acct(ahead); !a.LastGrew.Equal(t0) || !a.FirstBehind.Equal(t0) {
		t.Errorf("a progress time ahead of the backup was kept: %+v", a)
	}
	if a := acct(runs(ahead, 100, t0.Add(day), t0.Add(2*day), t0.Add(3*day))); !a.Stopped() {
		t.Errorf("after a clock put right, X stuck for 3 days is not stopped: %+v", a)
	}
	// A progress time later than this backup, alone (the streak's own times fine): reset to it.
	later := TBStreak{Serial: 1, Accounts: []TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1,
		FirstBehind: t0, LastBehind: t0, LastGrew: t0.Add(30 * day), FirstX: 100, GrewX: 100, LastX: 100, Runs: 1}}}
	later = runs(later, 100, t0.Add(day))
	if a := acct(later); !a.LastGrew.Equal(t0.Add(day)) || a.NoProgressRuns != 0 {
		t.Errorf("a progress time after the backup was not reset to it: %+v", a)
	}
	if a := acct(runs(later, 100, t0.Add(2*day), t0.Add(3*day), t0.Add(4*day))); !a.Stopped() {
		t.Errorf("after the reset, X stuck for 3 days is not stopped: %+v", a)
	}
}

// covers: MA-1281
// PLANT-01 (the lead's call): the damaged-record check covers the record's dates too. Any date
// before 2020-01-01, or a first time after a last one (the streak's first behind after its latest,
// its progress before its first or after its latest), and one behind run holding two different
// times, are damaged: the run starts the streak fresh and WARNs (as R4-01), so the email and
// behind_since never show such a date. Amended 2026-11-09 (the C5 round-2 pass's CLOCK-R2-A, the
// lead's call): a date more than a day after the run's clock is no longer damage; it is a clock
// behind (AheadOf, TestARecordAheadOfTheClockIsNotDamaged), so those plants moved there. Positive
// twin first: a record of real folds, read back, is not damaged, at any clock.
func TestAPlantedDateIsDamaged(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	s := TBStreak{}.StartRun(now.Add(-3*day)).Behind("k", "imap.x.example", 1, now.Add(-3*day))
	s = s.StartRun(now.Add(-time.Hour)).Behind("k", "imap.x.example", 1, now.Add(-time.Hour))
	out := t.TempDir()
	if err := WriteTBStreak(out, s.Named()); err != nil {
		t.Fatal(err)
	}
	good, err := ReadTBStreak(out)
	if err != nil || good.Damaged() {
		t.Fatalf("positive twin: a record of real folds: %v damaged %v", err, good.Damaged())
	}
	f := func(t time.Time) string { return t.Format(time.RFC3339) }
	entry := func(first, last, grew time.Time, runs, noProgress int) string {
		return `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(now.Add(-time.Hour)) + `","accounts":[{"store":"k","serial":1,"runs":` +
			itoa(runs) + `,"no_progress_runs":` + itoa(noProgress) + `,"first_behind":"` + f(first) + `","last_behind":"` + f(last) + `","last_grew":"` + f(grew) + `"}]}`
	}
	a, b := now.Add(-3*day), now.Add(-time.Hour)
	if r := mustReadPlant(t, out, entry(a, b, a, 2, 0)); r.Damaged() {
		t.Fatalf("positive twin: a plausible planted shape is damaged: %+v", r)
	}
	for name, plant := range map[string]string{
		"a progress time in 2099 (after the latest)": entry(a, b, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), 2, 0),
		"a latest time before 2020":                  entry(time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC), time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC), 2, 0),
		"the first after the latest":                 entry(b, a, b, 2, 0),
		"progress before the first":                  entry(b, b, a, 2, 0),
		"progress after the latest":                  entry(a, a.Add(day), b, 2, 0),
		"one behind run with two times":              entry(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), b, b, 1, 0),
		"more stuck backups than backups":            entry(a, b, a, 2, 3),
		"the first after the latest, no progress time": `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(now.Add(-time.Hour)) + `","accounts":[{"store":"k","serial":1,"runs":2,"first_behind":"` +
			f(b) + `","last_behind":"` + f(a) + `"}]}`,
	} {
		if r := mustReadPlant(t, out, plant); !r.Damaged() {
			t.Errorf("%s: not damaged: %+v", name, r)
		}
	}
	// Moved by CLOCK-R2-A: dates ahead of the clock, in order, are a clock behind, not damage.
	for name, plant := range map[string]string{
		"a first time in the future":           entry(now.Add(2*day), now.Add(2*day), now.Add(2*day), 2, 0),
		"the record's run in the future":       `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(now.Add(3*day)) + `","accounts":[{"store":"k","serial":1,"runs":1}]}`,
		"the record's run alone in the future": `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(now.Add(3*day)) + `"}`,
	} {
		r := mustReadPlant(t, out, plant)
		if _, ahead := r.AheadOf(now); r.Damaged() || !ahead {
			t.Errorf("%s: damaged %v, ahead %v; want a clock behind", name, r.Damaged(), ahead)
		}
	}
}

// covers: MA-1289, MA-1184
// SPAM-01's second guard: the record says whether a notice's email was already sent, for that
// record, store and streak, and that kind of email only. Positive twin first: a marked
// still-downloading notice is sent.
func TestTheRecordSaysWhichEmailWasSent(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	s := TBStreak{}.Behind("k", "imap.k.example", 1, t0).Behind("k", "imap.k.example", 9, t0.Add(80*time.Hour)).Named()
	a, _ := s.Account("k")
	n := TBNotice{RecordID: s.RecordID, Store: "k", Serial: a.Serial}
	if s.Sent(n) {
		t.Fatal("an unmarked streak's email is sent")
	}
	s, _ = s.MarkSent("k", a.Serial, false)
	if !s.Sent(n) {
		t.Fatal("positive twin: the marked still-downloading email is not sent")
	}
	stopped := n
	stopped.Stopped = true
	other, serial, store, none := n, n, n, n
	other.RecordID, serial.Serial, store.Store, none.RecordID = "AnotherRecord0123", a.Serial+1, "j", ""
	for name, m := range map[string]TBNotice{"the stopped email": stopped, "another record": other, "another streak": serial, "another store": store, "no record": none} {
		if s.Sent(m) {
			t.Errorf("%s reads as sent", name)
		}
	}
	s, _ = s.MarkSent("k", a.Serial, true)
	if !s.Sent(stopped) {
		t.Error("the marked stopped email is not sent")
	}
}

func mustReadPlant(t *testing.T, out, plant string) TBStreak {
	t.Helper()
	if err := os.WriteFile(filepath.Join(out, TBStreakName), []byte(plant), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := ReadTBStreak(out)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func itoa(n int) string { return strconv.Itoa(n) }
