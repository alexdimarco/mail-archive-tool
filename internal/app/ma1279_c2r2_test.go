package app

// The C2 adversarial pass round 2's fixes, end to end through a real backup run
// (docs/review-thunderbird-completeness-c2-adversarial-r2.md: R2-01, R2-04, R2-05, R2-06, R2-08).
// The round's attack tests are brought in from its clone, each with its positive twin first. Real
// temp profiles and archives only; never the owner's profile.

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// r2BigHeader: see the source package's copy. One email, header block near 64 KiB, CRLF; with
// keys, a per-copy X-Mozilla-Keys line pushes its Message-ID line across the 64 KiB bound.
func r2BigHeader(withKeys bool) string {
	const bound = 64 << 10
	keys := "X-Mozilla-Keys: " + strings.Repeat(" ", 80) + "\r\n"
	head := "X-Mozilla-Status: 0001\r\nX-Mozilla-Status2: 00000000\r\nFrom: a@x\r\nSubject: big\r\n"
	n := bound - 20 - len(keys) - len(head)
	var f strings.Builder
	line := func(l int) { f.WriteString("X-Junk: " + strings.Repeat("a", l-10) + "\r\n") }
	for n >= 2000 {
		line(1000)
		n -= 1000
	}
	line(n / 2)
	line(n - n/2)
	h := head
	if withKeys {
		h = "X-Mozilla-Status: 0001\r\nX-Mozilla-Status2: 00000000\r\n" + keys + "From: a@x\r\nSubject: big\r\n"
	}
	return h + f.String() + "Message-ID: <" + strings.Repeat("b", 40) + "@x>\r\n\r\nbody\r\n"
}

// covers: MA-1280
// R2-01 end to end (brought in): a Gmail account whose All Mail lists 2 and holds 1 email on
// disk, that email also in INBOX with a longer per-copy X-Mozilla-Keys line. The copy whose
// Message-ID straddles the 64 KiB bound has no identity (its header block does not end within
// the bound), never a cut one, so X is never 2: not caught up, not complete. Positive twin: both
// copies with the same per-copy lines read behind, 1 of 2.
func TestR2_01_GmailCutIDMakesFalseCaughtUp(t *testing.T) {
	gm := map[string]string{"is_gmail": "true"}
	layout := func(inbox string) []tbFolder {
		return []tbFolder{
			{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 1, msgs: []string{inbox}},
			{path: "[Gmail]", flags: tbcomplete.FlagImapNoselect, listed: 0},
			{path: "[Gmail]/All Mail", flags: tbcomplete.FlagAllMail | tbcomplete.FlagArchive, listed: 2, msgs: []string{r2BigHeader(false)}},
		}
	}
	_, same := imapProfile(t, "imap.gmail.com", gm, layout(r2BigHeader(false))...)
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{same}})); r.Verdict != tbcomplete.Behind || r.OnDisk != 1 {
		t.Fatalf("positive twin: one email in two folders, 2 listed: %v %v X %d Y %d", r.Verdict, r.Reason, r.OnDisk, r.Listed)
	}
	_, shifted := imapProfile(t, "imap.gmail.com", gm, layout(r2BigHeader(true))...)
	r := onlyResult(t, runTB(t, Options{Inputs: []string{shifted}}))
	if r.Verdict == tbcomplete.CaughtUp || r.MaySayComplete() || r.OnDisk != 1 || r.NoIdentity != 1 {
		t.Errorf("one email on disk (two copies differing in X-Mozilla-Keys), 2 listed: %v X %d Y %d complete %v no identity %d — a false caught_up (R42)", r.Verdict, r.OnDisk, r.Listed, r.MaySayComplete(), r.NoIdentity)
	}
}

