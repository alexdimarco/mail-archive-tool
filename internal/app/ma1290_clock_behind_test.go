package app

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The C5 adversarial pass round 2 (docs/review-thunderbird-completeness-c5-adversarial-r2.md; its
// attack tests written red in the clone .claude/attack-c5-r2 and brought in here, each with the
// result it now owes), the lead's calls of 2026-11-09: CLOCK-R2-A, WARN-R2 and PLANT-R2 in the fold.

func r2Fold(t *testing.T, out string, r tbcomplete.Result, now time.Time) ([]state.RunWarning, []state.TBNotice) {
	t.Helper()
	return foldTBStreak(Options{Out: out}, []tbcomplete.Result{r}, false, true, now, nil)
}

// clockBehindWords is the tbstreak_clock_behind WARN's sentence for a backup whose clock read now
// (the lead's words of 2026-11-09).
func clockBehindWords(now time.Time) string {
	return "This backup's clock read " + now.Local().Format("Jan 2, 2006") + ", earlier than a previous backup of this archive, so MailArchive did not count it toward how long Thunderbird has been downloading. Check this computer's date and time."
}

// covers: MA-1290
// CLOCK-R2-A, the rule: a backup whose clock is more than a day earlier than the latest date in the
// streak record records NOTHING to it (the file's bytes unchanged: no reset, the sent flags kept)
// and is due no email; it logs one line and returns the tbstreak_clock_behind WARN in the lead's
// words, never tbstreak_unreadable. Positive twin first: a clock 23 hours behind is folded (MA-891's
// rule: the first time restarts, the serial and the flags are kept), with no WARN.
func TestAClockBehindRecordsNothing(t *testing.T) {
	twin := tmpDir(t)
	r2Fold(t, twin, c5Behind("k", 100), aT0)
	r2Fold(t, twin, c5Behind("k", 200), aT0.Add(3*aDay))
	if n := aFold(t, twin, c5Behind("k", 300), aT0.Add(3*aDay+time.Hour)); len(n) != 1 {
		t.Fatalf("positive twin: not due the still email: %+v", n)
	} else if err := MarkTBNoticesSent(twin, n); err != nil {
		t.Fatal(err)
	}
	ws, n := r2Fold(t, twin, c5Behind("k", 300), aT0.Add(3*aDay+time.Hour-23*time.Hour))
	if a, _ := mustStreak(t, twin).Account("k"); len(ws) != 0 || n != nil || !a.StillSent || a.Runs != 4 {
		t.Fatalf("positive twin: a clock 23 hours behind: WARNs %+v, notices %+v, entry %+v", ws, n, a)
	}

	out := tmpDir(t)
	r2Fold(t, out, c5Behind("k", 100), aT0)
	r2Fold(t, out, c5Behind("k", 200), aT0.Add(3*aDay))
	if n := aFold(t, out, c5Behind("k", 300), aT0.Add(3*aDay+time.Hour)); len(n) != 1 {
		t.Fatalf("not due the still email: %+v", n)
	} else if err := MarkTBNoticesSent(out, n); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, state.TBStreakName)
	before, _ := os.ReadFile(path)
	behind := aT0.Add(3*aDay + time.Hour - 25*time.Hour)
	var buf bytes.Buffer
	ws, n = foldTBStreak(Options{Out: out}, []tbcomplete.Result{c5Behind("k", 300)}, false, true, behind, log.New(&buf, "", 0))
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Errorf("a clock more than a day behind changed the record:\n%s\n%s", before, after)
	}
	if n != nil {
		t.Errorf("a clock more than a day behind is due an email: %+v", n)
	}
	if len(ws) != 1 || ws[0].Code != tbStreakClockBehind || ws[0].Message != clockBehindWords(behind) {
		t.Errorf("the WARN: %+v, want %q", ws, clockBehindWords(behind))
	}
	if lines := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1; buf.Len() == 0 || lines != 1 || !strings.Contains(buf.String(), "clock read") || !strings.Contains(buf.String(), path) {
		t.Errorf("the log (want one line naming the record): %q", buf.String())
	}
	// The next backup at the right time continues the same streak, flags kept: nothing due.
	if ws, n := r2Fold(t, out, c5Behind("k", 300), aT0.Add(3*aDay+2*time.Hour)); len(ws) != 0 || n != nil {
		t.Errorf("the backup after the clock was put right: WARNs %+v, notices %+v", ws, n)
	}
	if a, _ := mustStreak(t, out).Account("k"); !a.StillSent || !a.FirstBehind.Equal(aT0) || a.Runs != 4 {
		t.Errorf("the streak after the clock was put right: %+v", a)
	}
}

