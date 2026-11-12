package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/assure/leak"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// gmailFolders is a Gmail store's folders: All Mail listing allMail, holding a-1…a-6; INBOX and
// Sent Mail, whose bodies Thunderbird keeps once, in whichever folder reached them first (§1's
// single-copy rule): INBOX holds copies of a-1…a-3, Sent Mail lists 4 with 2 on disk; and Spam,
// not kept (Thunderbird's default), with 3 leftover messages in its file.
func gmailFolders(allMail int) []tbFolder {
	var all []string
	for i := 1; i <= 6; i++ {
		all = append(all, tbMsg("a-"+string(rune('0'+i)), ""))
	}
	return []tbFolder{
		{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 3, msgs: all[:3]},
		{path: "[Gmail]", flags: tbcomplete.FlagImapNoselect, listed: 0},
		{path: "[Gmail]/All Mail", flags: tbcomplete.FlagAllMail | tbcomplete.FlagArchive, listed: allMail, msgs: all},
		{path: "[Gmail]/Sent Mail", flags: tbcomplete.FlagSentMail, listed: 4, msgs: all[4:]},
		{path: "[Gmail]/Spam", flags: tbcomplete.FlagJunk, notKept: true, listed: 3, msgs: tbMsgs("junk", 3)},
	}
}

// covers: MA-1175, MA-1180
// Gmail's X is the walk's distinct count by Message-ID (§3, §2.4 adjustment 2; the C1b row's
// "Message-ID set still owed by C2"): All Mail full with INBOX holding copies and Sent Mail 2 of 4
// on disk is caught up, and no folder is called behind. A mailbox the walk read that has no
// cache key (L3) is archived but never adds to X, and is counted in the record; nor does the
// leftover file of a folder not kept (Spam), whose mail is outside Y (the builder's reading).
// With All Mail listing 10, X stays 6 (adding either would reach 9 or 11, within the tolerance
// of 1): behind.
func TestGmailIsCountedByDistinctMessageID(t *testing.T) {
	gm := map[string]string{"is_gmail": "true"}
	_, store := imapProfile(t, "imap.gmail.com", gm, gmailFolders(6)...)
	leftover := filepath.Join(store, "Leftover")
	tbWrite(t, leftover, "From - Mon Mar  3 09:00:00 2025\n"+strings.Join(tbMsgs("left", 5), "\nFrom - Mon Mar  3 09:00:00 2025\n")+"\n")

	out := tmpDir(t)
	res := runTB(t, Options{Inputs: []string{store}, Out: out})
	r := onlyResult(t, res)
	if r.Verdict != tbcomplete.CaughtUp || !r.Gmail || r.OnDisk != 6 || r.Listed != 6 || r.AllMail != tbcomplete.AllMailSetsY {
		t.Fatalf("positive twin: a Gmail store with All Mail full: %+v", r)
	}
	if r.UnkeyedMailboxes != 1 || r.UnkeyedMessages != 5 {
		t.Errorf("the mailbox with no key: %d mailboxes, %d messages; want 1, 5", r.UnkeyedMailboxes, r.UnkeyedMessages)
	}
	if res.Stats.Exported != 3+6+2+3+5 {
		t.Errorf("archived %d, want every message of every mailbox file, per folder as today (3 + 6 + 2 + 3 Spam + 5 leftover = 19)", res.Stats.Exported)
	}
	lr, _, _ := state.ReadLastRun(out)
	assure.Reached(t, len(lr.Thunderbird), "accounts recorded")
	if a := lr.Thunderbird[0]; !a.Gmail || a.AllMail != "sets_y" || a.UnkeyedMailboxes != 1 || a.UnkeyedMessages != 5 || len(a.NotKept) != 0 || len(a.DefaultNotKept) != 1 {
		t.Errorf("the Gmail record: %+v", a)
	}

	_, behind := imapProfile(t, "imap.gmail.com", gm, gmailFolders(10)...)
	tbWrite(t, filepath.Join(behind, "Leftover"), "From - Mon Mar  3 09:00:00 2025\n"+strings.Join(tbMsgs("left", 5), "\nFrom - Mon Mar  3 09:00:00 2025\n")+"\n")
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{behind}})); r.Verdict != tbcomplete.Behind || r.OnDisk != 6 || r.Listed != 10 {
		t.Errorf("All Mail listing 10 with 6 distinct on disk: %v X %d Y %d, want behind 6 of 10", r.Verdict, r.OnDisk, r.Listed)
	}
}