// r2Patch is a mailing-list email carrying a git format-patch file as a 7bit text attachment, its
// first line escaped with prefix (">" as current Thunderbird writes) or not ("").
func r2Patch(prefix string) string {
	return "From: Dev <dev@x>\nTo: list@x\nSubject: [PATCH] fix\nMessage-ID: <p1@c2.example>\nX-Mozilla-Status: 0001\nX-Mozilla-Status2: 00000000\n" +
		"MIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: text/plain\n\nsee the patch\n\n--b\n" +
		"Content-Type: text/x-patch; name=\"0001-fix.patch\"\nContent-Transfer-Encoding: 7bit\nContent-Disposition: attachment; filename=\"0001-fix.patch\"\n\n" +
		prefix + "From 3f2a9c0d1e5b7a8c9d0e1f2a3b4c5d6e7f8a9b0c Mon Sep 17 00:00:00 2001\nFrom: Dev <dev@x>\nDate: Mon, 3 Mar 2025 09:00:00 +0000\nSubject: [PATCH] fix\n\n---\n a | 1 +\n--b--\n"
}

// covers: MA-1279
// R2-04 end to end (brought in): INBOX lists 3, holds 2 emails, one of them carrying an unescaped
// git patch attachment. Its first line is no separator Thunderbird writes, so the account is
// never caught up: behind, 2 of 3, never complete. Thunderbird's own separator quoted in a body,
// after an empty line, with 3 listed and 3 on disk: the quoted piece carries no X-Mozilla-Status,
// so it is loose: X stays 2 for certain (never a false caught up) and 3 at most (never a false
// behind): unknown, count_uncertain. Positive twin: the patch line escaped reads behind, 2 of 3.
func TestR2_04_GitPatchAttachmentMakesFalseCaughtUp(t *testing.T) {
	mk := func(listed int, msgs ...string) string {
		_, s := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: listed, msgs: msgs})
		return s
	}
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{mk(3, tbMsg("in-1", ""), r2Patch(">"))}})); r.Verdict != tbcomplete.Behind || r.OnDisk != 2 {
		t.Fatalf("positive twin: %v X %d Y %d", r.Verdict, r.OnDisk, r.Listed)
	}
	r := onlyResult(t, runTB(t, Options{Inputs: []string{mk(3, tbMsg("in-1", ""), r2Patch(""))}}))
	if r.Verdict == tbcomplete.CaughtUp || r.MaySayComplete() || r.Verdict != tbcomplete.Behind || r.OnDisk != 2 || r.SplitMailboxes != 1 {
		t.Errorf("2 emails on disk, one with an unescaped git patch attachment, 3 listed: %v X %d Y %d split %d complete %v — a false caught_up (R42)", r.Verdict, r.OnDisk, r.Listed, r.SplitMailboxes, r.MaySayComplete())
	}
	quoted := tbMsg("in-2", "") + "\nFrom - Mon Mar  3 09:00:00 2025\nFrom: someone@x\n"
	r = onlyResult(t, runTB(t, Options{Inputs: []string{mk(3, tbMsg("in-1", ""), quoted)}}))
	if r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonCountUncertain || r.OnDisk != 2 || r.OnDiskMost != 3 || r.NoStatus != 1 {
		t.Errorf("a quoted separator, 3 listed: %v %v X %d most %d no status %d; want unknown, count_uncertain, 2, 3, 1", r.Verdict, r.Reason, r.OnDisk, r.OnDiskMost, r.NoStatus)
	}
}

