package state

import (
	"strconv"
	"testing"
	"time"
)

// The C5 adversarial pass round 2 (docs/review-thunderbird-completeness-c5-adversarial-r2.md), the
// lead's calls of 2026-11-09: CLOCK-R2-A (a record ahead of the run's clock is a clock behind, never
// damage), PLANT-R2 (an inconsistent pair is damage) and SPAM-R2-A (failed sends are counted).

// covers: MA-1290, MA-1281
// CLOCK-R2-A: a record whose dates are more than a day after the run's clock is not damaged; it is
// AheadOf that clock, which names the record's latest date. Less than a day ahead is neither (a
// time zone, a small correction), and a run whose own clock is before MinPlausibleTime is never
// "behind" (its clock is plainly wrong, and MA-891's rules already move no time for it). A record
// all of whose dates are ahead, in order, is ahead and not damaged; one date ahead and out of order
// is still damaged by the order checks. Positive twin first: a record of real folds, read at its
// own time, is neither damaged nor ahead.
func TestARecordAheadOfTheClockIsNotDamaged(t *testing.T) {
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
	if _, ahead := good.AheadOf(now); ahead {
		t.Fatal("positive twin: a record of real folds is ahead of its own clock")
	}
	if _, ahead := good.AheadOf(now.Add(-time.Hour - 23*time.Hour)); ahead {
		t.Error("a record 23 hours ahead of the clock is ahead")
	}
	latest, ahead := good.AheadOf(now.Add(-time.Hour - 25*time.Hour))
	if !ahead || !latest.Equal(now.Add(-time.Hour)) {
		t.Errorf("a record 25 hours ahead of the clock: ahead %v, latest %v", ahead, latest)
	}
	if _, ahead := good.AheadOf(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)); !ahead {
		t.Error("a clock reset to a plausible firmware date is not behind the record")
	}
	if _, ahead := good.AheadOf(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)); ahead {
		t.Error("a run with an implausible clock is called behind")
	}

	f := func(t time.Time) string { return t.Format(time.RFC3339) }
	far := now.Add(10 * day)
	for name, plant := range map[string]string{
		"every date 10 days ahead, in order": `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(far) + `","accounts":[{"store":"k","serial":1,"runs":2,"first_behind":"` +
			f(far.Add(-day)) + `","last_behind":"` + f(far) + `","last_grew":"` + f(far.Add(-day)) + `","seen_at":"` + f(far) + `"}]}`,
		"the record's run alone ahead": `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(now.Add(3*day)) + `"}`,
		"the latest behind run in 2099": `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(now.Add(-time.Hour)) + `","accounts":[{"store":"k","serial":1,"runs":2,"first_behind":"` +
			f(now.Add(-day)) + `","last_behind":"2099-01-01T00:00:00Z","last_grew":"` + f(now.Add(-day)) + `"}]}`,
	} {
		r := mustReadPlant(t, out, plant)
		if r.Damaged() {
			t.Errorf("%s: damaged (it is a clock behind, the lead's call)", name)
		}
		if _, ahead := r.AheadOf(now); !ahead {
			t.Errorf("%s: not ahead of the clock", name)
		}
	}
	// Out of order is damaged whatever the clock: progress in 2099 after a latest behind run of today.
	bad := `{"version":1,"serial":1,"run":2,"last_run_at":"` + f(now.Add(-time.Hour)) + `","accounts":[{"store":"k","serial":1,"runs":2,"first_behind":"` +
		f(now.Add(-day)) + `","last_behind":"` + f(now.Add(-time.Hour)) + `","last_grew":"2099-01-01T00:00:00Z"}]}`
	if r := mustReadPlant(t, out, bad); !r.Damaged() {
		t.Error("progress after the latest behind run (in 2099) is not damaged")
	}
}

