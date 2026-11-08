package app

// The C2 adversarial pass round 3's fixes, end to end through a real backup run
// (docs/review-thunderbird-completeness-c2-adversarial-r3.md: R3-01, R3-04). Positive twins
// first. Real temp profiles and archives only; never the owner's profile.

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// r3Long is the source package's R3-01 line: a "From " body line whose bytes past go-mbox's
// 4,096-byte buffer begin "From " again.
func r3Long() string {
	head := "From x"
	return head + strings.Repeat("x", 4096-len(head)) + "From y\n"
}

// covers: MA-1279
// R3-01 end to end: INBOX lists 40 and holds 3 Thunderbird messages, the second with an unescaped
// "From " body line longer than go-mbox's buffer, followed by a block carrying an expunged
// X-Mozilla-Status. The reader then hands on one piece more than there are "From " lines, so no
// piece is counted: the account is unknown, split_differently, never behind (no behind streak
// starts, so no "still downloading" email can come of it), and the record keeps
// misaligned_mailboxes 1. Every piece is still archived. Positive twin: the same line escaped, as
// Thunderbird writes it, reads behind, 3 of 40.
func TestR3_01_ALongFromLineNeverMakesAFalseBehind(t *testing.T) {
	sep := "From - Mon Mar  3 09:00:00 2025\n"
	file := func(escape string) string {
		return sep + tbMsg("in-1", "") + "\n" + sep + tbMsg("in-2", "") + "\n" + escape + r3Long() +
			"X-Mozilla-Status: 0009\nMessage-ID: <crafted@x>\n\nhidden\n\n" + sep + tbMsg("in-3", "")
	}
	run := func(escape string) (tbcomplete.Result, string) {
		_, store := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)})
		tbWrite(t, filepath.Join(store, "INBOX"), file(escape))
		out := tmpDir(t)
		return onlyResult(t, runTB(t, Options{Inputs: []string{store}, Out: out})), out
	}
	if r, _ := run(">"); r.Verdict != tbcomplete.Behind || r.OnDisk != 3 || r.MisalignedMailboxes != 0 {
		t.Fatalf("positive twin: %v %v X %d misaligned %d", r.Verdict, r.Reason, r.OnDisk, r.MisalignedMailboxes)
	}
	r, out := run("")
	s, _ := state.ReadTBStreak(out)
	if r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonSplitDifferently || r.MisalignedMailboxes != 1 || r.MaySayComplete() || len(s.Accounts) != 0 {
		t.Errorf("a long unescaped From line, 40 listed: %v %v X %d misaligned %d complete %v; streak %+v", r.Verdict, r.Reason, r.OnDisk, r.MisalignedMailboxes, r.MaySayComplete(), s.Accounts)
	}
	lr, _, _ := state.ReadLastRun(out)
	if len(lr.Thunderbird) != 1 || lr.Thunderbird[0].MisalignedMailboxes != 1 || lr.Thunderbird[0].Verdict != "unknown" || lr.Exported < 3 {
		t.Errorf("the record: %+v, exported %d", lr.Thunderbird, lr.Exported)
	}
}

