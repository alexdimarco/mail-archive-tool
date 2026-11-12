package backupalert

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/uninstall"
)

// CAP-R3-A (docs/review-thunderbird-completeness-c5-adversarial-r3.md, MEDIUM; the lead's call of
// 2026-11-09): the cap is "5 failed tries in a row, then at most one try a day", never silent for
// good. Any email MailArchive delivers (any alert email, and Send test email on the Backup alerts
// card) starts every failed-try count again, the download emails' in the streak record and the sync
// email's in its told-file, so the next due backup tries; the alert_send_failed WARN is added only by
// a run that holds an email with 5 failures in a row and does not get one through, so it is gone
// from the first backup after an email gets through. The pass's attacks were written red in the
// clone .claude/attack-c5-r3 and are brought in here with the results they now owe.

// atClock moves the 5-tries rule's clock (alertNow) to *clk for the test.
func atClock(t *testing.T, clk *time.Time) {
	t.Helper()
	prev := alertNow
	alertNow = func() time.Time { return *clk }
	t.Cleanup(func() { alertNow = prev })
}

// tryHours runs one due backup an hour, hours 0 to last, each through run (one due report) at the
// clock *clk, and gives the hours at which a send was tried (g.fail shrank or an email was sent), and
// the hours whose last-run record holds the alert_send_failed WARN.
func tryHours(t *testing.T, g *streakRig, out string, clk *time.Time, t0 time.Time, last int, run func()) (tried, warned []int) {
	t.Helper()
	for h := 0; h <= last; h++ {
		*clk = t0.Add(time.Duration(h) * time.Hour)
		finishedRun(t, out)
		fails, sent := len(g.fail), len(g.sent)
		run()
		if len(g.fail) != fails || len(g.sent) != sent {
			tried = append(tried, h)
		}
		if len(sendFailedWarn(t, out)) > 0 {
			warned = append(warned, h)
		}
	}
	return tried, warned
}

// hoursFrom is the hours from a to b, inclusive.
func hoursFrom(a, b int) []int {
	var hs []int
	for h := a; h <= b; h++ {
		hs = append(hs, h)
	}
	return hs
}

// covers: MA-1291, MA-1184
// A dead mail server, the download email: hourly due backups for 76 hours, every send failing. It is
// tried at the first 5 (hours 0 to 4), then once a day (hours 28, 52, 76): never more than one try in
// a day after the 5th, and never silent for a whole day. The WARN, in the lead's words, is in every
// run's record from the 5th failure on and never before. Positive twin first: 4 failures in a row
// leave no WARN and the 5th backup still tries.
func TestADeadServerIsTriedOnceADay(t *testing.T) {
	clk := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	atClock(t, &clk)
	tw := newTBRig(t, alertsOnFailure, false)
	tw.fail = failures(4)
	tried, warned := tryHours(t, tw.streakRig, tw.out, &clk, clk, 4, func() { tw.report(false, nil) })
	if !slices.Equal(tried, hoursFrom(0, 4)) || len(warned) != 0 || len(tw.sent) != 1 {
		t.Fatalf("positive twin: tried at %v, WARN at %v, %d sent", tried, warned, len(tw.sent))
	}

	// The server worked once (a test email went, so failures are counted under its delivery id),
	// then died.
	g := newTBRig(t, alertsOnFailure, false)
	if err := SendTest(g.settings, nil, "", nil); err != nil || deliveredID(g.settings) == "" {
		t.Fatalf("the test email before the outage: %v", err)
	}
	g.fail = failures(100)
	t0 := clk
	tried, warned = tryHours(t, g.streakRig, g.out, &clk, t0, 76, func() { g.report(false, nil) })
	if want := []int{0, 1, 2, 3, 4, 28, 52, 76}; !slices.Equal(tried, want) {
		t.Errorf("CAP-R3-A: a dead server was tried at hours %v, want %v (5 in a row, then once a day)", tried, want)
	}
	if !slices.Equal(warned, hoursFrom(4, 76)) {
		t.Errorf("the WARN at hours %v, want every hour from the 5th failure (hour 4) on", warned)
	}
	if ws := sendFailedWarn(t, g.out); len(ws) != 1 || ws[0].Message != sendFailedWords {
		t.Errorf("the WARN's words: %+v, want %q", ws, sendFailedWords)
	}
	if still, _ := g.flags(); still || len(g.sent) != 1 {
		t.Error("a dead server's failures marked the email sent")
	}
}

