package state

import (
	"testing"
	"time"
)

// The C5 adversarial pass round 3 (docs/review-thunderbird-completeness-c5-adversarial-r3.md), the
// lead's calls of 2026-11-09: CAP-R3-A's rule in the record (5 failed tries in a row, then at most
// one a day; a delivery starts the count again) and TIE-R3's one-line hardening.

// covers: MA-1291
// The failed-try count is "in a row since the last delivered email": counted under the delivery id
// current at each failure, it reads 0 under any other id, and the next failure under a new id starts
// both kinds' counts again from 1 (the still email's 5 and the stopped email's 2 are gone). The
// latest failure's time is kept and survives a write and read; the record is not damaged. Positive
// twin first: under the same id the count stands.
func TestADeliveryStartsTheCountAgain(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	s := TBStreak{}.Behind("k", "imap.k.example", 1, t0).Behind("k", "imap.k.example", 9, t0.Add(80*time.Hour)).Named()
	a, _ := s.Account("k")
	n := TBNotice{RecordID: s.RecordID, Store: "k", Serial: a.Serial}
	stopped := n
	stopped.Stopped = true
	for i := 0; i < 7; i++ {
		s, _ = s.MarkFailed("k", a.Serial, false, "id-1", t0.Add(time.Duration(i)*time.Hour))
	}
	s, _ = s.MarkFailed("k", a.Serial, true, "id-1", t0.Add(8*time.Hour))
	s, _ = s.MarkFailed("k", a.Serial, true, "id-1", t0.Add(9*time.Hour))
	if s.Tries(n, "id-1") != AlertSendTries || s.Tries(stopped, "id-1") != 2 {
		t.Fatalf("positive twin: under one id, still %d stopped %d", s.Tries(n, "id-1"), s.Tries(stopped, "id-1"))
	}
	if s.Tries(n, "id-2") != 0 || s.Tries(stopped, "id-2") != 0 || s.Tries(n, "") != 0 {
		t.Errorf("CAP-R3-A: a count made under another id still counts: %d %d %d", s.Tries(n, "id-2"), s.Tries(stopped, "id-2"), s.Tries(n, ""))
	}
	at := t0.Add(30 * time.Hour)
	s, _ = s.MarkFailed("k", a.Serial, false, "id-2", at)
	if s.Tries(n, "id-2") != 1 || s.Tries(stopped, "id-2") != 0 {
		t.Errorf("CAP-R3-A: the first failure after a delivery: still %d stopped %d, want 1 and 0", s.Tries(n, "id-2"), s.Tries(stopped, "id-2"))
	}
	out := t.TempDir()
	if err := WriteTBStreak(out, s); err != nil {
		t.Fatal(err)
	}
	back, err := ReadTBStreak(out)
	b, _ := back.Account("k")
	if err != nil || !b.LastFailedTry.Equal(at) || b.TriedSince != "id-2" || back.Tries(n, "id-2") != 1 || back.Damaged() {
		t.Errorf("read back: %v %+v damaged %v", err, b, back.Damaged())
	}
}

// covers: MA-1291
// The daily rule (AlertTryDue): under 5 failures in a row, every due backup tries; at 5, a try is due
// a full day after the latest (not a nanosecond before), or when the latest is more than a day after
// now (a clock that was ahead, or a planted time: never silent for good). Through the record: a
// streak at 5 is not due an hour after its latest failure, is due a day after it, and is due at once
// under a new delivery id. Positive twin first: 4 failures, tried a minute later.
func TestAnEmailThatFailedFiveTimesIsTriedOnceADay(t *testing.T) {
	last := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	if !AlertTryDue(AlertSendTries-1, last, last.Add(time.Minute)) {
		t.Fatal("positive twin: 4 failures in a row, not tried")
	}
	for _, c := range []struct {
		name string
		now  time.Time
		due  bool
	}{
		{"an hour later", last.Add(time.Hour), false},
		{"a nanosecond short of a day", last.Add(AlertRetryEvery - time.Nanosecond), false},
		{"a day later", last.Add(AlertRetryEvery), true},
		{"a week later", last.Add(7 * AlertRetryEvery), true},
		{"the clock went back an hour", last.Add(-time.Hour), false},
		{"the latest is a day ahead exactly", last.Add(-AlertRetryEvery), false},
		{"the latest is more than a day ahead", last.Add(-AlertRetryEvery - time.Second), true},
		{"the latest is a year ahead", last.AddDate(-1, 0, 0), true},
	} {
		if got := AlertTryDue(AlertSendTries, last, c.now); got != c.due {
			t.Errorf("%s: due %v, want %v", c.name, got, c.due)
		}
	}
	s := TBStreak{}.Behind("k", "imap.k.example", 1, last.Add(-80*time.Hour)).Behind("k", "imap.k.example", 9, last.Add(-time.Hour)).Named()
	a, _ := s.Account("k")
	n := TBNotice{RecordID: s.RecordID, Store: "k", Serial: a.Serial}
	for i := AlertSendTries - 1; i >= 0; i-- {
		s, _ = s.MarkFailed("k", a.Serial, false, "id-1", last.Add(-time.Duration(i)*time.Minute))
	}
	if s.TryDue(n, "id-1", last.Add(time.Hour)) || !s.TryDue(n, "id-1", last.Add(AlertRetryEvery)) || !s.TryDue(n, "id-2", last.Add(time.Hour)) {
		t.Errorf("TryDue: an hour later %v, a day later %v, a new id %v; want false, true, true",
			s.TryDue(n, "id-1", last.Add(time.Hour)), s.TryDue(n, "id-1", last.Add(AlertRetryEvery)), s.TryDue(n, "id-2", last.Add(time.Hour)))
	}
}

// covers: MA-1290, MA-1281
// TIE-R3 (the pass's note; the lead's call of 2026-11-09 to apply its one line): a backup whose clock
// reads exactly the streak's first behind time, while that is before its latest, went back like any
// other step back: the first backup without progress is cleared with the growth time, so the next
// backup does not find a record that cannot fit together. The pass's TestR3Note_ExactTieIsDamage,
// at the record. Positive twin first: the same history a nanosecond off.
func TestAClockOnTheFirstTimeIsAStepBack(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	// fold gives the first record, after any backup, that does not fit together (each is what the
	// next backup reads), or ok.
	fold := func(back time.Time) (TBStreakAccount, bool) {
		var s TBStreak
		for _, at := range []time.Time{t0, t0.Add(time.Hour), back, t0.Add(3 * time.Hour)} {
			if s = s.StartRun(at).Behind("k", "imap.k.example", 100, at); s.Damaged() {
				a, _ := s.Account("k")
				return a, false
			}
		}
		return TBStreakAccount{}, true
	}
	if a, ok := fold(t0.Add(time.Nanosecond)); !ok {
		t.Fatalf("positive twin: a nanosecond off is damaged: %+v", a)
	}
	if a, ok := fold(t0); !ok {
		t.Errorf("TIE-R3: a clock exactly on the first time left a record that does not fit together: %+v", a)
	}
}