// covers: MA-1279
// The owner-shaped case (the lead's count-only scan of the owner's real profile, with the owner's
// permission: 2 of 318,856 messages lack X-Mozilla-Status, in one mailbox): a mailbox listing 170
// with 2 of its messages written without the line is short by 2 for certain, within the tolerance
// (17), so still caught up; never complete (missing 2); the record keeps no_status 2. With 30
// listed messages missing as well it is behind. Positive twin: all 170 with the line.
func TestTwoMessagesWithoutTheStatusLineStayCaughtUp(t *testing.T) {
	noStatus := func(id string) string {
		return "From: Alice <alice@example.com>\nTo: Bob <bob@example.com>\nSubject: message " + id +
			"\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + id + "@c2.example>\nContent-Type: text/plain\n\nbody of " + id + "\n"
	}
	run := func(listed int, msgs []string) (tbcomplete.Result, string) {
		_, s := imapProfile(t, "imap.owner.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: listed, msgs: msgs})
		out := tmpDir(t)
		return onlyResult(t, runTB(t, Options{Inputs: []string{s}, Out: out})), out
	}
	if r, _ := run(170, tbMsgs("in", 170)); r.Verdict != tbcomplete.CaughtUp || !r.MaySayComplete() {
		t.Fatalf("positive twin: %v %v complete %v", r.Verdict, r.Reason, r.MaySayComplete())
	}
	msgs := append(tbMsgs("in", 168), noStatus("ns-1"), noStatus("ns-2"))
	r, out := run(170, msgs)
	if r.Verdict != tbcomplete.CaughtUp || r.MaySayComplete() || r.OnDisk != 168 || r.Missing != 2 || r.Tolerance != 17 || r.NoStatus != 2 {
		t.Errorf("2 of 170 without X-Mozilla-Status: %v %v complete %v X %d missing %d tolerance %d no status %d", r.Verdict, r.Reason, r.MaySayComplete(), r.OnDisk, r.Missing, r.Tolerance, r.NoStatus)
	}
	if lr, _, _ := state.ReadLastRun(out); len(lr.Thunderbird) != 1 || lr.Thunderbird[0].NoStatus != 2 || lr.Thunderbird[0].Verdict != "caught_up" || lr.Thunderbird[0].Complete {
		t.Errorf("the record: %+v", lr.Thunderbird)
	}
	if r, _ := run(200, msgs); r.Verdict != tbcomplete.Behind {
		t.Errorf("200 listed, 170 on disk with 2 without the line: %v %v", r.Verdict, r.Reason)
	}
}

// covers: MA-1279
// R2-05 end to end (brought in): every email Thunderbird lists is on disk, but the writer put no
// empty line before the separators. Those pieces cannot be counted; the account is never told
// behind (unknown, split_differently), so no behind streak starts and C5's "still downloading"
// email cannot come from them. The record keeps glued_mailboxes. Positive twin: the same five
// with empty lines read caught_up.
func TestR2_05_UnseparatedPiecesMakeAFalseBehind(t *testing.T) {
	var msgs []string
	for i := 1; i <= 5; i++ {
		msgs = append(msgs, tbMsg(fmt.Sprintf("in-%d", i), ""))
	}
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{func() string {
		_, s := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 5, msgs: msgs})
		return s
	}()}})); r.Verdict != tbcomplete.CaughtUp {
		t.Fatalf("positive twin: %v %v X %d", r.Verdict, r.Reason, r.OnDisk)
	}
	_, store := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 5, msgs: msgs})
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString("From - Mon Mar  3 09:00:00 2025\n" + m) // no empty line after the message
	}
	tbWrite(t, filepath.Join(store, "INBOX"), b.String())
	out := tmpDir(t)
	r := onlyResult(t, runTB(t, Options{Inputs: []string{store}, Out: out}))
	s, _ := state.ReadTBStreak(out)
	if r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonSplitDifferently || r.GluedMailboxes != 1 || len(s.Accounts) != 0 {
		t.Errorf("all 5 listed emails on disk, separators with no empty line before them: %v %v X %d Y %d glued %d; streak %+v", r.Verdict, r.Reason, r.OnDisk, r.Listed, r.GluedMailboxes, s.Accounts)
	}
	if lr, _, _ := state.ReadLastRun(out); len(lr.Thunderbird) != 1 || lr.Thunderbird[0].GluedMailboxes != 1 {
		t.Errorf("the record: %+v", lr.Thunderbird)
	}
}