// covers: MA-1290, MA-1184
// CLOCK-R2-A's dead clock battery, the first backup of a streak: a gap of more than
// TBStreakUnseenFor (30 days) since the streak's latest behind backup restarts its times at this
// backup, as a clock that went back does: the serial and the sent flags are kept. So a streak whose
// first backup read a firmware date (2025-03-01) and whose next read the right one never claims
// "still going after 610 days". A gap of 30 days exactly keeps the first time. Positive twin first:
// a weekly gap keeps the streak's first time.
func TestALongGapRestartsTheStreaksTimes(t *testing.T) {
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
	weekly := TBStreak{}.Behind("k", "imap.k.example", 100, t0).Behind("k", "imap.k.example", 200, t0.Add(7*day))
	if a := acct(weekly); !a.FirstBehind.Equal(t0) || !a.StillDownloading() {
		t.Fatalf("positive twin: a weekly gap: %+v", a)
	}
	at30 := weekly.Behind("k", "imap.k.example", 300, t0.Add(37*day))
	if a := acct(at30); !a.FirstBehind.Equal(t0) {
		t.Errorf("a gap of 30 days exactly restarted the streak: %+v", a)
	}

	sent, _ := weekly.MarkSent("k", acct(weekly).Serial, false)
	gap := sent.Behind("k", "imap.k.example", 300, t0.Add(7*day+30*day+time.Second))
	a := acct(gap)
	if !a.FirstBehind.Equal(t0.Add(37*day+time.Second)) || !a.LastGrew.Equal(a.FirstBehind) || a.NoProgressRuns != 0 || !a.FirstNoProgress.IsZero() {
		t.Errorf("a gap of more than 30 days kept the old times: %+v", a)
	}
	if a.Serial != acct(weekly).Serial || !a.StillSent {
		t.Errorf("a gap of more than 30 days lost the serial or the sent flag: %+v", a)
	}

	bios := TBStreak{}.Behind("k", "imap.k.example", 100, time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)).Behind("k", "imap.k.example", 100, t0)
	if a := acct(bios); !a.FirstBehind.Equal(t0) || a.StillDownloading() {
		t.Errorf("a firmware-date first backup: the streak spans %v", a.LastBehind.Sub(a.FirstBehind))
	}
}

// covers: MA-1281
// PLANT-R2 (the lead's call): a pair of fields no backup writes apart is damaged. A first backup
// without progress with no count of them (the pass's plant: it moved the stopped email 22 hours
// early), a count with no first one, the stopped email marked sent but not the still one (the
// stopped mark sets both), as many backups without progress as backups (a streak's first backup
// never counts one), a first backup without progress with no progress time, and a count of failed
// sends past AlertSendTries. Each plant holds that one fault. Positive twin first: a consistent
// entry, with each count of failed sends at the cap itself.
func TestAnInconsistentPairIsDamaged(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	f := func(t time.Time) string { return t.Format(time.RFC3339) }
	grew, mid, last := now.Add(-3*day), now.Add(-2*day), now.Add(-time.Hour)
	type fields struct {
		runs, noProgress, stillTries, stoppedTries int
		firstNoProgress, lastGrew                  time.Time
		stillSent, stoppedSent                     bool
	}
	plant := func(p fields) string {
		opt := func(k string, v time.Time) string {
			if v.IsZero() {
				return ""
			}
			return `,"` + k + `":"` + f(v) + `"`
		}
		return `{"version":1,"serial":1,"run":9,"last_run_at":"` + f(last) + `","accounts":[{"store":"k","serial":1,"runs":` + strconv.Itoa(p.runs) +
			`,"no_progress_runs":` + strconv.Itoa(p.noProgress) + `,"still_downloading_tries":` + strconv.Itoa(p.stillTries) +
			`,"stopped_tries":` + strconv.Itoa(p.stoppedTries) + `,"still_downloading_sent":` + strconv.FormatBool(p.stillSent) +
			`,"stopped_sent":` + strconv.FormatBool(p.stoppedSent) + `,"first_behind":"` + f(grew) + `","last_behind":"` + f(last) + `"` +
			opt("last_grew", p.lastGrew) + opt("first_no_progress", p.firstNoProgress) + `}]}`
	}
	ok := fields{runs: 4, noProgress: 2, firstNoProgress: mid, lastGrew: grew, stillTries: AlertSendTries, stoppedTries: AlertSendTries, stillSent: true}
	out := t.TempDir()
	if r := mustReadPlant(t, out, plant(ok)); r.Damaged() {
		t.Fatalf("positive twin: a consistent entry is damaged: %+v", r)
	}
	both := ok
	both.stoppedSent = true
	if r := mustReadPlant(t, out, plant(both)); r.Damaged() {
		t.Fatalf("positive twin: both emails sent is damaged: %+v", r)
	}
	one := func(change func(*fields)) string {
		p := ok
		change(&p)
		return plant(p)
	}
	for name, p := range map[string]string{
		"a first backup without progress, no count (PLANT-R2)": one(func(p *fields) { p.noProgress = 0 }),
		"a count, no first backup without progress":            one(func(p *fields) { p.firstNoProgress = time.Time{} }),
		"stopped sent, still not":                              one(func(p *fields) { p.stillSent, p.stoppedSent = false, true }),
		"as many backups without progress as backups":          one(func(p *fields) { p.runs = 2 }),
		"a first backup without progress, no progress time":    one(func(p *fields) { p.lastGrew = time.Time{} }),
		"still-downloading sends past the cap":                 one(func(p *fields) { p.stillTries = AlertSendTries + 1 }),
		"stopped sends past the cap":                           one(func(p *fields) { p.stoppedTries = AlertSendTries + 1 }),
	} {
		if r := mustReadPlant(t, out, p); !r.Damaged() {
			t.Errorf("%s: not damaged", name)
		}
	}
}

