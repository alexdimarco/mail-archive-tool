package app

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The C5 adversarial pass's tests (docs/review-thunderbird-completeness-c5-adversarial.md; written
// red in the pass's clone .claude/attack-c5 and brought in by the fix round, each with the positive
// result it now owes beside the pass's own check).

var aT0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

const aDay = 24 * time.Hour

func aFold(t *testing.T, out string, r tbcomplete.Result, now time.Time) []state.TBNotice {
	t.Helper()
	_, n := foldTBStreak(Options{Out: out}, []tbcomplete.Result{r}, false, true, now, nil)
	return n
}

// covers: MA-1184, MA-1188
// FALSE-01: the computer is off for 4 days between two behind runs; the first run after it finds X
// unchanged (Thunderbird had no chance to download). "A computer that was switched off trips
// nothing" (§5): the stopped email needs 3 backups without progress across 3 days, spanning a day
// themselves (the lead's call of 2026-11-09). The still-downloading email is due (MA-891's rule,
// measured between runs). Positive twin: hourly backups without progress, a day after the gap, are
// due the stopped email, once.
func TestAttackC5_SwitchedOffFourDaysSendsStopped(t *testing.T) {
	out := tmpDir(t)
	aFold(t, out, c5Behind("k", 100), aT0)
	n := aFold(t, out, c5Behind("k", 100), aT0.Add(4*aDay))
	t.Logf("notices after a 4-day gap: %+v", n)
	if len(n) == 1 && n[0].Stopped {
		e := state.TBNoticeEmail(n, aT0.Add(4*aDay))
		t.Errorf("FALSE-01 CONFIRMED: the first run after a 4-day switch-off is due the STOPPED email: %q", e.Lines)
	}
	if len(n) != 1 || n[0].Stopped {
		t.Fatalf("the gap's still-downloading email: %+v", n)
	}
	if err := MarkTBNoticesSent(out, n); err != nil {
		t.Fatal(err)
	}
	// Amended 2026-11-09 (the lead's call): hourly backups after the gap are due nothing for the
	// first 23 hours (nor at 23h59) though 3 of them found no progress; at 24 hours, exactly one
	// stopped email; none after it.
	for h := 1; h <= 23; h++ {
		if n = aFold(t, out, c5Behind("k", 100), aT0.Add(4*aDay+time.Duration(h)*time.Hour)); n != nil {
			t.Fatalf("hourly, %d hours after the gap: due %+v", h, n)
		}
	}
	if n = aFold(t, out, c5Behind("k", 100), aT0.Add(4*aDay+23*time.Hour+59*time.Minute)); n != nil {
		t.Fatalf("23h59 after the gap: due %+v", n)
	}
	stopped := 0
	for h := 24; h <= 30; h++ {
		n = aFold(t, out, c5Behind("k", 100), aT0.Add(4*aDay+time.Duration(h)*time.Hour))
		for _, nn := range n {
			if !nn.Stopped || !nn.LastGrew.Equal(aT0) {
				t.Errorf("hourly, %d hours after the gap: %+v", h, nn)
			}
			stopped++
		}
		if h == 24 && len(n) != 1 {
			t.Errorf("positive twin: a day of hourly backups without progress after the gap: %+v", n)
		}
		if err := MarkTBNoticesSent(out, n); err != nil {
			t.Fatal(err)
		}
	}
	if stopped != 1 {
		t.Errorf("want exactly one stopped email, got %d", stopped)
	}
}

// covers: MA-1184
// FALSE-02: X falls mid-streak (a bulk delete, a folder repaired and fetched again) and then grows
// run to run, but stays under the streak's high mark for 3 days: progress is measured against the
// previous backup, so it is the still-downloading email, never "stopped".
func TestAttackC5_XDropThenGrowthSendsStopped(t *testing.T) {
	out := tmpDir(t)
	aFold(t, out, c5Behind("k", 1000), aT0)
	aFold(t, out, c5Behind("k", 200), aT0.Add(1*aDay))
	aFold(t, out, c5Behind("k", 500), aT0.Add(2*aDay))
	n := aFold(t, out, c5Behind("k", 800), aT0.Add(3*aDay))
	t.Logf("notices: %+v", n)
	if len(n) == 1 && n[0].Stopped {
		e := state.TBNoticeEmail(n, aT0.Add(3*aDay))
		t.Errorf("FALSE-02 CONFIRMED: X grew 200->500->800 over the last 2 days, yet the STOPPED email is due: %q", e.Lines)
	}
	if len(n) != 1 || n[0].Stopped || !n[0].LastGrew.Equal(aT0.Add(3*aDay)) {
		t.Errorf("X fell then grew: want the still-downloading email with progress today: %+v", n)
	}
}

