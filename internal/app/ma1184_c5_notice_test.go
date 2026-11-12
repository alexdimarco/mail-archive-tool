package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// Slice C5's half in the run (docs/design-thunderbird-completeness.md §5, TC-20, M10): the fold
// finds which behind-streaks are due an email, and MarkTBNoticesSent sets a streak's flag once the
// email went. The R4-01 and R4-02 fixes carried to C5 from the C2 pass round 4.

func c5Behind(id string, x int64) tbcomplete.Result {
	return tbcomplete.Result{Account: "imap." + id + ".example", StoreID: id, Verdict: tbcomplete.Behind, OnDisk: x, Listed: 1000}
}

// covers: MA-1184
// The fold names the streaks due an email (TC-20): only an account this run found behind, never one
// it did not reach or found unknown; a due streak not yet marked is due again at the next behind run
// (a failed send is tried again); once MarkTBNoticesSent sets its flag it is due nothing more of that
// kind; a caught_up or limited run ends the streak with no notice. The notice names the record, the
// store and the streak's serial, and carries the store folder and counts. Positive twin first: a
// streak behind for 3 days is due the still-downloading email.
func TestTheFoldNamesTheStreaksDueAnEmail(t *testing.T) {
	out := tmpDir(t)
	opts := Options{Out: out}
	t0 := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	if _, n := foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 100), c5Behind("k2", 100)}, false, true, t0, nil); n != nil {
		t.Fatalf("a first behind run is due %+v", n)
	}
	// k2 is not reached by this run: never named.
	_, n := foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 500)}, false, true, t0.Add(3*day), nil)
	rec := mustStreak(t, out)
	if len(n) != 1 || n[0].Store != "k1" || n[0].Stopped || n[0].Serial != 1 || n[0].RecordID != rec.RecordID || rec.RecordID == "" ||
		n[0].Account != "imap.k1.example" || n[0].OnDisk != 500 || n[0].Listed != 1000 || !n[0].FirstBehind.Equal(t0) || !n[0].LastBehind.Equal(t0.Add(3*day)) {
		t.Fatalf("positive twin: 3 days behind: %+v (record %q)", n, rec.RecordID)
	}
	// k2 reached only as unknown: never named; k1 not yet marked: due again (a failed send retries).
	unknown := tbcomplete.Result{Account: "imap.k2.example", StoreID: "k2", Verdict: tbcomplete.Unknown}
	_, n = foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 600), unknown}, false, true, t0.Add(3*day+time.Hour), nil)
	if len(n) != 1 || n[0].Store != "k1" {
		t.Fatalf("an unmarked due streak, and an unknown store: %+v", n)
	}
	if err := MarkTBNoticesSent(out, n); err != nil {
		t.Fatal(err)
	}
	if a, _ := mustStreak(t, out).Account("k1"); !a.StillSent || a.StoppedSent {
		t.Fatalf("MarkTBNoticesSent: %+v", a)
	}
	if _, n = foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 700)}, false, true, t0.Add(4*day), nil); n != nil {
		t.Errorf("a marked streak is due again: %+v", n)
	}
	// X stops at 700 for 3 days and 3 backups (FALSE-01's rule): the stopped email, once.
	for _, d := range []time.Duration{5, 6} {
		if _, n = foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 700)}, false, true, t0.Add(d*day), nil); n != nil {
			t.Fatalf("X unchanged for %d days is due %+v", d-4, n)
		}
	}
	_, n = foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 700)}, false, true, t0.Add(7*day+time.Hour), nil)
	if len(n) != 1 || !n[0].Stopped || !n[0].LastGrew.Equal(t0.Add(4*day)) {
		t.Fatalf("3 days without growth: %+v", n)
	}
	if err := MarkTBNoticesSent(out, n); err != nil {
		t.Fatal(err)
	}
	if _, n = foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 700)}, false, true, t0.Add(9*day), nil); n != nil {
		t.Errorf("the stopped email is due twice: %+v", n)
	}
	// A caught_up run ends it with no notice; so does a limited run of a new streak.
	caught := c5Behind("k1", 1000)
	caught.Verdict = tbcomplete.CaughtUp
	if _, n = foldTBStreak(opts, []tbcomplete.Result{caught}, false, true, t0.Add(10*day), nil); n != nil {
		t.Errorf("a caught_up run is due %+v", n)
	}
	if _, had := mustStreak(t, out).Account("k1"); had {
		t.Error("a caught_up run kept the streak")
	}
	foldTBStreak(opts, []tbcomplete.Result{c5Behind("k1", 10)}, false, true, t0.Add(11*day), nil)
	limited := c5Behind("k1", 10)
	limited.Verdict = tbcomplete.Limited
	if _, n = foldTBStreak(opts, []tbcomplete.Result{limited}, false, true, t0.Add(14*day+time.Hour), nil); n != nil {
		t.Errorf("a limited run is due %+v", n)
	}
	// A streak due and not yet marked is never named by a run that did not reach its store, nor by
	// one that found it unknown (the C2 pass's note: C5 never emails about a store the run did not
	// reach); the next behind run names it.
	out2 := tmpDir(t)
	foldTBStreak(Options{Out: out2}, []tbcomplete.Result{c5Behind("d", 1)}, false, true, t0, nil)
	if _, n = foldTBStreak(Options{Out: out2}, []tbcomplete.Result{c5Behind("d", 5)}, false, true, t0.Add(3*day), nil); len(n) != 1 {
		t.Fatalf("the due streak: %+v", n)
	}
	unknownD := tbcomplete.Result{Account: "imap.d.example", StoreID: "d", Verdict: tbcomplete.Unknown}
	if _, n = foldTBStreak(Options{Out: out2}, []tbcomplete.Result{unknownD, c5Behind("other", 1)}, false, true, t0.Add(3*day+time.Hour), nil); n != nil {
		t.Errorf("a due streak found unknown was named: %+v", n)
	}
	if _, n = foldTBStreak(Options{Out: out2}, []tbcomplete.Result{c5Behind("other", 2)}, false, true, t0.Add(3*day+2*time.Hour), nil); n != nil {
		t.Errorf("a due streak not reached was named: %+v", n)
	}
	if _, n = foldTBStreak(Options{Out: out2}, []tbcomplete.Result{c5Behind("d", 6)}, false, true, t0.Add(3*day+3*time.Hour), nil); len(n) != 1 || n[0].Store != "d" {
		t.Errorf("the next behind run did not name it: %+v", n)
	}

	// A run that did not end ok, and an encrypted archive, are due nothing.
	foldTBStreak(opts, []tbcomplete.Result{c5Behind("k3", 1)}, false, true, t0.Add(20*day), nil)
	if _, n = foldTBStreak(opts, []tbcomplete.Result{c5Behind("k3", 2)}, false, false, t0.Add(24*day), nil); n != nil {
		t.Errorf("a failed run is due %+v", n)
	}
	if _, n = foldTBStreak(opts, []tbcomplete.Result{c5Behind("k3", 2)}, true, true, t0.Add(24*day), nil); n != nil {
		t.Errorf("an encrypted run is due %+v", n)
	}
}

