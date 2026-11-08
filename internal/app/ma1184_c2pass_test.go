package app

// The C2 adversarial pass's tests, brought in from the pass's clone
// (docs/review-thunderbird-completeness-c2-adversarial.md) and turned from findings into guards:
// each asserts the safe behaviour, with its positive twin first. Real temp-dir profiles and
// archives only.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// covers: MA-1184, MA-1180
// ADV-C2-01: two stores whose folders share one name (two profiles, each with
// ImapMail/imap.same.example) backed up in one run keep a streak each, keyed by the store's full
// path, never by the name: either order, the behind store's streak reaches 2 runs and the
// caught-up one has none. The record holds an entry per store, told apart by store_id, and TC-19's
// first-seen time is carried by store, never across them. Positive twin: two distinct names.
func TestADV_C2_01_StreakKeyIsTheStore(t *testing.T) {
	mk := func(host string, behind bool) string {
		listed := 3
		if behind {
			listed = 40
		}
		_, store := imapProfile(t, host, nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: listed, msgs: tbMsgs("m", 3)})
		return store
	}
	twice := func(inputs []string) (state.TBStreak, string, []tbcomplete.Result) {
		out := tmpDir(t)
		var last Result
		for i := 0; i < 2; i++ {
			last = runTB(t, Options{Inputs: inputs, Out: out})
			time.Sleep(10 * time.Millisecond)
		}
		s, err := state.ReadTBStreak(out)
		if err != nil {
			t.Fatal(err)
		}
		return s, out, last.Thunderbird
	}
	behindOf := func(rs []tbcomplete.Result) (string, string) {
		var b, c string
		for _, r := range rs {
			if r.Verdict == tbcomplete.Behind {
				b = r.StoreID
			} else {
				c = r.StoreID
			}
		}
		return b, c
	}
	s, _, rs := twice([]string{mk("imap.ok.example", false), mk("imap.behind.example", true)})
	b, _ := behindOf(rs)
	if a, ok := s.Account(b); !ok || a.Runs != 2 || a.Account != "imap.behind.example" {
		t.Fatalf("positive twin: the behind account's streak: %+v %v", a, ok)
	}
	for _, order := range [][2]bool{{true, false}, {false, true}} {
		s, out, rs := twice([]string{mk("imap.same.example", order[0]), mk("imap.same.example", order[1])})
		b, c := behindOf(rs)
		if b == "" || c == "" || b == c {
			t.Fatalf("order %v: the two stores were not told apart: %q %q", order, b, c)
		}
		if a, ok := s.Account(b); !ok || a.Runs != 2 || a.Serial != 1 {
			t.Errorf("order %v: the behind store's streak is %+v (present %v), want 2 runs, serial 1", order, a, ok)
		}
		if _, ok := s.Account(c); ok || len(s.Accounts) != 1 {
			t.Errorf("order %v: the caught-up store has a streak: %+v", order, s.Accounts)
		}
		lr, _, _ := state.ReadLastRun(out)
		if len(lr.Thunderbird) != 2 || lr.Thunderbird[0].StoreID == lr.Thunderbird[1].StoreID || lr.Thunderbird[0].StoreID == "" {
			t.Errorf("order %v: the record's entries: %+v", order, lr.Thunderbird)
		}
	}
	// TC-19's carry-forward by store: the same name, another store, carries nothing.
	lim := tbcomplete.Limits{MaxAgeDays: 30}
	t1 := time.Now().Add(-time.Hour)
	prev := tbRecords([]tbcomplete.Result{{Account: "imap.same.example", StoreID: "s1", Verdict: tbcomplete.Limited, Limits: lim}}, nil, t1)
	if prev[0].LimitHash == "" {
		t.Fatal("positive twin: no limit fingerprint")
	}
	again := tbRecords([]tbcomplete.Result{{Account: "imap.same.example", StoreID: "s1", Verdict: tbcomplete.Limited, Limits: lim}}, prev, time.Now())
	other := tbRecords([]tbcomplete.Result{{Account: "imap.same.example", StoreID: "s2", Verdict: tbcomplete.Limited, Limits: lim}}, prev, time.Now())
	noID := tbRecords([]tbcomplete.Result{{Account: "imap.same.example", Verdict: tbcomplete.Limited, Limits: lim}}, []state.TBAccount{{Account: "imap.same.example", LimitHash: prev[0].LimitHash, LimitSince: t1}}, time.Now())
	if !again[0].LimitSince.Equal(prev[0].LimitSince) || other[0].LimitSince.Equal(prev[0].LimitSince) || noID[0].LimitSince.Equal(t1) {
		t.Errorf("carry-forward: same store %v, other store %v, no store id %v; first seen %v", again[0].LimitSince, other[0].LimitSince, noID[0].LimitSince, prev[0].LimitSince)
	}
}