// covers: MA-1282
// R2-06 (brought in, and through a real run): 65 accounts in one run, the 65th the only behind
// one. The record keeps 64, the weakest first, so the behind account is in it, and says one was
// left out (thunderbird_more 1); a lead built from the record and that count is never complete.
// Positive twin: 64 accounts, all kept, in the run's order, none left out.
func TestR2_06_AccountsPastTheCapLeaveNoTrace(t *testing.T) {
	var rs []tbcomplete.Result
	for i := 0; i < state.TBAccountsMax; i++ {
		rs = append(rs, tbcomplete.Result{Account: fmt.Sprintf("imap.%02d.example", i), StoreID: fmt.Sprintf("s%02d", i), Verdict: tbcomplete.CaughtUp, Listed: 1, OnDisk: 1})
	}
	if recs := tbRecords(rs, nil, time.Now()); len(recs) != state.TBAccountsMax || recs[0].Account != "imap.00.example" || tbRecordsMore(rs) != 0 {
		t.Fatalf("positive twin: %d", len(recs))
	}
	rs = append(rs, tbcomplete.Result{Account: "imap.behind.example", StoreID: "sb", Verdict: tbcomplete.Behind, Listed: 40, OnDisk: 3, Missing: 37})
	recs := tbRecords(rs, nil, time.Now())
	behind := false
	for _, a := range recs {
		behind = behind || a.Verdict == "behind" || !a.Complete
	}
	if !behind || len(recs) != state.TBAccountsMax || recs[0].Verdict != "behind" || tbRecordsMore(rs) != 1 {
		t.Errorf("65 accounts, the 65th behind: the record keeps %d, behind kept %v, first %q, more %d", len(recs), behind, recs[0].Verdict, tbRecordsMore(rs))
	}
	// The weakest first, each group in the run's order.
	mixed := append([]tbcomplete.Result(nil), rs[:state.TBAccountsMax-1]...)
	mixed = append(mixed, tbcomplete.Result{Account: "u", Verdict: tbcomplete.Unknown}, tbcomplete.Result{Account: "l", Verdict: tbcomplete.Limited},
		tbcomplete.Result{Account: "c", Verdict: tbcomplete.CaughtUp, Listed: 5, OnDisk: 4, Missing: 1})
	got := tbRecords(mixed, nil, time.Now())
	if got[0].Account != "l" || got[1].Account != "u" || got[2].Account != "c" || got[3].Account != "imap.00.example" {
		t.Errorf("the weakest first: %q %q %q %q", got[0].Account, got[1].Account, got[2].Account, got[3].Account)
	}

	// Through a real run: one profile with 65 IMAP accounts, the last one behind.
	var accts []tbAcct
	var caches []map[string]any
	var inputs []string
	p := newTBProfile(t)
	for i := 0; i <= state.TBAccountsMax; i++ {
		host := fmt.Sprintf("imap.%02d.example", i)
		accts = append(accts, tbAcct{n: i + 1, host: host, typ: "imap", dirRel: "ImapMail/" + host})
		listed := 1
		if i == state.TBAccountsMax {
			listed = 30
		}
		store := filepath.Join(p, "ImapMail", host)
		caches = append(caches, tbStoreAt(t, store, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: listed, msgs: tbMsgs(host, 1)}))
		inputs = append(inputs, store)
	}
	tbWrite(t, filepath.Join(p, "prefs.js"), tbPrefsJS(accts...))
	writeTBCache(t, p, caches...)
	out := tmpDir(t)
	res := runTB(t, Options{Inputs: inputs, Out: out})
	if len(res.Thunderbird) != state.TBAccountsMax+1 {
		t.Fatalf("the run gave %d results", len(res.Thunderbird))
	}
	lr, _, err := state.ReadLastRun(out)
	if err != nil || len(lr.Thunderbird) != state.TBAccountsMax || lr.ThunderbirdMore != 1 || lr.Thunderbird[0].Account != fmt.Sprintf("imap.%02d.example", state.TBAccountsMax) || lr.Thunderbird[0].Verdict != "behind" {
		t.Errorf("the run's record: %v, %d accounts, more %d, first %+v", err, len(lr.Thunderbird), lr.ThunderbirdMore, lr.Thunderbird[0])
	}
	var fromRecord []tbcomplete.Result
	for _, a := range lr.Thunderbird {
		if a.Verdict == "caught_up" && a.Complete {
			fromRecord = append(fromRecord, tbcomplete.Result{Verdict: tbcomplete.CaughtUp, Listed: a.Listed, OnDisk: a.OnDisk})
		}
	}
	if tbcomplete.RunLeadWith(fromRecord, lr.ThunderbirdMore) == tbcomplete.LeadComplete {
		t.Error("a lead from the record's complete accounts and its thunderbird_more is complete")
	}
}