// covers: MA-1276
// The C1a pass round 2's R2-02, closed by C2: a store with no key of its own in tail mode never
// takes the keys of another account of its profile whose folder is also …/ImapMail/<the store's
// name>. The run passes the profile's other stores (each live account's own folder) to the
// reader. Positive twin first: the same store with its own keys is checked.
func TestTailModeNeverTakesAnotherStoresKeys(t *testing.T) {
	elsewhere := filepath.Join(tmpDir(t), "elsewhere", "ImapMail", "imap.c2.example")
	p := newTBProfile(t,
		tbAcct{n: 1, host: "imap.c2.example", typ: "imap", dirRel: "ImapMail/imap.c2.example"},
		tbAcct{n: 2, host: "imap.other.example", typ: "imap", dir: elsewhere})
	store := filepath.Join(p, "ImapMail", "imap.c2.example")
	own := tbStoreAt(t, store, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 1, msgs: tbMsgs("own", 1)})
	other := tbStoreAt(t, elsewhere, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 1, msgs: tbMsgs("other", 1)})
	writeTBCache(t, p, own, other)
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{store}})); r.Verdict != tbcomplete.CaughtUp {
		t.Fatalf("positive twin: the store with its own keys: %v %v", r.Verdict, r.Reason)
	}
	// Thunderbird has not saved the store's keys yet: only the other account's are there.
	writeTBCache(t, p, other)
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{store}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonCacheUnreadable {
		t.Errorf("a store with no keys took another account's: %v %v (X %d, Y %d), want unknown cache_unreadable", r.Verdict, r.Reason, r.OnDisk, r.Listed)
	}
}