// covers: MA-1184
// A notice is given only when the run wrote the streak record, so its flag can be set: a refused
// write gives none (else every run would send). MarkTBNoticesSent marks only the streak it names: a
// record started again (another RecordID) or a streak that ended is left alone; it never writes
// beside an encrypted archive, nor while another run holds the archive's lock. Positive twin first:
// a written record gives the notice and MarkTBNoticesSent marks it.
func TestANoticeNeedsTheWrittenRecord(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	due := func(out string) []state.TBNotice {
		foldTBStreak(Options{Out: out}, []tbcomplete.Result{c5Behind("k", 1)}, false, true, t0, nil)
		_, n := foldTBStreak(Options{Out: out}, []tbcomplete.Result{c5Behind("k", 9)}, false, true, t0.Add(80*time.Hour), nil)
		return n
	}
	out := tmpDir(t)
	n := due(out)
	if len(n) != 1 {
		t.Fatalf("positive twin: %+v", n)
	}

	// Held lock: nothing marked, an error.
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkTBNoticesSent(out, n); !errors.Is(err, lockfile.ErrHeld) {
		t.Errorf("a held lock: %v", err)
	}
	held.Release()
	if a, _ := mustStreak(t, out).Account("k"); a.StillSent {
		t.Fatal("marked under another run's lock")
	}
	// Another record's notice, and a streak of another serial: left alone.
	other := append([]state.TBNotice(nil), n...)
	other[0].RecordID = "AnotherRecord0123"
	stale := append([]state.TBNotice(nil), n...)
	stale[0].Serial++
	for _, bad := range [][]state.TBNotice{other, stale} {
		if err := MarkTBNoticesSent(out, bad); err != nil {
			t.Fatal(err)
		}
		if a, _ := mustStreak(t, out).Account("k"); a.StillSent {
			t.Fatalf("marked a streak the notice does not name: %+v", bad)
		}
	}
	if err := MarkTBNoticesSent(out, n); err != nil {
		t.Fatal(err)
	}
	if a, _ := mustStreak(t, out).Account("k"); !a.StillSent {
		t.Fatal("positive twin: not marked")
	}

	// A refused write: no notice.
	w := tmpDir(t)
	foldTBStreak(Options{Out: w}, []tbcomplete.Result{c5Behind("k", 1)}, false, true, t0, nil)
	prev := writeTBStreak
	writeTBStreak = func(string, state.TBStreak) error { return errors.New("the drive is read-only") }
	_, n2 := foldTBStreak(Options{Out: w}, []tbcomplete.Result{c5Behind("k", 9)}, false, true, t0.Add(80*time.Hour), nil)
	writeTBStreak = prev
	if n2 != nil {
		t.Errorf("a refused write gave %+v", n2)
	}

	// An encrypted archive: never written, even with a notice in hand.
	enc, _ := encryptedOut(t)
	if err := MarkTBNoticesSent(enc, n); err == nil {
		t.Error("MarkTBNoticesSent wrote beside an encrypted archive")
	}
	if _, err := os.Lstat(filepath.Join(enc, state.TBStreakName)); !os.IsNotExist(err) {
		t.Errorf("an encrypted archive holds %s (%v)", state.TBStreakName, err)
	}
}