// covers: MA-1175
// ADV-C2-02: Gmail's X counts each email once across its folders, a message with no Message-ID
// included (known by its content without Thunderbird's per-copy lines): the same five ID-less
// messages in All Mail and in INBOX count five, so ten listed is behind, never a false caught_up.
// The same message marked read in one folder and unread in the other is still one. Positive twin:
// with Message-IDs, the same layout is behind 5 of 10.
func TestADV_C2_02_GmailNoIDCopiesCountOnce(t *testing.T) {
	gm := map[string]string{"is_gmail": "true"}
	layout := func(withID bool, inboxStatus string) []tbFolder {
		var all, inbox []string
		for i := 1; i <= 5; i++ {
			id := ""
			if withID {
				id = fmt.Sprintf("n-%d", i)
			}
			m := tbMsg(id, "")
			if !withID {
				m = strings.Replace(m, "Subject: message ", fmt.Sprintf("Subject: message n-%d", i), 1)
			}
			all = append(all, m)
			inbox = append(inbox, strings.Replace(m, "X-Mozilla-Status: 0001", "X-Mozilla-Status: "+inboxStatus, 1))
		}
		return []tbFolder{
			{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 5, msgs: inbox},
			{path: "[Gmail]", flags: tbcomplete.FlagImapNoselect, listed: 0},
			{path: "[Gmail]/All Mail", flags: tbcomplete.FlagAllMail | tbcomplete.FlagArchive, listed: 10, msgs: all},
		}
	}
	_, withID := imapProfile(t, "imap.gmail.com", gm, layout(true, "0001")...)
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{withID}})); r.Verdict != tbcomplete.Behind || r.OnDisk != 5 {
		t.Fatalf("positive twin: 5 distinct of 10 listed: %v X %d Y %d", r.Verdict, r.OnDisk, r.Listed)
	}
	for _, status := range []string{"0001", "0000"} {
		_, noID := imapProfile(t, "imap.gmail.com", gm, layout(false, status)...)
		r := onlyResult(t, runTB(t, Options{Inputs: []string{noID}}))
		if r.Verdict != tbcomplete.Behind || r.OnDisk != 5 || r.MaySayComplete() || r.NoIdentity != 0 {
			t.Errorf("INBOX status %s: 5 ID-less messages in two folders, 10 listed: %v X %d Y %d complete %v no identity %d; want behind 5 of 10", status, r.Verdict, r.OnDisk, r.Listed, r.MaySayComplete(), r.NoIdentity)
		}
	}
}