// covers: MA-1180, MA-1184
// An encrypted archive keeps nothing about completeness beside it (owner ruling O1, R44): after a
// behind run of a keyed archive the verdict is in the run's result, in memory, for the screen;
// the last-run file and every history entry keep to the A18 allow-list, the tree grammar holds,
// a scan of the whole folder finds no account name, verdict or "thunderbird", and no behind-streak
// record is written, while a stray one is removed (as the presync one is). Positive twin first:
// the same behind store backed up into a plaintext archive is recorded and starts a streak.
func TestAnEncryptedArchiveKeepsNoCompleteness(t *testing.T) {
	folders := []tbFolder{{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)}}
	_, store := imapProfile(t, "imap.qzc2account.example", nil, folders...)

	plain := tmpDir(t)
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{store}, Out: plain})); r.Verdict != tbcomplete.Behind {
		t.Fatalf("positive twin: %+v", r)
	}
	if lr, _, _ := state.ReadLastRun(plain); len(lr.Thunderbird) != 1 {
		t.Fatalf("positive twin: the plaintext record: %+v", lr.Thunderbird)
	}
	if s, err := state.ReadTBStreak(plain); err != nil || len(s.Accounts) != 1 {
		t.Fatalf("positive twin: the plaintext streak: %v %+v", err, s)
	}

	out, key := encryptedOut(t)
	tbWrite(t, filepath.Join(out, state.TBStreakName), `{"version":1,"serial":3,"accounts":[{"account":"imap.qzc2account.example","runs":2}]}`)
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Mode: export.Incremental}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := onlyResult(t, res); r.Verdict != tbcomplete.Behind || r.OnDisk != 3 || r.Listed != 40 {
		t.Errorf("the verdict in memory: %+v", r)
	}
	allowRecord := map[string]bool{"version": true, "status": true, "started": true, "finished": true, "error_kind": true, "program": true, "encrypted": true}
	allowEntry := map[string]bool{"when": true, "status": true, "errorKind": true, "encrypted": true}
	for _, k := range jsonKeys(rawRecord(t, out)) {
		if !allowRecord[k] {
			t.Errorf("the encrypted last-run carries %q", k)
		}
	}
	for i, e := range rawHistory(t, out) {
		for _, k := range jsonKeys(e) {
			if !allowEntry[k] {
				t.Errorf("history entry %d carries %q", i, k)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(out, state.TBStreakName)); !os.IsNotExist(err) {
		t.Errorf("an encrypted archive holds %s (%v)", state.TBStreakName, err)
	}
	requireTree(t, "after a behind run", out, leak.AtRest)
	for _, n := range []string{"qzc2account", "thunderbird", "Thunderbird", "behind", "caught_up", "tbstreak"} {
		hits, err := leak.ScanTree(out, leak.Markers{leak.SourcePath: n})
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range hits {
			t.Errorf("the encrypted archive names %q: %s", n, hit)
		}
	}
}

// covers: MA-1184
// The behind streak's own record (§5's foldcheck M10 fold): only a behind run starts or extends an
// account's streak; the history keeps only 50 runs, so an hourly schedule behind for 4 days has
// long wrapped it, yet the record still holds the streak from its first run, and its
// still-downloading threshold (3 days or more, at least 2 runs, measured between runs) is met;
// X that does not grow for 3 days meets the stopped threshold, one that grows does not. A
// caught_up run ends the streak; a limited run ends it; an unknown run neither extends nor ends
// it. The serial rises only when a streak starts; the record is named once.
func TestTheBehindStreakOutlivesTheHistory(t *testing.T) {
	out := tmpDir(t)
	opts := Options{Out: out}
	t0 := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	run := func(at time.Time, v tbcomplete.Verdict, x int64) {
		updateTBStreak(opts, []tbcomplete.Result{{Account: "imap.c2.example", StoreID: "k1", Verdict: v, OnDisk: x, Listed: 1000}}, false, true, at, nil)
		_ = state.AppendRunHistory(out, state.RunHistoryEntry{When: at, Status: state.RunOK})
	}
	streak := func() state.TBStreakAccount {
		t.Helper()
		s, err := state.ReadTBStreak(out)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := s.Account("k1")
		return a
	}
	run(t0, tbcomplete.Behind, 100)
	if a := streak(); a.Runs != 1 || !a.FirstBehind.Equal(t0) || a.FirstX != 100 || a.Serial != 1 || a.StillDownloading() {
		t.Fatalf("positive twin: the first behind run: %+v", a)
	}
	for h := 1; h <= 96; h++ {
		run(t0.Add(time.Duration(h)*time.Hour), tbcomplete.Behind, 100+int64(h))
	}
	h, _ := state.ReadRunHistory(out)
	if len(h) != state.RunHistoryMax || h[0].When.Equal(t0) {
		t.Fatalf("the history did not wrap: %d entries, first %v", len(h), h[0].When)
	}
	a := streak()
	if a.Runs != 97 || !a.FirstBehind.Equal(t0) || !a.StillDownloading() || a.Stopped() || a.GrewX != 196 {
		t.Errorf("after 4 days hourly: %+v; want 97 runs from %v, still downloading, not stopped", a, t0)
	}
	s0, _ := state.ReadTBStreak(out)
	if s0.RecordID == "" || s0.Serial != 1 {
		t.Errorf("the record: id %q serial %d", s0.RecordID, s0.Serial)
	}
	// An unknown run neither extends nor ends it.
	run(t0.Add(97*time.Hour), tbcomplete.Unknown, 0)
	if b := streak(); b.Runs != 97 {
		t.Errorf("an unknown run moved the streak: %+v", b)
	}
	// X stops growing for 3 days: stopped.
	for d := 98; d <= 98+72; d += 12 {
		run(t0.Add(time.Duration(d)*time.Hour), tbcomplete.Behind, 196)
	}
	if b := streak(); !b.Stopped() || !b.StillDownloading() {
		t.Errorf("X unchanged for 3 days: %+v; want stopped", b)
	}
	// A caught_up run ends it; the next behind run starts a new streak, serial 2.
	run(t0.Add(200*time.Hour), tbcomplete.CaughtUp, 1000)
	if _, had := mustStreak(t, out).Account("k1"); had {
		t.Error("a caught-up run did not end the streak")
	}
	run(t0.Add(201*time.Hour), tbcomplete.Behind, 900)
	if b := streak(); b.Serial != 2 || b.Runs != 1 {
		t.Errorf("a new streak: %+v, want serial 2, 1 run", b)
	}
	run(t0.Add(202*time.Hour), tbcomplete.Limited, 900)
	if _, had := mustStreak(t, out).Account("k1"); had {
		t.Error("a limited run did not end the streak")
	}
	if s, _ := state.ReadTBStreak(out); s.RecordID != s0.RecordID {
		t.Errorf("the record was renamed: %q → %q", s0.RecordID, s.RecordID)
	}
	// A run that did not end ok moves nothing.
	updateTBStreak(opts, []tbcomplete.Result{{Account: "imap.c2.example", StoreID: "k1", Verdict: tbcomplete.Behind}}, false, false, t0.Add(203*time.Hour), nil)
	if _, had := mustStreak(t, out).Account("k1"); had {
		t.Error("a failed run started a streak")
	}
}

func mustStreak(t *testing.T, out string) state.TBStreak {
	t.Helper()
	s, err := state.ReadTBStreak(out)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// covers: MA-1173, MA-1180
// The check changes nothing a backup archives (§2.4, Q7; P14's rule): a Thunderbird Gmail store,
// with deleted copies, a mailbox with no key and a folder not kept, archived with the check on
// and with it off, leaves the same records (timestamps masked) and the same bytes in every
// per-message file. Positive twin: the run with the check on did give a verdict.
func TestTheCheckChangesNothingArchived(t *testing.T) {
	folders := append(gmailFolders(10), tbFolder{path: "Old", listed: 2, msgs: []string{tbMsg("old-1", ""), tbMsg("old-gone", "0009")}})
	_, store := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, folders...)
	tbWrite(t, filepath.Join(store, "Leftover"), "From - Mon Mar  3 09:00:00 2025\n"+tbMsg("left-1", "")+"\n")

	on := tmpDir(t)
	if res := runTB(t, Options{Inputs: []string{store}, Out: on, KeepRaw: true}); len(res.Thunderbird) != 1 {
		t.Fatalf("positive twin: the check gave %+v", res.Thunderbird)
	}
	tbCheckOff = true
	off := tmpDir(t)
	res := runTB(t, Options{Inputs: []string{store}, Out: off, KeepRaw: true})
	tbCheckOff = false
	if len(res.Thunderbird) != 0 {
		t.Fatalf("the check was not off: %+v", res.Thunderbird)
	}
	a, b := p14Collect(t, on), p14Collect(t, off)
	assure.Reached(t, len(a.Files), "per-message files")
	if len(a.Entries) != len(b.Entries) || len(a.Files) != len(b.Files) {
		t.Fatalf("records %d/%d, files %d/%d with the check on/off", len(a.Entries), len(b.Entries), len(a.Files), len(b.Files))
	}
	for k, r := range a.Entries {
		ra, _ := json.Marshal(r)
		rb, _ := json.Marshal(b.Entries[k])
		if string(ra) != string(rb) {
			t.Errorf("record %s differs with the check on:\n%s\n%s", k, ra, rb)
		}
	}
	for p, h := range a.Files {
		if b.Files[p] != h {
			t.Errorf("file %s differs with the check on", p)
		}
	}
}