// covers: MA-1291
// SPAM-R2-A: the record counts the failed sends of each email of a streak, beside its sent flag: up
// to AlertSendTries and no further, for that record, store, streak and kind only, and a failure
// never marks the email sent. The counts survive the record's write and read. Positive twin first:
// an unmarked streak has no failed send.
func TestTheRecordCountsFailedSends(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	s := TBStreak{}.Behind("k", "imap.k.example", 1, t0).Behind("k", "imap.k.example", 9, t0.Add(80*time.Hour)).Named()
	a, _ := s.Account("k")
	n := TBNotice{RecordID: s.RecordID, Store: "k", Serial: a.Serial}
	if s.Tries(n) != 0 {
		t.Fatalf("positive twin: an unmarked streak has %d failed sends", s.Tries(n))
	}
	for i := 1; i <= AlertSendTries+2; i++ {
		var ok bool
		if s, ok = s.MarkFailed("k", a.Serial, false); !ok {
			t.Fatalf("send %d: the streak was not found", i)
		}
		if got := s.Tries(n); got != min(i, AlertSendTries) {
			t.Errorf("after %d failed sends the record says %d", i, got)
		}
	}
	if s.Sent(n) {
		t.Error("failed sends marked the email sent")
	}
	stopped := n
	stopped.Stopped = true
	if s.Tries(stopped) != 0 {
		t.Errorf("the stopped email has the still one's count: %d", s.Tries(stopped))
	}
	if s, _ = s.MarkFailed("k", a.Serial, true); s.Tries(stopped) != 1 || s.Tries(n) != AlertSendTries {
		t.Errorf("the stopped email's own count: stopped %d, still %d", s.Tries(stopped), s.Tries(n))
	}
	if _, ok := s.MarkFailed("k", a.Serial+1, false); ok {
		t.Error("another streak's failure was counted on this one")
	}
	other := n
	other.RecordID = "AnotherRecord0123"
	if s.Tries(other) != 0 {
		t.Error("another record's notice reads this record's count")
	}
	out := t.TempDir()
	if err := WriteTBStreak(out, s); err != nil {
		t.Fatal(err)
	}
	back, err := ReadTBStreak(out)
	if err != nil || back.Tries(n) != AlertSendTries || back.Tries(stopped) != 1 || back.Damaged() {
		t.Errorf("read back: %v still %d stopped %d damaged %v", err, back.Tries(n), back.Tries(stopped), back.Damaged())
	}
}