// covers: MA-1173
// ADV-C2-03: body lines that start with "From " (unescaped by the message's writer) never add to
// X: only Thunderbird's own separators are counted, the folder reads behind 5 of 11, and the
// mailbox is recorded as split differently (no complete words). What the backup archives is
// unchanged by the check (on/off equality). Positive twin: the same body escaped counts 5.
func TestADV_C2_03_UnescapedFromLinesNeverInflateX(t *testing.T) {
	body := func(prefix string) string {
		var b strings.Builder
		for i := 0; i < 6; i++ {
			fmt.Fprintf(&b, "%sFrom the desk of Bob, note %d\n", prefix, i)
		}
		return b.String()
	}
	msgs := func(prefix string) []string {
		m := tbMsgs("in", 4)
		return append(m, tbMsg("crafted", "")+body(prefix))
	}
	_, escaped := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 11, msgs: msgs(">")})
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{escaped}})); r.Verdict != tbcomplete.Behind || r.OnDisk != 5 || r.SplitMailboxes != 0 {
		t.Fatalf("positive twin: 5 messages of 11 listed: %v X %d split %d", r.Verdict, r.OnDisk, r.SplitMailboxes)
	}
	_, raw := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 11, msgs: msgs("")})
	out := tmpDir(t)
	r := onlyResult(t, runTB(t, Options{Inputs: []string{raw}, Out: out}))
	if r.Verdict != tbcomplete.Behind || r.OnDisk != 5 || r.SplitMailboxes != 1 || r.MaySayComplete() {
		t.Errorf("5 messages on disk, one with 6 unescaped From lines, 11 listed: %v X %d Y %d split %d", r.Verdict, r.OnDisk, r.Listed, r.SplitMailboxes)
	}
	if lr, _, _ := state.ReadLastRun(out); len(lr.Thunderbird) != 1 || lr.Thunderbird[0].SplitMailboxes != 1 {
		t.Errorf("the record does not say the mailbox was split differently: %+v", lr.Thunderbird)
	}
	// Caught up by Thunderbird's count (5 listed), the split mailbox can't be told complete.
	_, five := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 5, msgs: msgs("")})
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{five}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonSplitDifferently || r.MaySayComplete() {
		t.Errorf("5 of 5 with a split mailbox: %v %v complete %v", r.Verdict, r.Reason, r.MaySayComplete())
	}
}

// covers: MA-1184
// ADV-C2-04 (held, brought in): the encrypted path's stray-record removal removes only the exact
// name: a link is removed as a link (its target kept), a non-empty folder of that name is kept, a
// file of that name in another folder is kept. Positive twin: the exact regular file is removed.
func TestADV_C2_04_EncryptedRemovalTouchesOnlyTheName(t *testing.T) {
	behind := []tbcomplete.Result{{Account: "imap.x.example", StoreID: "k", Verdict: tbcomplete.Behind, OnDisk: 1}}
	out4 := tmpDir(t)
	tbWrite(t, filepath.Join(out4, state.TBStreakName), "{}")
	updateTBStreak(Options{Out: out4}, behind, true, true, time.Now(), discard())
	if _, err := os.Lstat(filepath.Join(out4, state.TBStreakName)); !os.IsNotExist(err) {
		t.Fatalf("positive twin: the stray record stays: %v", err)
	}
	if runtime.GOOS != "windows" {
		out := tmpDir(t)
		target := filepath.Join(tmpDir(t), "precious.txt")
		tbWrite(t, target, "keep me")
		if err := os.Symlink(target, filepath.Join(out, state.TBStreakName)); err != nil {
			t.Fatal(err)
		}
		updateTBStreak(Options{Out: out}, behind, true, true, time.Now(), discard())
		if b, err := os.ReadFile(target); err != nil || string(b) != "keep me" {
			t.Errorf("the link's target: %q %v", b, err)
		}
		if _, err := os.Lstat(filepath.Join(out, state.TBStreakName)); !os.IsNotExist(err) {
			t.Errorf("the stray link stays: %v", err)
		}
	}
	out2 := tmpDir(t)
	tbWrite(t, filepath.Join(out2, state.TBStreakName, "inside.txt"), "x")
	updateTBStreak(Options{Out: out2}, behind, true, true, time.Now(), discard())
	if _, err := os.Stat(filepath.Join(out2, state.TBStreakName, "inside.txt")); err != nil {
		t.Errorf("a folder's contents were removed: %v", err)
	}
	parent := tmpDir(t)
	out3 := filepath.Join(parent, "archive")
	tbWrite(t, filepath.Join(out3, "x"), "")
	tbWrite(t, filepath.Join(parent, state.TBStreakName), "{}")
	updateTBStreak(Options{Out: out3}, behind, true, true, time.Now(), discard())
	if _, err := os.Stat(filepath.Join(parent, state.TBStreakName)); err != nil {
		t.Errorf("another folder's record was removed: %v", err)
	}
}