// coldBoots is CLOCK-R2-A's dead clock battery (the pass's TestR2Attack_ColdBootClockResetRepeatsTheEmail):
// for 6 days, a backup at each cold start reads the firmware date (bios, a minute later each day),
// then 8 hourly backups read the right time; every notice due is "sent" (marked). x gives the
// on-disk count of the i-th backup. It returns the notices, the WARN codes and the WARNs' words.
func coldBoots(t *testing.T, bios time.Time, x func(i int) int64) (notices []state.TBNotice, codes map[string]int, words []string) {
	t.Helper()
	out := tmpDir(t)
	codes = map[string]int{}
	i := 0
	fold := func(now time.Time) {
		ws, n := r2Fold(t, out, c5Behind("k", x(i)), now)
		i++
		for _, w := range ws {
			codes[w.Code]++
			words = append(words, w.Message)
		}
		notices = append(notices, n...)
		if err := MarkTBNoticesSent(out, n); err != nil {
			t.Fatal(err)
		}
	}
	for d := 0; d < 6; d++ {
		fold(bios.Add(time.Duration(d) * time.Minute))
		for h := 1; h <= 8; h++ {
			fold(aT0.Add(time.Duration(d)*aDay + time.Duration(h)*time.Hour))
		}
	}
	return notices, codes, words
}

// covers: MA-1290, MA-1184
// CLOCK-R2-A, re-tested as the lead asked: a dead clock battery resetting to 2025-03-01 at each of 6
// cold starts sends ONE email over the 6 days, never one counting the firmware date ("610 days"):
// X stuck, the one email is the stopped one, since the first right-time backup; X growing, the one
// email is the still-downloading one, "after 3 days". Each cold start after the first leaves the
// tbstreak_clock_behind WARN naming the date it read (5), never tbstreak_unreadable. The pass's
// baseline (a385f97) sent 1; its finding sent 6. Positive twin first: the implausible reset (2001,
// the pass's TestR2Attack_ImplausibleResetDoesNotRepeat) sends one email and no WARN.
func TestADeadClockBatteryEmailsOnce(t *testing.T) {
	n, codes, _ := coldBoots(t, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC), func(int) int64 { return 100 })
	if len(n) != 1 || len(codes) != 0 {
		t.Fatalf("positive twin: an implausible reset: %d emails, WARNs %v", len(n), codes)
	}

	bios := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	stuck, codes, words := coldBoots(t, bios, func(int) int64 { return 100 })
	if len(stuck) != 1 || !stuck[0].Stopped {
		t.Errorf("X stuck: %d emails (%+v), want one stopped email", len(stuck), stuck)
	}
	if codes[tbStreakClockBehind] != 5 || codes[tbStreakUnreadable] != 0 || len(codes) != 1 {
		t.Errorf("X stuck: WARNs %v, want 5 tbstreak_clock_behind", codes)
	}
	for _, w := range words {
		if !strings.HasPrefix(w, "This backup's clock read Mar 1, 2025, earlier than a previous backup") && !strings.HasPrefix(w, "This backup's clock read Feb 28, 2025,") {
			t.Errorf("the WARN: %q", w)
		}
	}
	growing, codes, _ := coldBoots(t, bios, func(i int) int64 { return int64(100 + i) })
	if len(growing) != 1 || growing[0].Stopped {
		t.Fatalf("X growing: %d emails (%+v), want one still-downloading email", len(growing), growing)
	}
	line := state.TBNoticeEmail(growing, aT0).Lines[0]
	if !strings.HasSuffix(line, "still going after 3 days.") {
		t.Errorf("X growing: the line %q", line)
	}
	if codes[tbStreakClockBehind] != 5 || len(codes) != 1 {
		t.Errorf("X growing: WARNs %v", codes)
	}
	for _, e := range append(stuck, growing...) {
		if e.FirstBehind.Before(aT0) || e.LastGrew.Before(aT0) {
			t.Errorf("an email carries the firmware date: %+v", e)
		}
	}
}