// covers: MA-1281
// R2-08 in the run's fold: a full streak record never silently refuses a new behind store. The
// record holds 64 entries no run reaches any more; a new behind store gets its streak, the entry
// reached longest ago goes, and the run's log says so. A store no run reaches for
// state.TBStreakUnseenRuns runs is pruned and logged; one the runs keep reaching (even only as
// unknown) is kept. Positive twin: a behind store with room starts its streak and logs nothing.
func TestAFullStreakRecordMakesRoomAndSaysSo(t *testing.T) {
	out := tmpDir(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	behind := func(id string) []tbcomplete.Result {
		return []tbcomplete.Result{{Account: "imap." + id + ".example", StoreID: id, Verdict: tbcomplete.Behind, OnDisk: 1, Listed: 40}}
	}
	if w := updateTBStreak(Options{Out: out}, behind("first"), false, true, now, logger); w != nil || buf.Len() != 0 {
		t.Fatalf("positive twin: %+v %q", w, buf.String())
	}
	if _, ok := mustStreak(t, out).Account("first"); !ok {
		t.Fatal("positive twin: no streak")
	}
	// 63 more, each in its own run: 64 entries.
	for i := 1; i < state.TBAccountsMax; i++ {
		updateTBStreak(Options{Out: out}, behind(fmt.Sprintf("s%02d", i)), false, true, now.Add(time.Duration(i)*time.Minute), logger)
	}
	if s := mustStreak(t, out); len(s.Accounts) != state.TBAccountsMax || buf.Len() != 0 {
		t.Fatalf("64 entries: %d, log %q", len(s.Accounts), buf.String())
	}
	updateTBStreak(Options{Out: out}, behind("new"), false, true, now.Add(2*time.Hour), logger)
	s := mustStreak(t, out)
	if _, ok := s.Account("new"); !ok || len(s.Accounts) != state.TBAccountsMax {
		t.Errorf("a full record: the new behind store got no streak (%d entries)", len(s.Accounts))
	}
	if _, ok := s.Account("first"); ok || !strings.Contains(buf.String(), "is full (64 accounts)") || !strings.Contains(buf.String(), "imap.first.example") || !strings.Contains(buf.String(), "imap.new.example") {
		t.Errorf("the entry reached longest ago did not go, or the log does not say so: %q", buf.String())
	}
	// One run with a new behind store listed before the oldest entry's store, behind again: the
	// record's streaks are folded first, so the oldest is extended, and the next oldest goes.
	oldest, _ := s.Account("s01")
	updateTBStreak(Options{Out: out}, append(behind("newer"), behind("s01")...), false, true, now.Add(3*time.Hour), logger)
	s = mustStreak(t, out)
	if a, ok := s.Account("s01"); !ok || a.Runs != oldest.Runs+1 || a.Serial != oldest.Serial {
		t.Errorf("the oldest entry, behind again in the run that made room: %+v %v (was %+v)", a, ok, oldest)
	}
	if _, ok := s.Account("s02"); ok {
		t.Error("the next oldest entry did not go")
	}

	// Pruning: two stores, one reached every run (as unknown), one never again.
	out2 := tmpDir(t)
	buf.Reset()
	two := append(behind("kept"), behind("left")...)
	updateTBStreak(Options{Out: out2}, two, false, true, now, logger)
	unknown := []tbcomplete.Result{{Account: "imap.kept.example", StoreID: "kept", Verdict: tbcomplete.Unknown}}
	for i := 1; i < state.TBStreakUnseenRuns; i++ {
		updateTBStreak(Options{Out: out2}, unknown, false, true, now.Add(time.Duration(i)*time.Hour), logger)
	}
	if s := mustStreak(t, out2); len(s.Accounts) != 2 || buf.Len() != 0 {
		t.Fatalf("after %d unseen runs: %d entries, log %q", state.TBStreakUnseenRuns-1, len(s.Accounts), buf.String())
	}
	updateTBStreak(Options{Out: out2}, unknown, false, true, now.Add(100*time.Hour), logger)
	s = mustStreak(t, out2)
	if _, ok := s.Account("left"); ok || len(s.Accounts) != 1 || !strings.Contains(buf.String(), "dropped imap.left.example") {
		t.Errorf("after %d unseen runs: %+v, log %q", state.TBStreakUnseenRuns, s.Accounts, buf.String())
	}
	if a, ok := s.Account("kept"); !ok || a.Runs != 1 {
		t.Errorf("the store reached as unknown: %+v %v (its streak neither extended nor ended)", a, ok)
	}
}