// covers: MA-1184
// ADV-C2-05: a streak record other users can read is never silent: the run logs a line and
// returns a WARN (tbstreak_unreadable) that the last-run record keeps and status shows; the record
// is not trusted, so the streak starts again from that run, and it is rewritten owner-only, so
// the following runs extend it. Positive twin: an owner-only record is extended with no WARN.
func TestADV_C2_05_LooseModeIsNeverSilent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no group or other permission bits for util.ReadSecureFile to refuse")
	}
	out := tmpDir(t)
	behind := []tbcomplete.Result{{Account: "imap.x.example", StoreID: "k", Verdict: tbcomplete.Behind, OnDisk: 1}}
	t0 := time.Now().Add(-96 * time.Hour)
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	if w := updateTBStreak(Options{Out: out}, behind, false, true, t0, logger); w != nil {
		t.Fatalf("positive twin: a WARN on a first run: %+v", w)
	}
	if w := updateTBStreak(Options{Out: out}, behind, false, true, t0.Add(24*time.Hour), logger); w != nil || mustStreak(t, out).Accounts[0].Runs != 2 || logBuf.Len() != 0 {
		t.Fatalf("positive twin: an owner-only record: %+v %+v %q", w, mustStreak(t, out), logBuf.String())
	}
	if err := os.Chmod(filepath.Join(out, state.TBStreakName), 0o644); err != nil {
		t.Fatal(err)
	}
	w := updateTBStreak(Options{Out: out}, behind, false, true, t0.Add(48*time.Hour), logger)
	if w == nil || w.Code != "tbstreak_unreadable" || !strings.Contains(w.Message, state.TBStreakName) || !strings.Contains(logBuf.String(), "other users") {
		t.Fatalf("a 0644 record: warn %+v, log %q", w, logBuf.String())
	}
	fi, err := os.Stat(filepath.Join(out, state.TBStreakName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the record was not rewritten owner-only: %v %v", fi.Mode(), err)
	}
	if a := mustStreak(t, out).Accounts[0]; a.Runs != 1 || !a.FirstBehind.Equal(t0.Add(48*time.Hour)) {
		t.Errorf("the untrusted streak was carried: %+v (want a fresh one from this run)", a)
	}
	if w := updateTBStreak(Options{Out: out}, behind, false, true, t0.Add(96*time.Hour), logger); w != nil || mustStreak(t, out).Accounts[0].Runs != 2 {
		t.Errorf("the run after: %+v %+v", w, mustStreak(t, out))
	}
	// A loose record and a run with no behind account: still rewritten owner-only, still a WARN.
	if err := os.Chmod(filepath.Join(out, state.TBStreakName), 0o644); err != nil {
		t.Fatal(err)
	}
	unknown := []tbcomplete.Result{{Account: "imap.x.example", StoreID: "k", Verdict: tbcomplete.Unknown}}
	if w := updateTBStreak(Options{Out: out}, unknown, false, true, t0.Add(97*time.Hour), logger); w == nil {
		t.Error("a loose record with an unknown run: no WARN")
	}
	if fi, err := os.Stat(filepath.Join(out, state.TBStreakName)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("a loose record with an unknown run was not rewritten owner-only: %v", err)
	}
	// A result with no store identity never starts a streak.
	fresh := tmpDir(t)
	updateTBStreak(Options{Out: fresh}, []tbcomplete.Result{{Account: "imap.x.example", Verdict: tbcomplete.Behind, OnDisk: 1}}, false, true, t0, logger)
	if _, err := os.Stat(filepath.Join(fresh, state.TBStreakName)); !os.IsNotExist(err) {
		t.Errorf("a result with no store identity started a streak (the record was written): %v", err)
	}
	// End to end: the WARN is in the last-run record and status shows it.
	_, store := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)})
	arch := tmpDir(t)
	runTB(t, Options{Inputs: []string{store}, Out: arch})
	if err := os.Chmod(filepath.Join(arch, state.TBStreakName), 0o644); err != nil {
		t.Fatal(err)
	}
	runTB(t, Options{Inputs: []string{store}, Out: arch})
	lr, _, _ := state.ReadLastRun(arch)
	found := false
	for _, w := range lr.Warnings {
		found = found || w.Code == "tbstreak_unreadable"
	}
	rep := health.Assess(health.Input{Out: arch, LastRun: lr, LastRunState: state.LastRunPresent}, time.Now())
	if !found || !strings.Contains(strings.Join(rep.Codes, " "), "tbstreak_unreadable") || rep.Posture == "GREEN" {
		t.Errorf("the WARN did not reach the record (%v) or status: %+v", found, rep)
	}
}