// covers: MA-1291, MA-910
// The same for the shipped Thunderbird-not-synced email, whose count is in its told-file: 5 in a
// row, then once a day, never told. Positive twin first: a stale run with a working server is told
// at once.
func TestADeadServerIsTriedOnceADaySyncEmail(t *testing.T) {
	clk := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	atClock(t, &clk)
	tw := newSyncRig(t, alertsOnFailure)
	tw.plant(staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped"))
	tw.tell(false)
	if tw.count() != 1 {
		t.Fatalf("positive twin: %d emails", tw.count())
	}

	g := newSyncRig(t, alertsOnFailure)
	s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped").Named()
	g.plant(s)
	if err := SendTest(g.settings, nil, "", nil); err != nil || deliveredID(g.settings) == "" {
		t.Fatalf("the test email before the outage: %v", err)
	}
	g.fail = failures(100)
	tried, warned := tryHours(t, g.streakRig, g.out, &clk, clk, 76, func() { g.tell(false) })
	if want := []int{0, 1, 2, 3, 4, 28, 52, 76}; !slices.Equal(tried, want) {
		t.Errorf("CAP-R3-A (sync email): a dead server was tried at hours %v, want %v", tried, want)
	}
	if !slices.Equal(warned, hoursFrom(4, 76)) {
		t.Errorf("the WARN at hours %v, want every hour from hour 4 on", warned)
	}
	told, err := readSyncAlert(SyncAlertPath(g.settings, g.key))
	if err != nil || told.toldOf(s) || told.Tries != state.AlertSendTries || told.TriedSince != deliveredID(g.settings) {
		t.Errorf("the told-file: %+v (%v), want not told, 5 in a row under the delivery id", told, err)
	}
}

// covers: MA-1291, MA-1184
// The pass's TestR3Attack_OutageThenWorkingServerDownloadEmail, brought in with the clock moving: an
// hourly schedule, the mail server down for the first 5 due backups (hours 0 to 4: a laptop offline
// for an afternoon), then working. The pass ran its 10 backups within a second, where "at most one
// try a day" (the lead's rule) tries nothing until a day has passed or an email has got through, so
// here the backups are an hour apart. The email goes at the first daily try, hour 28, once; the WARN
// is in the records of hours 4 to 27 and in none after. (The pass measured 0 emails and the WARN at
// every backup after, for good.)
func TestAWorkingServerGetsTheDownloadEmailAfterAnOutage(t *testing.T) {
	clk := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	atClock(t, &clk)
	g := newTBRig(t, alertsOnFailure, false)
	g.fail = failures(5)
	tried, warned := tryHours(t, g.streakRig, g.out, &clk, clk, 40, func() { g.report(false, nil) })
	if want := []int{0, 1, 2, 3, 4, 28}; !slices.Equal(tried, want) || len(g.sent) != 1 {
		t.Errorf("CAP-R3-A: tried at hours %v (want %v), %d delivered (want 1)", tried, want, len(g.sent))
	}
	if !slices.Equal(warned, hoursFrom(4, 27)) {
		t.Errorf("the WARN at hours %v, want 4 to 27 and none after the email got through", warned)
	}
	if still, _ := g.flags(); !still {
		t.Error("the delivered email was not marked sent")
	}
}

// covers: MA-1291, MA-910
// The pass's TestR3Attack_OutageThenWorkingServerSyncEmail, brought in the same way: the sync email
// goes at the first daily try after the outage, once, and is told; the WARN is gone after it.
func TestAWorkingServerGetsTheSyncEmailAfterAnOutage(t *testing.T) {
	clk := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	atClock(t, &clk)
	g := newSyncRig(t, alertsOnFailure)
	s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped").Named()
	g.plant(s)
	g.fail = failures(5)
	tried, warned := tryHours(t, g.streakRig, g.out, &clk, clk, 40, func() { g.tell(false) })
	if want := []int{0, 1, 2, 3, 4, 28}; !slices.Equal(tried, want) || g.count() != 1 {
		t.Errorf("CAP-R3-A (sync email): tried at hours %v (want %v), %d delivered (want 1)", tried, want, g.count())
	}
	if !slices.Equal(warned, hoursFrom(4, 27)) {
		t.Errorf("the WARN at hours %v, want 4 to 27", warned)
	}
	if told, err := readSyncAlert(SyncAlertPath(g.settings, g.key)); err != nil || !told.toldOf(s) {
		t.Errorf("the told-file after delivery: %+v (%v)", told, err)
	}
}

// covers: MA-1291, MA-1184, MA-910
// Send test email (backupalert.SendTest, what the Backup alerts card's button calls) that goes starts
// every count again: an archive's download email and another archive's sync email, each with 5
// failures in a row an hour ago (both WARNs shown), are each tried, and delivered, at their very next
// due backup, with no WARN. Any other delivered email does the same: a backup alert sent for a third
// archive. Positive twin first: a test email that fails resets nothing, and the next backup within
// the day tries neither.
func TestASentEmailStartsEveryCountAgain(t *testing.T) {
	clk := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	atClock(t, &clk)
	// Two archives under one Desktop settings file: the download email's and the sync email's.
	g := newTBRig(t, alertsOnFailure, false)
	h := &syncRig{streakRig: g.streakRig, out: filepath.Join(t.TempDir(), "archive2")}
	if err := os.MkdirAll(h.out, 0o700); err != nil {
		t.Fatal(err)
	}
	h.key = KeyFor(h.out)
	s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped").Named()
	h.plant(s)
	g.fail = failures(10)
	t0 := clk
	for i := 0; i < 5; i++ {
		clk = t0.Add(time.Duration(i) * time.Minute)
		finishedRun(t, g.out)
		g.report(false, nil)
		finishedRun(t, h.out)
		h.tell(false)
	}
	if len(g.fail) != 0 || len(sendFailedWarn(t, g.out)) != 1 || len(sendFailedWarn(t, h.out)) != 1 {
		t.Fatalf("setup: %d sends left, WARNs %d and %d", len(g.fail), len(sendFailedWarn(t, g.out)), len(sendFailedWarn(t, h.out)))
	}
	before := deliveredID(g.settings)

	// Positive twin: the test email fails; an hour later neither email is tried.
	g.fail = failures(1)
	if err := SendTest(g.settings, nil, "", nil); err == nil {
		t.Fatal("positive twin: a failing test email reported sent")
	}
	clk = t0.Add(time.Hour)
	g.fail = failures(2)
	finishedRun(t, g.out)
	g.report(false, nil)
	finishedRun(t, h.out)
	h.tell(false)
	if len(g.fail) != 2 || deliveredID(g.settings) != before || len(sendFailedWarn(t, g.out)) != 1 || len(sendFailedWarn(t, h.out)) != 1 {
		t.Fatalf("positive twin: a failed test email reset the counts (%d tried, mark %q, WARNs %d %d)", 2-len(g.fail), deliveredID(g.settings), len(sendFailedWarn(t, g.out)), len(sendFailedWarn(t, h.out)))
	}

	// The test email goes: both counts start again, read before any backup.
	g.fail = nil
	if err := SendTest(g.settings, nil, "", nil); err != nil {
		t.Fatal(err)
	}
	since := deliveredID(g.settings)
	rec, _ := state.ReadTBStreak(g.out)
	told, _ := readSyncAlert(SyncAlertPath(h.settings, h.key))
	if since == "" || since == before || rec.Tries(g.notice, since) != 0 || told.triesOf(s, since) != 0 {
		t.Fatalf("CAP-R3-A: the test email did not start the counts again: mark %q (was %q), download %d, sync %d", since, before, rec.Tries(g.notice, since), told.triesOf(s, since))
	}
	clk = t0.Add(2 * time.Hour)
	finishedRun(t, g.out)
	g.report(false, nil)
	finishedRun(t, h.out)
	h.tell(false)
	if still, _ := g.flags(); !still || h.count() != 1 {
		t.Errorf("CAP-R3-A: after Send test email went, the next due backups sent: download %v, sync %d", still, h.count())
	}
	if len(sendFailedWarn(t, g.out)) != 0 || len(sendFailedWarn(t, h.out)) != 0 {
		t.Errorf("the WARN is still there after an email got through: %d %d", len(sendFailedWarn(t, g.out)), len(sendFailedWarn(t, h.out)))
	}

	// Any delivered email does it: a backup alert for a third archive.
	k := newTBRig(t, alertsOnFailure, false)
	k.fail = failures(5)
	for i := 0; i < 5; i++ {
		clk = t0.Add(time.Duration(3*60+i) * time.Minute)
		k.report(false, nil)
	}
	FireForRun(k.settings, nil, filepath.Join(t.TempDir(), "third"), ResultFailed, nil, 0, nil)
	clk = t0.Add(4 * time.Hour)
	k.report(false, nil)
	if still, _ := k.flags(); !still {
		t.Error("CAP-R3-A: a delivered backup alert did not start the download email's count again")
	}
}

// covers: MA-1291, MA-1184
// Two accounts in one archive: account A's email failed 5 times in a row an hour ago, account B's is
// due for the first time. The next backup tries only B's (A waits for its day); when that fails, the
// run still adds the WARN, for A. When B's goes instead, the run adds no WARN (an email got through),
// and the backup after it tries A's, which goes. Positive twin first: A at 3 failures, one email for
// both fails, A at 4 and B at 1: no WARN.
func TestAWaitingEmailKeepsItsWarningBesideAnother(t *testing.T) {
	clk := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	atClock(t, &clk)
	rig := func(aTries int) (*streakRig, string, state.TBNotice, state.TBNotice) {
		g := newStreakRig(t, alertsOnFailure)
		out := filepath.Join(t.TempDir(), "archive")
		if err := os.MkdirAll(out, 0o700); err != nil {
			t.Fatal(err)
		}
		t0 := clk.Add(-80 * time.Hour)
		s := state.TBStreak{}.StartRun(t0).Behind("kA", "imap.a.example", 100, t0).Behind("kB", "imap.b.example", 100, t0)
		s = s.StartRun(clk.Add(-time.Hour)).Behind("kA", "imap.a.example", 500, clk.Add(-time.Hour)).Behind("kB", "imap.b.example", 500, clk.Add(-time.Hour)).Named()
		a, _ := s.Account("kA")
		b, _ := s.Account("kB")
		for range aTries {
			s, _ = s.MarkFailed("kA", a.Serial, false, "", clk.Add(-time.Hour))
		}
		if err := state.WriteTBStreak(out, s); err != nil {
			t.Fatal(err)
		}
		na := state.TBNotice{RecordID: s.RecordID, Store: "kA", Serial: a.Serial, Account: a.Account, OnDisk: 500, Listed: 1000, FirstBehind: a.FirstBehind, LastBehind: a.LastBehind, LastGrew: a.LastGrew}
		nb := state.TBNotice{RecordID: s.RecordID, Store: "kB", Serial: b.Serial, Account: b.Account, OnDisk: 500, Listed: 1000, FirstBehind: b.FirstBehind, LastBehind: b.LastBehind, LastGrew: b.LastGrew}
		return g, out, na, nb
	}
	tries := func(out string, n state.TBNotice) int {
		s, err := state.ReadTBStreak(out)
		if err != nil {
			t.Fatal(err)
		}
		return s.Tries(n, "")
	}

	tw, twOut, twA, twB := rig(state.AlertSendTries - 2)
	tw.fail = failures(1)
	finishedRun(t, twOut)
	ReportTBBehind(tw.settings, nil, twOut, []state.TBNotice{twA, twB}, false, nil)
	if len(sendFailedWarn(t, twOut)) != 0 || tries(twOut, twA) != state.AlertSendTries-1 || tries(twOut, twB) != 1 {
		t.Fatalf("positive twin: WARN %v, A %d, B %d", sendFailedWarn(t, twOut), tries(twOut, twA), tries(twOut, twB))
	}

	g, out, na, nb := rig(state.AlertSendTries)
	g.fail = failures(1)
	finishedRun(t, out)
	ReportTBBehind(g.settings, nil, out, []state.TBNotice{na, nb}, false, nil)
	if len(g.fail) != 0 || tries(out, na) != state.AlertSendTries || tries(out, nb) != 1 {
		t.Errorf("only B's email is tried: %d sends left, A %d, B %d", len(g.fail), tries(out, na), tries(out, nb))
	}
	if len(sendFailedWarn(t, out)) != 1 {
		t.Errorf("CAP-R3-A: A's email waits for its day, B's failed, and the run has no WARN")
	}

	h, hOut, ha, hb := rig(state.AlertSendTries)
	finishedRun(t, hOut)
	if !ReportTBBehind(h.settings, nil, hOut, []state.TBNotice{ha, hb}, false, nil) || len(h.sent) != 1 || h.sent[0].TB.Accounts != 1 {
		t.Fatalf("B's email alone: %d sent", len(h.sent))
	}
	if len(sendFailedWarn(t, hOut)) != 0 {
		t.Error("the run whose email got through has the WARN")
	}
	clk = clk.Add(time.Hour)
	finishedRun(t, hOut)
	if !ReportTBBehind(h.settings, nil, hOut, []state.TBNotice{ha}, false, nil) || len(h.sent) != 2 {
		t.Errorf("CAP-R3-A: after B's email got through, A's was not tried at the next backup (%d sent)", len(h.sent))
	}
}

// covers: MA-1291
// The delivery mark beside the settings file: owner-only, only a version and a random id (no
// address, no subject, no archive), a new id at every delivered email and none at a failed one; a
// mark planted as a link is not read (counts stand under "") and is replaced by a regular file at
// the next delivery, its target untouched; and uninstall removes it with the settings files (the two
// names kept in step by behaviour). Positive twin first: no email, no mark.
func TestTheDeliveryMark(t *testing.T) {
	g := newStreakRig(t, alertsOnFailure)
	path := DeliveredPath(g.settings)
	if _, err := os.Lstat(path); !os.IsNotExist(err) || deliveredID(g.settings) != "" {
		t.Fatalf("positive twin: a mark before any email: %v", err)
	}
	g.fail = failures(1)
	_ = SendTest(g.settings, nil, "", nil)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("a failed email wrote the mark: %v", err)
	}
	if err := SendTest(g.settings, nil, "", nil); err != nil {
		t.Fatal(err)
	}
	first := deliveredID(g.settings)
	data, _ := os.ReadFile(path)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || len(m) != 2 || len(first) != 32 || strings.Contains(string(data), "example.org") {
		t.Errorf("the mark holds %s (%v), want only a version and a 32-hex id", data, err)
	}
	if fi, err := os.Stat(path); err != nil || (fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows") {
		t.Errorf("the mark's mode: %v (%v)", fi, err)
	}
	_ = SendTest(g.settings, nil, "", nil)
	second := deliveredID(g.settings)
	if second == first || second == "" {
		t.Errorf("a second email kept the id %q", second)
	}
	// An email the server accepted, whose goodbye failed, counts as delivered (SPAM-01): a new id.
	restore := SetSenderForTest(func(alert.Config, string, alert.Event) error {
		return fmt.Errorf("the goodbye: %w", alert.ErrAfterAccepted)
	})
	err := SendTest(g.settings, nil, "", nil)
	restore()
	if third := deliveredID(g.settings); err != nil || third == second {
		t.Errorf("an accepted email whose goodbye failed: %v, id kept %v", err, third == second)
	}
	targets, err := uninstall.ConfigTargets(filepath.Dir(g.settings), nil)
	if err != nil || !slices.Contains(targets, path) {
		t.Errorf("uninstall does not remove the mark: %v (%v)", targets, err)
	}

	h := newStreakRig(t, alertsOnFailure)
	target := filepath.Join(t.TempDir(), "elsewhere")
	body := `{"version":1,"id":"planted"}`
	if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, DeliveredPath(h.settings)); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if deliveredID(h.settings) != "" {
		t.Error("a linked mark was read")
	}
	if err := SendTest(h.settings, nil, "", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	fi, _ := os.Lstat(DeliveredPath(h.settings))
	if string(got) != body || fi.Mode()&os.ModeSymlink != 0 || deliveredID(h.settings) == "" {
		t.Errorf("the linked mark: target now %q, still a link %v", got, fi.Mode()&os.ModeSymlink != 0)
	}
}