// covers: MA-1290, MA-1184
// The pass's TestR2Attack_FlappingClockSuppresses, with the result it now owes: a clock set 3 days
// back every other day for 20 days, X stuck. The backups behind record nothing and each leaves the
// tbstreak_clock_behind WARN (10); the others make one still-downloading email and then one stopped
// email (the stopped email is no longer suppressed: the count of backups without progress is kept).
// Positive twin first: the same 20 days with a right clock, one stopped email and no WARN.
func TestAFlappingClockStillEmails(t *testing.T) {
	run := func(flap bool) (still, stopped int, codes map[string]int) {
		out := tmpDir(t)
		codes = map[string]int{}
		for d := 0; d < 20; d++ {
			now := aT0.Add(time.Duration(d) * aDay)
			if flap && d%2 == 1 {
				now = now.Add(-3 * aDay)
			}
			ws, n := r2Fold(t, out, c5Behind("k", 100), now)
			for _, w := range ws {
				codes[w.Code]++
			}
			for _, nn := range n {
				if nn.Stopped {
					stopped++
				} else {
					still++
				}
			}
			if err := MarkTBNoticesSent(out, n); err != nil {
				t.Fatal(err)
			}
		}
		return still, stopped, codes
	}
	if still, stopped, codes := run(false); still != 0 || stopped != 1 || len(codes) != 0 {
		t.Fatalf("positive twin: a right clock: still %d stopped %d WARNs %v", still, stopped, codes)
	}
	still, stopped, codes := run(true)
	if still != 1 || stopped != 1 || codes[tbStreakClockBehind] != 10 || len(codes) != 1 {
		t.Errorf("a flapping clock: still %d stopped %d WARNs %v; want 1, 1 and 10 tbstreak_clock_behind", still, stopped, codes)
	}
}

// covers: MA-1281
// WARN-R2 (the lead's words): the damaged-record WARN no longer mentions clocks and no longer says
// "Nothing is needed"; it says the counts or dates do not fit together, and that something other
// than MailArchive is changing the file if it shows after every backup. Positive twin first: a
// record of real folds gets no WARN.
func TestTheDamagedWarningWords(t *testing.T) {
	twin := tmpDir(t)
	r2Fold(t, twin, c5Behind("k", 100), aT0)
	if ws, _ := r2Fold(t, twin, c5Behind("k", 100), aT0.Add(time.Hour)); len(ws) != 0 {
		t.Fatalf("positive twin: %+v", ws)
	}
	out := tmpDir(t)
	tbWrite(t, filepath.Join(out, state.TBStreakName), `{"version":1,"serial":1,"run":1,"accounts":[{"store":"k","serial":2,"runs":1}]}`)
	ws, _ := r2Fold(t, out, c5Behind("k", 100), aT0)
	path := filepath.Join(out, state.TBStreakName)
	want := "MailArchive did not trust its record of how long Thunderbird has been downloading (" + path + "), because it holds counts or dates that do not fit together, so it started that count again at the last backup. If this shows after every backup, something other than MailArchive is changing that file."
	if len(ws) != 1 || ws[0].Code != tbStreakUnreadable || ws[0].Message != want {
		t.Errorf("the damaged-record WARN: %+v\nwant %q", ws, want)
	}
	if len(ws) == 1 && (strings.Contains(strings.ToLower(ws[0].Message), "clock") || strings.Contains(ws[0].Message, "Nothing is needed")) {
		t.Errorf("the damaged-record WARN mentions a clock or says nothing is needed: %q", ws[0].Message)
	}
}