// covers: MA-1184
// ADV-C2-10 in the run: a streak record a later MailArchive wrote is never written over: nothing
// moves, the file keeps its bytes, and the run returns the WARN. Positive twin: a version-1
// record is extended.
func TestALaterStreakRecordIsNeverDowngraded(t *testing.T) {
	out := tmpDir(t)
	behind := []tbcomplete.Result{{Account: "imap.x.example", StoreID: "k", Verdict: tbcomplete.Behind, OnDisk: 1}}
	tbWrite(t, filepath.Join(out, state.TBStreakName), `{"version":1,"serial":1,"accounts":[{"store":"k","account":"imap.x.example","serial":1,"runs":3}]}`)
	if w := updateTBStreak(Options{Out: out}, behind, false, true, time.Now(), discard()); w != nil || mustStreak(t, out).Accounts[0].Runs != 4 {
		t.Fatalf("positive twin: %+v %+v", w, mustStreak(t, out))
	}
	later := `{"version":2,"serial":7,"accounts":[{"store":"k","account":"imap.x.example","runs":40,"new_field":true}]}`
	tbWrite(t, filepath.Join(out, state.TBStreakName), later)
	w := updateTBStreak(Options{Out: out}, behind, false, true, time.Now(), discard())
	if b, _ := os.ReadFile(filepath.Join(out, state.TBStreakName)); string(b) != later || w == nil || w.Code != "tbstreak_unreadable" {
		t.Errorf("a version-2 record: now %q, warn %+v", b, w)
	}
}

// covers: MA-1180
// ADV-C2-06 (held, brought in): a planted last-run record never changes this run's verdict: a
// behind store stays behind whatever `thunderbird` the old record says. Positive twin: the same
// run with no old record.
func TestADV_C2_06_PlantedRecordNeverMakesCaughtUp(t *testing.T) {
	_, store := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)})
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{store}})); r.Verdict != tbcomplete.Behind {
		t.Fatalf("positive twin: %v", r.Verdict)
	}
	out := tmpDir(t)
	planted := map[string]any{"version": 1, "status": "ok", "started": time.Now().Add(-time.Hour), "finished": time.Now().Add(-time.Hour),
		"thunderbird": []map[string]any{{"account": "imap.c2.example", "verdict": "caught_up", "complete": true, "on_disk": 40, "listed": 40}}}
	data, _ := json.Marshal(planted)
	tbWrite(t, filepath.Join(out, state.LastRunName), string(data))
	r := onlyResult(t, runTB(t, Options{Inputs: []string{store}, Out: out}))
	if r.Verdict != tbcomplete.Behind || r.OnDisk != 3 {
		t.Errorf("a planted caught_up record changed the verdict: %+v", r)
	}
	lr, _, _ := state.ReadLastRun(out)
	if len(lr.Thunderbird) != 1 || lr.Thunderbird[0].Verdict != "behind" || lr.Thunderbird[0].Complete {
		t.Errorf("the record after the run: %+v", lr.Thunderbird)
	}
}