// covers: MA-1184
// MISS-01, the dead clock battery: the streak's first run had a clock before MinPlausibleTime;
// the first plausible run starts its progress time, so a really stopped account gets the stopped
// email, once.
func TestAttackC5_ImplausibleFirstClockNeverStopped(t *testing.T) {
	out := tmpDir(t)
	aFold(t, out, c5Behind("k", 100), time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	var stopped, still int
	for d := 0; d <= 10; d++ {
		for _, nn := range aFold(t, out, c5Behind("k", 100), aT0.Add(time.Duration(d)*aDay)) {
			if nn.Stopped {
				stopped++
				if !nn.LastGrew.Equal(aT0) {
					t.Errorf("the stopped email's since-date: %v, want %v", nn.LastGrew, aT0)
				}
			} else {
				still++
			}
			if err := MarkTBNoticesSent(out, []state.TBNotice{nn}); err != nil {
				t.Fatal(err)
			}
		}
	}
	a, _ := mustStreak(t, out).Account("k")
	t.Logf("still=%d stopped=%d; entry %+v", still, stopped, a)
	if stopped == 0 {
		t.Errorf("MISS-01 CONFIRMED: X stuck at 100 for 10 days of daily behind runs, no stopped email (LastGrew %v)", a.LastGrew)
	}
	if stopped != 1 || still != 0 {
		t.Errorf("want exactly one stopped email and no still one: still=%d stopped=%d", still, stopped)
	}
}

// covers: MA-1281, MA-1184
// PLANT-01: a planted entry (owner-only file, plausible counters, so the counter check does not
// catch it) with dates no run could have written: progress before the streak began (a), one behind
// run holding two times far apart (b), progress in 2099 (c). Each is a damaged record: the streak
// starts fresh from this run, with a tbstreak_unreadable WARN and a log line; the email never
// carries the planted date, and a really stopped account still gets its stopped email.
func TestAttackC5_PlantedTimesShapeTheEmail(t *testing.T) {
	fold := func(out string, x int64, now time.Time) ([]state.RunWarning, []state.TBNotice, string) {
		var buf bytes.Buffer
		ws, n := foldTBStreak(Options{Out: out}, []tbcomplete.Result{c5Behind("k", x)}, false, true, now, log.New(&buf, "", 0))
		return ws, n, buf.String()
	}
	damaged := func(name string, ws []state.RunWarning, logged string) {
		t.Helper()
		if len(ws) != 1 || ws[0].Code != tbStreakUnreadable || !strings.Contains(ws[0].Message, "holds counts or dates no backup") || !strings.Contains(logged, "counts or dates that could not have been written by a backup") {
			t.Errorf("%s: no damaged-record WARN: %+v, log %q", name, ws, logged)
		}
	}

	out := tmpDir(t)
	s := state.TBStreak{}.StartRun(aT0.Add(-time.Hour))
	s.Serial = 1
	s.Accounts = []state.TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1,
		FirstBehind: aT0.Add(-time.Hour), LastBehind: aT0.Add(-time.Hour), LastGrew: state.MinPlausibleTime.Add(aDay),
		FirstX: 1, GrewX: 1 << 40, Runs: 1, SeenRun: s.Run, BehindRun: s.Run, SeenAt: aT0.Add(-time.Hour)}}
	if err := state.WriteTBStreak(out, s.Named()); err != nil {
		t.Fatal(err)
	}
	ws, n, logged := fold(out, 500, aT0)
	if len(n) == 1 && n[0].Stopped {
		e := state.TBNoticeEmail(n, aT0)
		t.Errorf("PLANT-01 CONFIRMED (early stopped, date before the streak): %q", e.Lines)
	}
	damaged("a", ws, logged)
	if a, _ := mustStreak(t, out).Account("k"); n != nil || a.Runs != 1 || !a.LastGrew.Equal(aT0) || a.GrewX != 500 {
		t.Errorf("a: not started fresh: %+v, notices %+v", a, n)
	}

	out2 := tmpDir(t)
	s2 := state.TBStreak{}.StartRun(aT0.Add(-time.Hour))
	s2.Serial = 1
	s2.Accounts = []state.TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1,
		FirstBehind: state.MinPlausibleTime, LastBehind: aT0.Add(-time.Hour), LastGrew: aT0.Add(-time.Hour),
		FirstX: 1, GrewX: 1, Runs: 1, SeenRun: s2.Run, BehindRun: s2.Run, SeenAt: aT0.Add(-time.Hour)}}
	if err := state.WriteTBStreak(out2, s2.Named()); err != nil {
		t.Fatal(err)
	}
	ws, n, logged = fold(out2, 500, aT0)
	if len(n) == 1 && !n[0].Stopped {
		e := state.TBNoticeEmail(n, aT0)
		t.Errorf("PLANT-01b CONFIRMED (still email on the first real run, planted day count): %q", e.Lines)
	}
	damaged("b", ws, logged)
	if a, _ := mustStreak(t, out2).Account("k"); n != nil || !a.FirstBehind.Equal(aT0) {
		t.Errorf("b: not started fresh: %+v, notices %+v", a, n)
	}

	// A planted LastGrew in the future with GrewX high: X stuck for 10 days, never stopped.
	out3 := tmpDir(t)
	s3 := state.TBStreak{}.StartRun(aT0.Add(-time.Hour))
	s3.Serial = 1
	s3.Accounts = []state.TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1,
		FirstBehind: aT0.Add(-time.Hour), LastBehind: aT0.Add(-time.Hour), LastGrew: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		FirstX: 1, GrewX: 1 << 40, Runs: 1, StillSent: true, SeenRun: s3.Run, BehindRun: s3.Run, SeenAt: aT0.Add(-time.Hour)}}
	if err := state.WriteTBStreak(out3, s3.Named()); err != nil {
		t.Fatal(err)
	}
	got, firstStopped := 0, -1
	for d := 0; d <= 10; d++ {
		ws, n, logged := fold(out3, 500, aT0.Add(time.Duration(d)*aDay))
		if d == 0 {
			damaged("c", ws, logged)
		}
		got += len(n)
		if len(n) == 1 && n[0].Stopped && firstStopped < 0 {
			firstStopped = d
			if n[0].LastGrew.Year() != 2026 {
				t.Errorf("c: the stopped email carries %v", n[0].LastGrew)
			}
		}
	}
	if got == 0 {
		t.Errorf("PLANT-01c CONFIRMED: a planted future LastGrew holds back the stopped email for 10 days of X stuck")
	}
	if firstStopped != 3 {
		t.Errorf("c: the stopped email first came on day %d, want day 3", firstStopped)
	}

	// (d) Every date 10 days ahead, all in order: only "in the future" says it is damaged.
	out4 := tmpDir(t)
	ahead := aT0.Add(10 * aDay)
	s4 := state.TBStreak{}.StartRun(ahead)
	s4.Serial = 1
	s4.Accounts = []state.TBStreakAccount{{Store: "k", Account: "imap.k.example", Serial: 1,
		FirstBehind: ahead, LastBehind: ahead, LastGrew: ahead, FirstX: 500, GrewX: 500, LastX: 500, Runs: 1,
		SeenRun: s4.Run, BehindRun: s4.Run, SeenAt: ahead}}
	if err := state.WriteTBStreak(out4, s4.Named()); err != nil {
		t.Fatal(err)
	}
	ws, n, logged = fold(out4, 500, aT0)
	damaged("d", ws, logged)
	if a, _ := mustStreak(t, out4).Account("k"); n != nil || a.Runs != 1 || !a.FirstBehind.Equal(aT0) {
		t.Errorf("d: not started fresh: %+v, notices %+v", a, n)
	}
}