// covers: MA-1281
// R4-01 (the C2 pass round 4, the lead's call): a record whose run counter is at its cap (or
// otherwise impossible) is damaged: the streak starts fresh from this run, the run logs a line and
// records a tbstreak_unreadable WARN that status shows, and the record is rewritten plausible. So a
// planted full record of entries that look "found behind by this run" can no longer turn a real
// behind store away. Positive twin first: the same full record with a plausible counter makes room
// for the real store (by eviction) with no tbstreak_unreadable WARN.
func TestR4_01_AnImplausibleCounterStartsFresh(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	plant := func(out string, run int) {
		var b strings.Builder
		fmt.Fprintf(&b, `{"version":1,"serial":%d,"run":%d,"last_run_at":%q,"record_id":"PlantedRecordId01","accounts":[`, state.TBStreakAccountsMax, run, now.Add(-time.Hour).Format(time.RFC3339))
		for i := 0; i < state.TBStreakAccountsMax; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"store":"p%03d","account":"imap.p%03d.example","serial":%d,"runs":1,"seen_run":%d,"behind_run":%d,"seen_at":%q}`, i, i, i+1, run, run, now.Add(-time.Hour).Format(time.RFC3339))
		}
		b.WriteString("]}")
		tbWrite(t, filepath.Join(out, state.TBStreakName), b.String())
	}
	codes := func(ws []state.RunWarning) string {
		var c []string
		for _, w := range ws {
			c = append(c, w.Code)
		}
		return strings.Join(c, ",")
	}

	twin := tmpDir(t)
	plant(twin, 1000)
	ws, _ := foldTBStreak(Options{Out: twin}, []tbcomplete.Result{c5Behind("real", 1)}, false, true, now, nil)
	if _, ok := mustStreak(t, twin).Account("real"); !ok || strings.Contains(codes(ws), "tbstreak_unreadable") {
		t.Fatalf("positive twin: a plausible counter: %s %+v", codes(ws), mustStreak(t, twin).Accounts[:1])
	}

	out := tmpDir(t)
	plant(out, 1<<40)
	var buf bytes.Buffer
	ws, _ = foldTBStreak(Options{Out: out}, []tbcomplete.Result{c5Behind("real", 1)}, false, true, now, log.New(&buf, "", 0))
	s := mustStreak(t, out)
	if _, ok := s.Account("real"); !ok || len(s.Accounts) != 1 || s.Run != 1 || s.Damaged() || s.RecordID == "PlantedRecordId01" {
		t.Errorf("the damaged record was not started fresh: run %d, %d entries, id %q", s.Run, len(s.Accounts), s.RecordID)
	}
	if codes(ws) != "tbstreak_unreadable" || !strings.Contains(ws[0].Message, state.TBStreakName) || !strings.Contains(buf.String(), "could not have been written by a backup") {
		t.Errorf("the WARN and log: %s %+v, log %q", codes(ws), ws, buf.String())
	}
	rep := health.Assess(health.Input{Out: out, LastRun: state.LastRun{Status: state.RunOK, Warnings: ws}, LastRunState: state.LastRunPresent}, now)
	if !strings.Contains(strings.Join(rep.Codes, " "), "tbstreak_unreadable") {
		t.Errorf("status does not show it: %+v", rep.Codes)
	}
}

// covers: MA-1281
// R4-02 (the C2 pass round 4, the lead's call): every eviction raises tbstreak_full, naming the
// store whose streak was dropped, so status shows it; not only a new behind store that found no
// room. Positive twin first: a record with room starts the new streak with no WARN.
func TestR4_02_EveryEvictionWarns(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	out := tmpDir(t)
	if ws, _ := foldTBStreak(Options{Out: out}, []tbcomplete.Result{c5Behind("weekly", 1)}, false, true, now, nil); ws != nil {
		t.Fatalf("positive twin: %+v", ws)
	}
	var rs []tbcomplete.Result
	for i := 1; i < state.TBStreakAccountsMax; i++ {
		rs = append(rs, c5Behind(fmt.Sprintf("h%03d", i), 1))
	}
	if ws, _ := foldTBStreak(Options{Out: out}, rs, false, true, now.Add(time.Hour), nil); ws != nil {
		t.Fatalf("256 entries: %+v", ws)
	}
	// The hourly job's next run finds a new behind store: the weekly store's streak goes.
	ws, _ := foldTBStreak(Options{Out: out}, append(rs, c5Behind("hnew", 1)), false, true, now.Add(2*time.Hour), nil)
	if _, ok := mustStreak(t, out).Account("weekly"); ok {
		t.Fatal("no eviction happened")
	}
	if len(ws) != 1 || ws[0].Code != "tbstreak_full" || !strings.Contains(ws[0].Message, "imap.weekly.example") || !strings.Contains(ws[0].Message, state.TBStreakName) {
		t.Fatalf("an eviction left no tbstreak_full naming the dropped store: %+v", ws)
	}
	rep := health.Assess(health.Input{Out: out, LastRun: state.LastRun{Status: state.RunOK, Warnings: ws}, LastRunState: state.LastRunPresent}, now)
	if !strings.Contains(strings.Join(rep.Codes, " "), "tbstreak_full") {
		t.Errorf("status does not show it: %+v", rep.Codes)
	}
}

// covers: MA-1184, MA-1183
// Through a real backup run of a real Thunderbird profile: a plaintext archive whose streak has
// lasted 3 days carries the still-downloading notice in the run's result, and `status -json` says
// since when the account has been behind (`behind_since`, the streak's first behind run). An
// encrypted archive's run carries none. Positive twin first: the first behind run carries none and
// already says behind_since.
func TestARealBehindRunCarriesItsNotice(t *testing.T) {
	folders := []tbFolder{{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)}}
	_, store := imapProfile(t, "imap.qzc5.example", nil, folders...)
	out := tmpDir(t)
	res := runTB(t, Options{Inputs: []string{store}, Out: out})
	if r := onlyResult(t, res); r.Verdict != tbcomplete.Behind || res.TBNotices != nil {
		t.Fatalf("positive twin: %+v, notices %+v", r, res.TBNotices)
	}
	s := mustStreak(t, out)
	first := s.Accounts[0].FirstBehind
	rep := health.Assess(health.Gather(out, ""), time.Now())
	j := health.JSON(health.Gather(out, ""), rep)
	if len(j.ThunderbirdCompleteness) != 1 || !j.ThunderbirdCompleteness[0].BehindSince.Equal(first) {
		t.Fatalf("positive twin: -json behind_since: %+v (want %v)", j.ThunderbirdCompleteness, first)
	}
	// The streak began 4 days ago, with 1 message then: this run's 3 are growth, so it is the
	// still-downloading email.
	back := time.Now().UTC().Add(-4 * 24 * time.Hour)
	s.Accounts[0].FirstBehind, s.Accounts[0].LastBehind, s.Accounts[0].LastGrew, s.Accounts[0].GrewX, s.Accounts[0].FirstX, s.Accounts[0].LastX = back, back, back, 1, 1, 1
	s.LastRunAt, s.Accounts[0].SeenAt = back, back
	if err := state.WriteTBStreak(out, s); err != nil {
		t.Fatal(err)
	}
	res = runTB(t, Options{Inputs: []string{store}, Out: out})
	if n := res.TBNotices; len(n) != 1 || n[0].Stopped || n[0].Account != "imap.qzc5.example" || n[0].OnDisk != 3 || n[0].Listed != 40 || n[0].RecordID != s.RecordID {
		t.Fatalf("a real run 4 days into its streak: %+v", n)
	}
	j = health.JSON(health.Gather(out, ""), health.Assess(health.Gather(out, ""), time.Now()))
	if len(j.ThunderbirdCompleteness) != 1 || !j.ThunderbirdCompleteness[0].BehindSince.Equal(back) {
		t.Errorf("-json behind_since: %+v (want %v)", j.ThunderbirdCompleteness, back)
	}

	enc, key := encryptedOut(t)
	er, err := Run(context.Background(), Options{Inputs: []string{store}, Out: enc, Key: key, Mode: export.Incremental}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if onlyResult(t, er).Verdict != tbcomplete.Behind || er.TBNotices != nil {
		t.Errorf("an encrypted run's notices: %+v", er.TBNotices)
	}
}