// covers: MA-1276
// ADV-C2-07: the account's own `directory` setting, left at the profile's old place after the
// profile moved, is never "another store": in tail mode (keys still at the old place) the store
// keeps its own keys and is checked. Positive twin: the same profile without the stale `directory`.
func TestADV_C2_07_OwnStaleDirectoryKeepsOwnKeys(t *testing.T) {
	oldStore := filepath.Join(tmpDir(t), "oldhome", "abcd1234.default-release", "ImapMail", "imap.c2.example")
	setup := func(withStaleDir bool) string {
		a := tbAcct{n: 1, host: "imap.c2.example", typ: "imap", dirRel: "ImapMail/imap.c2.example"}
		if withStaleDir {
			a.dir = oldStore
		}
		p := newTBProfile(t, a)
		store := filepath.Join(p, "ImapMail", "imap.c2.example")
		folders := []tbFolder{{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 3, msgs: tbMsgs("in", 3)}}
		tbStoreAt(t, store, folders...)
		writeTBCache(t, p, tbStoreAt(t, oldStore, folders...))
		return store
	}
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{setup(false)}})); r.Verdict != tbcomplete.CaughtUp {
		t.Fatalf("positive twin: a moved profile, keys at the old place, tail mode: %v %v", r.Verdict, r.Reason)
	}
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{setup(true)}})); r.Verdict != tbcomplete.CaughtUp {
		t.Errorf("the account's own stale directory setting made its keys another store's: %v %v", r.Verdict, r.Reason)
	}
}

// covers: MA-1180
// ADV-C2-08: the record always fits the reader: at its own bounds (64 accounts, 200 kept and 3×10
// not-kept folders each, names of 80 characters of '<', 6 bytes each in JSON) the folders of all
// accounts stay within state.TBFoldersTotalMax, the `thunderbird` field within 1 MiB (a quarter of
// ReadLastRun's 4 MiB, leaving the rest of the record its room), and the record reads back. The
// folders cut are counted in the More fields. Positive twin: one account keeps all its folders.
func TestADV_C2_08_RecordBoundsFitTheReader(t *testing.T) {
	name := strings.Repeat("<", state.TBNameMax)
	var folders []tbcomplete.FolderCount
	for i := 0; i < state.TBFolderMax; i++ {
		folders = append(folders, tbcomplete.FolderCount{Path: fmt.Sprintf("%s%03d", name[:state.TBNameMax-3], i), Listed: 1 << 40, OnDisk: 1 << 39})
	}
	var notKept []tbcomplete.FolderCount
	for i := 0; i < state.TBNotKeptMax+3; i++ {
		notKept = append(notKept, tbcomplete.FolderCount{Path: name, Listed: 1 << 40})
	}
	acct := func(i int) tbcomplete.Result {
		return tbcomplete.Result{Account: fmt.Sprintf("%s%02d", name[:state.TBNameMax-2], i), StoreID: strings.Repeat("f", 32), Verdict: tbcomplete.Behind,
			Folders: folders, NotKept: notKept, DefaultNotKept: notKept, Limits: tbcomplete.Limits{MaxAgeDays: 1}}
	}
	one := tbRecords([]tbcomplete.Result{acct(0)}, nil, time.Now())
	if len(one[0].Folders) != state.TBFolderMax || len(one[0].NotKept) != state.TBNotKeptMax || len(one[0].DefaultNotKept) != state.TBNotKeptMax || one[0].NotKeptMore != 3 || one[0].DefaultNotKeptMore != 3 {
		t.Fatalf("positive twin: one account: %d kept, %d not kept (+%d), %d default (+%d)", len(one[0].Folders), len(one[0].NotKept), one[0].NotKeptMore, len(one[0].DefaultNotKept), one[0].DefaultNotKeptMore)
	}
	var rs []tbcomplete.Result
	for i := 0; i < state.TBAccountsMax; i++ {
		rs = append(rs, acct(i))
	}
	recs := tbRecords(rs, nil, time.Now())
	total := 0
	for _, a := range recs {
		total += len(a.Folders) + len(a.NotKept) + len(a.DefaultNotKept)
		if len(a.Folders)+a.FoldersMore != state.TBFolderMax || len(a.NotKept)+a.NotKeptMore != len(notKept) || len(a.DefaultNotKept)+a.DefaultNotKeptMore != len(notKept) {
			t.Errorf("%s: a cut folder is not counted", a.Account[:4])
		}
	}
	data, err := json.Marshal(recs)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worst case: %d accounts, %d folders, %d bytes of `thunderbird`", len(recs), total, len(data))
	if total > state.TBFoldersTotalMax || len(data) > 1<<20 {
		t.Errorf("the record's own bounds allow %d folders and %d bytes of `thunderbird`; want at most %d and %d", total, len(data), state.TBFoldersTotalMax, 1<<20)
	}
	out := tmpDir(t)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Thunderbird: recs}); err != nil {
		t.Fatal(err)
	}
	if lr, st, err := state.ReadLastRun(out); err != nil || st != state.LastRunPresent || len(lr.Thunderbird) != state.TBAccountsMax {
		t.Errorf("the worst-case record does not read back: %v %v %d", err, st, len(lr.Thunderbird))
	}
}

