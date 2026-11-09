package app

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The C5 adversarial pass round 3 (docs/review-thunderbird-completeness-c5-adversarial-r3.md; its
// attack tests written in the clone .claude/attack-c5-r3 and brought in here), the lead's calls of
// 2026-11-09: TIE-R3's hardening, the pass's random search kept as a guard, and GAP-R3-C accepted as
// a documented residual (a skipped test citing its row).

// covers: MA-1290, MA-1281
// TIE-R3, through the real fold (the pass's TestR3Note_ExactTieIsDamage): a backup whose clock reads
// exactly the streak's first behind time, before its latest, is a step back; the next backup gives
// no tbstreak_unreadable. Positive twin first: the same history a nanosecond off.
func TestAClockOnTheFirstTimeIsNotDamage(t *testing.T) {
	for _, back := range []time.Duration{time.Nanosecond, 0} {
		out := tmpDir(t)
		r2Fold(t, out, c5Behind("k", 100), aT0)
		r2Fold(t, out, c5Behind("k", 100), aT0.Add(time.Hour))
		r2Fold(t, out, c5Behind("k", 100), aT0.Add(back))
		ws, _ := r2Fold(t, out, c5Behind("k", 100), aT0.Add(3*time.Hour))
		if len(ws) != 0 {
			t.Errorf("TIE-R3 (the clock %v after the first time): %+v", back, ws)
		}
	}
}

// covers: MA-1290, MA-1281, MA-1291
// The pass's TestR3Attack_NormalHistoriesAreNeverDamaged (PAIR-R3-A), kept as a guard: 20 seeded
// histories of 80 backups (the pass ran 150, which takes over two minutes here; its seeds 1 to 20 are
// kept), two accounts, real time moving by an hour, a day, 3 days or a 31-to-40-day
// gap; the clock right, hours behind or ahead (inside the one-day grace), 3 days behind, a firmware
// date or 2001; X growing, stuck, caught up or falling; each due notice marked sent, marked failed
// (under a delivery id that changes now and then, CAP-R3-A) or left. No backup gives
// tbstreak_unreadable and no written record is Damaged. (Signatures adapted: the fold helper is
// r2Fold's, and MarkTBNoticesFailed takes the delivery id and the time.)
func TestNormalHistoriesAreNeverDamaged(t *testing.T) {
	firmware := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	bad, folds := 0, 0
	jit := func(rng *rand.Rand) time.Duration {
		return time.Duration(rng.Intn(3600))*time.Second + time.Duration(rng.Intn(1e9))
	}
	const seeds = 20
	for seed := int64(1); seed <= seeds && bad < 5; seed++ {
		rng := rand.New(rand.NewSource(seed))
		out := tmpDir(t)
		real := aT0
		x := map[string]int64{"a": 100, "b": 100}
		since := ""
		var hist []string
		for step := 0; step < 80 && bad < 5; step++ {
			switch p := rng.Intn(100); {
			case p < 50:
				real = real.Add(time.Hour)
			case p < 80:
				real = real.Add(aDay)
			case p < 94:
				real = real.Add(3 * aDay)
			default:
				real = real.Add(time.Duration(31+rng.Intn(10)) * aDay)
			}
			real = real.Add(jit(rng) / 4)
			clock := real
			switch p := rng.Intn(100); {
			case p < 70:
			case p < 76:
				clock = real.Add(-time.Duration(1+rng.Intn(20))*time.Hour - jit(rng))
			case p < 82:
				clock = real.Add(time.Duration(1+rng.Intn(20))*time.Hour + jit(rng))
			case p < 88:
				clock = real.Add(-3 * aDay)
			case p < 94:
				clock = firmware.Add(time.Duration(step) * time.Minute)
			default:
				clock = time.Date(2001, 1, 1, 0, 0, step, 0, time.UTC)
			}
			var rs []tbcomplete.Result
			for _, id := range []string{"a", "b"} {
				r := c5Behind(id, x[id])
				switch p := rng.Intn(100); {
				case p < 35:
					x[id] += int64(1 + rng.Intn(50))
					r.OnDisk = x[id]
				case p < 90:
				case p < 95:
					r.Verdict = tbcomplete.CaughtUp
				default:
					x[id] = max(0, x[id]-int64(rng.Intn(30)))
					r.OnDisk = x[id]
				}
				rs = append(rs, r)
			}
			ws, ns := foldTBStreak(Options{Out: out}, rs, false, true, clock, nil)
			folds++
			hist = append(hist, fmt.Sprintf("real %s clock %s x=%v notices=%d", real.Format(time.RFC3339Nano), clock.Format(time.RFC3339Nano), x, len(ns)))
			for _, w := range ws {
				if w.Code == tbStreakUnreadable {
					t.Errorf("seed %d, step %d: a normal history gave the damaged WARN: %s\n%s", seed, step, w.Message, strings.Join(hist, "\n"))
					bad++
				}
			}
			if s, err := state.ReadTBStreak(out); err == nil && s.Damaged() {
				t.Errorf("seed %d, step %d: the written record is Damaged: %+v\n%s", seed, step, s, strings.Join(hist, "\n"))
				bad++
			}
			if rng.Intn(10) == 0 {
				since = fmt.Sprintf("id-%d", step) // an email got through somewhere
			}
			if len(ns) > 0 {
				switch p := rng.Intn(10); {
				case p < 6:
					if err := MarkTBNoticesSent(out, ns); err != nil {
						t.Fatal(err)
					}
				case p < 9:
					if err := MarkTBNoticesFailed(out, ns, since, clock); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	if folds < seeds*80 {
		t.Errorf("only %d backups folded", folds)
	}
}

// covers: MA-1290
// GAP-R3-C (LOW), accepted by the lead on 2026-11-09 as a documented residual (scenario-catalog
// MA-1290; design-thunderbird-completeness.md §5's round-3 note): a record with no dates yet, whose
// first backup's clock reads 3 to 30 days slow, gives the still-downloading email one real hour into
// the streak, saying "still going after N days" with N the clock's error, once per such streak. A fix
// needs a sign that the first clock was wrong, and a 2-backup streak spanning 3 to 30 days is also a
// daily schedule after a week switched off. The pass's TestR3Attack_SlowClockAtTheFirstBackup is kept
// here, skipped: if it ever passes, remove the skip so it becomes a guard.
func TestASlowClockAtTheFirstBackupGivesNoEarlyEmail(t *testing.T) {
	t.Skip("MA-1290 documented residual GAP-R3-C (the lead's call of 2026-11-09): a first backup on a clock 3 to 30 days slow gives one early still email")
	for _, n := range []int{2, 5, 10, 29, 31} {
		out := tmpDir(t)
		r2Fold(t, out, c5Behind("k", 100), aT0.Add(-time.Duration(n)*aDay))
		_, ns := r2Fold(t, out, c5Behind("k", 150), aT0.Add(time.Hour))
		if len(ns) > 0 {
			t.Errorf("GAP-R3-C (%d days slow): the still email is due one real hour into the streak: %q", n, strings.Join(state.TBNoticeEmail(ns, aT0.Add(time.Hour)).Lines, " "))
		}
	}
}