// covers: MA-1188
// INJ (held by the pass, brought in as a guard): a store folder name with CR/LF, bidi controls and
// 500 characters, through the real fold, the state words and the real message composer: no Bcc
// line in the headers or the body, no bidi control, the name capped.
func TestAttackC5_FolderNameInTheEmail(t *testing.T) {
	out := tmpDir(t)
	evil := "imap.‮gro.elpmaxe‬\r\nBcc: evil@example.org\r\n\r\nX" + strings.Repeat("A", 500)
	r := c5Behind("k", 100)
	r.Account = evil
	aFold(t, out, r, aT0)
	n := aFold(t, out, func() tbcomplete.Result { r.OnDisk = 200; return r }(), aT0.Add(3*aDay))
	if len(n) != 1 {
		t.Fatalf("not due: %+v", n)
	}
	e := state.TBNoticeEmail(n, aT0.Add(3*aDay))
	ev := alert.Event{Result: alert.ResultTBBehind, Archive: "/a\r\nBcc: e@x", Host: "h\r\nBcc: e@x", When: aT0,
		TB: &alert.TBBehind{Stopped: e.Stopped, Accounts: e.Accounts, Lines: e.Lines, Remedy: e.Remedy, Which: e.Which}}
	msg := string(alert.Config{From: "f@example.org", To: "t@example.org"}.Message(ev))
	head, body, _ := strings.Cut(msg, "\r\n\r\n")
	for _, l := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(l), "bcc:") {
			t.Errorf("INJ CONFIRMED: header %q", l)
		}
	}
	for _, l := range strings.Split(body, "\r\n") {
		if strings.HasPrefix(strings.ToLower(l), "bcc:") {
			t.Errorf("INJ (body line) CONFIRMED: %q", l)
		}
	}
	if strings.ContainsAny(msg, "‮‬") {
		t.Errorf("BIDI CONFIRMED in the message")
	}
	if strings.Contains(e.Lines[0], strings.Repeat("A", 100)) {
		t.Errorf("LONG CONFIRMED: %d runes in the line", len([]rune(e.Lines[0])))
	}
	if !strings.HasPrefix(e.Lines[0], "The account kept in the folder imap.") {
		t.Errorf("the line: %q", e.Lines[0])
	}
}