// covers: MA-1281
// R3-04 end to end: a full streak record never drops a streak the run found behind. Positive twin
// (in the fold): 256 behind stores in one run all get a streak, with no WARN and nothing logged.
// Then a real run of one profile with 257 IMAP accounts, every one behind: 256 get a streak, the
// 257th gets none, the log names it, and the last-run record keeps a tbstreak_full WARN naming it
// that status shows. A second run of the same 257 keeps every one of the 256 (two runs each) and
// says the same.
func TestAFullStreakRecordNeverDropsABehindStreak(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	var rs []tbcomplete.Result
	for i := 0; i < state.TBStreakAccountsMax; i++ {
		rs = append(rs, tbcomplete.Result{Account: fmt.Sprintf("imap.%03d.example", i), StoreID: fmt.Sprintf("s%03d", i), Verdict: tbcomplete.Behind, OnDisk: 1, Listed: 30})
	}
	var buf bytes.Buffer
	twin := tmpDir(t)
	if w := updateTBStreak(Options{Out: twin}, rs, false, true, now, log.New(&buf, "", 0)); w != nil || buf.Len() != 0 || len(mustStreak(t, twin).Accounts) != state.TBStreakAccountsMax {
		t.Fatalf("positive twin: %+v %q %d", w, buf.String(), len(mustStreak(t, twin).Accounts))
	}
	// In the fold: one more behind store the next hour is logged by name and given the WARN.
	more := append(rs, tbcomplete.Result{Account: "imap.waiting.example", StoreID: "sw", Verdict: tbcomplete.Behind, OnDisk: 1, Listed: 30})
	w := updateTBStreak(Options{Out: twin}, more, false, true, now.Add(time.Hour), log.New(&buf, "", 0))
	if len(w) != 1 || w[0].Code != "tbstreak_full" || !strings.Contains(buf.String(), "could not start counting imap.waiting.example") || !strings.Contains(buf.String(), "is full: all 256 accounts") {
		t.Errorf("the fold, 257 behind: %+v, log %q", w, buf.String())
	}

	var accts []tbAcct
	var caches []map[string]any
	var inputs []string
	p := newTBProfile(t)
	for i := 0; i <= state.TBStreakAccountsMax; i++ {
		host := fmt.Sprintf("imap.%03d.example", i)
		accts = append(accts, tbAcct{n: i + 1, host: host, typ: "imap", dirRel: "ImapMail/" + host})
		store := filepath.Join(p, "ImapMail", host)
		caches = append(caches, tbStoreAt(t, store, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 30, msgs: tbMsgs(host, 1)}))
		inputs = append(inputs, store)
	}
	tbWrite(t, filepath.Join(p, "prefs.js"), tbPrefsJS(accts...))
	writeTBCache(t, p, caches...)
	out := tmpDir(t)
	last := fmt.Sprintf("imap.%03d.example", state.TBStreakAccountsMax)
	for round := 1; round <= 2; round++ {
		res := runTB(t, Options{Inputs: inputs, Out: out})
		if len(res.Thunderbird) != state.TBStreakAccountsMax+1 {
			t.Fatalf("round %d: the run gave %d results", round, len(res.Thunderbird))
		}
		for _, r := range res.Thunderbird {
			if r.Verdict != tbcomplete.Behind {
				t.Fatalf("round %d: %s is %v, want every account behind", round, r.Account, r.Verdict)
			}
		}
		s := mustStreak(t, out)
		if len(s.Accounts) != state.TBStreakAccountsMax {
			t.Errorf("round %d: %d streaks, want %d", round, len(s.Accounts), state.TBStreakAccountsMax)
		}
		for _, a := range s.Accounts {
			if a.Runs != round || a.Account == last {
				t.Errorf("round %d: %s has %d runs (want %d; the 257th none)", round, a.Account, a.Runs, round)
			}
		}
		lr, _, err := state.ReadLastRun(out)
		var full *state.RunWarning
		for i := range lr.Warnings {
			if lr.Warnings[i].Code == "tbstreak_full" {
				full = &lr.Warnings[i]
			}
		}
		if err != nil || full == nil || !strings.Contains(full.Message, last) || !strings.Contains(full.Message, state.TBStreakName) || !strings.Contains(full.Message, "1 more") {
			t.Fatalf("round %d: the last-run record has no tbstreak_full WARN naming %s: %v %+v", round, last, err, lr.Warnings)
		}
		rep := health.Assess(health.Input{Out: out, LastRun: lr, LastRunState: state.LastRunPresent}, time.Now())
		if !strings.Contains(strings.Join(rep.Codes, " "), "tbstreak_full") || rep.Posture == "GREEN" {
			t.Errorf("round %d: status does not show tbstreak_full: %+v", round, rep.Codes)
		}
	}
	// Both of the streak's WARNs in one run reach the record: a record other users can read
	// (started again from this run) that is then too full for the 257th.
	if runtime.GOOS == "windows" {
		return // no group or other permission bits for the read to refuse
	}
	if err := os.Chmod(filepath.Join(out, state.TBStreakName), 0o644); err != nil {
		t.Fatal(err)
	}
	runTB(t, Options{Inputs: inputs, Out: out})
	lr, _, _ := state.ReadLastRun(out)
	codes := map[string]bool{}
	for _, w := range lr.Warnings {
		codes[w.Code] = true
	}
	if !codes["tbstreak_unreadable"] || !codes["tbstreak_full"] {
		t.Errorf("a loose and full record in one run: the record's WARNs %+v", lr.Warnings)
	}
}

// covers: MA-1281
// tbstreak_full's sentence names the record, how many wait and at most three of them by name.
// Positive twin: one waiting account is named.
func TestTheFullRecordSentenceNamesWhoWaits(t *testing.T) {
	if m := tbStreakFullMessage("/a/.mailarchive-tbstreak.json", []string{"imap.one.example"}); !strings.Contains(m, "/a/.mailarchive-tbstreak.json") || !strings.Contains(m, "1 more (imap.one.example)") || !strings.Contains(m, "256") {
		t.Fatalf("positive twin: %q", m)
	}
	m := tbStreakFullMessage("/a/r", []string{"a1", "a2", "a3", "a4", "a5"})
	if !strings.Contains(m, "5 more (a1, a2, a3 and 2 more)") || strings.Contains(m, "a4") {
		t.Errorf("five waiting: %q", m)
	}
}