// covers: MA-1281, MA-1188
// The pass's TestR2Attack_PlantedNoProgressWithoutCount (PLANT-R2), with the result it now owes: a
// planted first_no_progress with no_progress_runs 0 (last progress 4 days ago) is damaged: the first
// backup gets the tbstreak_unreadable WARN, the streak starts fresh, and no stopped email is due in
// the next hours (the pass: due 2 hours into the planted window). Positive twin first: the same
// record with the count that goes with the date (3) is not damaged.
func TestAttackC5R2_PlantedNoProgressWithoutCount(t *testing.T) {
	plant := func(out string, count int) {
		s := state.TBStreak{}.StartRun(aT0.Add(-time.Hour))
		s.Serial = 1
		s.Accounts = []state.TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1,
			FirstBehind: aT0.Add(-4 * aDay), LastBehind: aT0.Add(-time.Hour), LastGrew: aT0.Add(-4 * aDay),
			FirstNoProgress: aT0.Add(-4*aDay + time.Minute), NoProgressRuns: count,
			FirstX: 100, GrewX: 100, LastX: 100, Runs: 5, SeenRun: s.Run, BehindRun: s.Run, SeenAt: aT0.Add(-time.Hour)}}
		if err := state.WriteTBStreak(out, s.Named()); err != nil {
			t.Fatal(err)
		}
	}
	twin := tmpDir(t)
	plant(twin, 3)
	if ws, _ := r2Fold(t, twin, c5Behind("k", 100), aT0); len(ws) != 0 {
		t.Fatalf("positive twin: a consistent record: %+v", ws)
	}
	out := tmpDir(t)
	plant(out, 0)
	for h := 0; h < 3; h++ {
		ws, n := r2Fold(t, out, c5Behind("k", 100), aT0.Add(time.Duration(h)*time.Hour))
		if h == 0 && (len(ws) != 1 || ws[0].Code != tbStreakUnreadable) {
			t.Errorf("the planted pair: WARNs %+v, want tbstreak_unreadable", ws)
		}
		if len(n) > 0 {
			t.Errorf("PLANT-R2 CONFIRMED: due %+v %d hours into the planted window", n, h)
		}
	}
	if a, _ := mustStreak(t, out).Account("k"); !a.FirstBehind.Equal(aT0) {
		t.Errorf("not started fresh: %+v", a)
	}
}

// covers: MA-1291
// RecordRunWarning, the alert layer's way to put a WARN in the last-run record after the run (the
// record a run wrote, which status and Status & health read): added once per code, under the
// archive's lock; never to a record a run is writing, to an encrypted archive's (its allow-list
// keeps no warnings), or when no record exists; a held lock leaves the record as it was. Positive
// twin first: a finished ok record gets the WARN.
func TestRecordRunWarningAddsToTheLastRun(t *testing.T) {
	w := state.RunWarning{Code: "alert_send_failed", Message: "MailArchive could not send its email about Thunderbird (5 tries)."}
	out := tmpDir(t)
	ok := state.LastRun{Status: state.RunOK, Started: aT0, Finished: aT0.Add(time.Minute), Warnings: []state.RunWarning{{Code: "tbstreak_full", Message: "x"}}}
	if err := state.WriteLastRun(out, ok); err != nil {
		t.Fatal(err)
	}
	if err := RecordRunWarning(out, w); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if err := RecordRunWarning(out, w); err != nil {
		t.Fatal(err)
	}
	lr, _, _ := state.ReadLastRun(out)
	if len(lr.Warnings) != 2 || lr.Warnings[1] != w || lr.Status != state.RunOK || !lr.Finished.Equal(ok.Finished) {
		t.Fatalf("positive twin: the record %+v", lr)
	}

	absent := tmpDir(t)
	if err := RecordRunWarning(absent, w); err == nil {
		t.Error("no record: no error")
	}
	if _, st, _ := state.ReadLastRun(absent); st != state.LastRunAbsent {
		t.Error("no record: one was made")
	}
	running := tmpDir(t)
	if err := state.WriteLastRun(running, state.LastRun{Status: state.RunRunning, Started: aT0}); err != nil {
		t.Fatal(err)
	}
	if err := RecordRunWarning(running, w); err == nil {
		t.Error("a running record: no error")
	}
	if lr, _, _ := state.ReadLastRun(running); lr.Warnings != nil {
		t.Errorf("a running record was given %+v", lr.Warnings)
	}
	enc := tmpDir(t)
	if err := state.WriteLastRun(enc, state.LastRun{Status: state.RunOK, Started: aT0, Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	if err := RecordRunWarning(enc, w); err == nil {
		t.Error("an encrypted archive's record: no error")
	}
	if b, _ := os.ReadFile(filepath.Join(enc, state.LastRunName)); strings.Contains(string(b), "alert_send_failed") {
		t.Errorf("an encrypted archive's record holds the WARN: %s", b)
	}
	held := tmpDir(t)
	if err := state.WriteLastRun(held, ok); err != nil {
		t.Fatal(err)
	}
	l, err := lockfile.AcquireAs(filepath.Join(held, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	old := tbMarkWait
	tbMarkWait = 100 * time.Millisecond
	err = RecordRunWarning(held, w)
	tbMarkWait = old
	l.Release()
	if err == nil || !errors.Is(err, lockfile.ErrHeld) && !strings.Contains(err.Error(), "lock") {
		t.Errorf("a held lock: %v", err)
	}
	if lr, _, _ := state.ReadLastRun(held); len(lr.Warnings) != 1 {
		t.Errorf("a held lock: the record %+v", lr.Warnings)
	}
}