// covers: MA-1202
// ADV-C2-15 (held, brought in): the run refuses a whole profile folder as an input, so no verdict
// can come from it. Positive twin: the store itself is told behind.
func TestADV_C2_15_ProfileFolderInput(t *testing.T) {
	p, store := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)})
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{store}})); r.Verdict != tbcomplete.Behind {
		t.Fatalf("positive twin: the store itself: %v", r.Verdict)
	}
	res, err := Run(context.Background(), Options{Inputs: []string{p}, Out: tmpDir(t), Mode: export.Incremental}, discard(), nil)
	if err == nil {
		if len(res.Thunderbird) != 1 || res.Thunderbird[0].Verdict != tbcomplete.Behind {
			t.Errorf("the profile folder as input: %+v (lead %v)", res.Thunderbird, tbcomplete.RunLead(res.Thunderbird))
		}
	} else if !strings.Contains(err.Error(), "no input mail sources found") {
		t.Errorf("the profile folder as input: %v", err)
	}
}

// covers: MA-1202
// The C2 pass's LOW item 3, a walk that ended with an error: the store is told unknown
// (walk_failed), never left out, and Thunderbird's folder list is not read for it; a store
// another reader opened (no counts) is told other_reader without a nil count being read. The
// run's own failure words still win (a failed run records no completeness, §11). Positive twin:
// the same planned store, its walk not failed, is told from its counts. (runFile's line that
// marks the store failed is covered by reading: an input that fails is refused before its walk
// by the input scan, and the walk's own errors need a broken archive drive.)
func TestAStoreWhoseWalkFailedIsUnknown(t *testing.T) {
	_, store := imapProfile(t, "imap.c2.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)})
	reads := countCacheReads(t, nil)
	tb := planTB(store)
	if tb.counts == nil {
		t.Fatal("the store is not counted")
	}
	if rs := assessThunderbird([]*tbStore{tb}, time.Now()); len(rs) != 1 || rs[0].Reason == tbcomplete.ReasonWalkFailed || *reads != 1 || rs[0].StoreID == "" {
		t.Fatalf("positive twin: %+v, %d cache reads", rs, *reads)
	}
	failed := planTB(store)
	failed.walkFailed = true
	if rs := assessThunderbird([]*tbStore{failed}, time.Now()); len(rs) != 1 || rs[0].Verdict != tbcomplete.Unknown || rs[0].Reason != tbcomplete.ReasonWalkFailed || *reads != 1 {
		t.Errorf("a store whose walk failed: %+v, %d cache reads", rs, *reads)
	}
	other := planTBOther(store)
	if rs := assessThunderbird([]*tbStore{other}, time.Now()); other == nil || len(rs) != 1 || rs[0].Reason != tbcomplete.ReasonOtherReader {
		t.Errorf("a store another reader opened: %+v", rs)
	}
	if planTBOther(filepath.Join(tmpDir(t), "evolution", "local")) != nil {
		t.Error("a folder under no Thunderbird profile was planned")
	}
}

// covers: MA-1202
// ADV-C2-16: a Thunderbird store in maildir format whose server has folders named "cur" and ".x"
// is opened by the Evolution reader (source detection is unchanged here): the account is told
// unknown (other_reader), never left out, so the run's lead is never "complete". A real Evolution
// store, under no Thunderbird profile, still gets no result. Positive twin: the same store without
// those folders is told behind.
func TestADV_C2_16_StoreReadByAnotherReaderIsUnknown(t *testing.T) {
	build := func(odd bool) string {
		p := newTBProfile(t, tbAcct{n: 1, host: "imap.c2.example", typ: "imap", dirRel: "ImapMail/imap.c2.example"})
		store := filepath.Join(p, "ImapMail", "imap.c2.example")
		for i, m := range tbMsgs("in", 3) {
			tbWrite(t, filepath.Join(store, "INBOX", "cur", fmt.Sprintf("%d.eml", i)), m)
		}
		tbWrite(t, filepath.Join(store, "INBOX.msf"), "")
		checked := time.Now().Add(-time.Hour)
		entries := map[string]any{store: map[string]any{"flags": 0x0c, "totalMsgs": -1, "totalUnreadMsgs": -1, "pendingUnreadMsgs": 0, "pendingMsgs": 0, "expungedBytes": 0, "folderSize": -1},
			filepath.Join(store, "INBOX.msf"): tbEntry(tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40}, checked)}
		if odd {
			for _, f := range []string{"cur", ".x"} {
				tbWrite(t, filepath.Join(store, f, "cur", "1.eml"), tbMsg(f+"-1", ""))
				tbWrite(t, filepath.Join(store, f+".msf"), "")
				entries[filepath.Join(store, f+".msf")] = tbEntry(tbFolder{path: f, listed: 1}, checked)
			}
		}
		writeTBCache(t, p, entries)
		return store
	}
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{build(false)}})); r.Verdict != tbcomplete.Behind {
		t.Fatalf("positive twin: a maildir-format store: %v %v X %d", r.Verdict, r.Reason, r.OnDisk)
	}
	res := runTB(t, Options{Inputs: []string{build(true)}})
	if len(res.Thunderbird) != 1 || res.Thunderbird[0].Verdict != tbcomplete.Unknown || res.Thunderbird[0].Reason != tbcomplete.ReasonOtherReader || tbcomplete.RunLead(res.Thunderbird) == tbcomplete.LeadComplete {
		t.Errorf("a behind Thunderbird store with folders named cur and .x: %+v (lead %v)", res.Thunderbird, tbcomplete.RunLead(res.Thunderbird))
	}
	// A real Evolution store under no Thunderbird profile: no result, as before.
	evo := filepath.Join(tmpDir(t), "evolution", "mail", "local")
	tbWrite(t, filepath.Join(evo, "cur", "1.eml"), tbMsg("e-1", ""))
	tbWrite(t, filepath.Join(evo, ".Sub", "cur", "1.eml"), tbMsg("e-2", ""))
	if res := runTB(t, Options{Inputs: []string{evo}}); len(res.Thunderbird) != 0 {
		t.Errorf("an Evolution store was told: %+v", res.Thunderbird)
	}
}
