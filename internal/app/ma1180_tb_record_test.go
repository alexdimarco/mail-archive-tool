package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// covers: MA-1180, MA-1173
// A behind run is a success with a note (the completeness design §5, Q2; slice C2): its record
// is "ok", nothing is partial and no attention notice is written, so the alert streak, which
// keys on the run's outcome, is not moved; the last-run record carries `thunderbird` (the
// account by its store folder's name, the verdict and its X and Y) and the run-history entry
// `tb` ({account, verdict, x, y}). The walk counts as it reads: a deleted copy not yet compacted
// (0x0008) is still archived but left out of X. Positive twin first: a store with everything
// on disk is caught_up and may say complete.
func TestABehindRunIsRecordedOKWithItsCounts(t *testing.T) {
	t.Run("positive twin: caught up", func(t *testing.T) {
		_, store := imapProfile(t, "imap.c2.example", nil,
			tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 3, msgs: tbMsgs("in", 3)},
			tbFolder{path: "Sent", flags: tbcomplete.FlagSentMail, listed: 2, msgs: tbMsgs("sent", 2)})
		out := tmpDir(t)
		res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}, discard(), nil)
		if err != nil {
			t.Fatal(err)
		}
		r := onlyResult(t, res)
		if r.Verdict != tbcomplete.CaughtUp || !r.MaySayComplete() || r.OnDisk != 5 || r.Listed != 5 || r.Account != "imap.c2.example" {
			t.Fatalf("a store with all of its mail on disk: %+v", r)
		}
		lr, st, err := state.ReadLastRun(out)
		if err != nil || st != state.LastRunPresent || len(lr.Thunderbird) != 1 {
			t.Fatalf("record: %v %v %+v", err, st, lr.Thunderbird)
		}
		if a := lr.Thunderbird[0]; a.Verdict != "caught_up" || !a.Complete || a.OnDisk != 5 || a.Listed != 5 || a.Account != "imap.c2.example" {
			t.Errorf("the record's account: %+v", a)
		}
	})

	t.Run("behind", func(t *testing.T) {
		inbox := append(tbMsgs("in", 3), tbMsg("deleted", "0009"))
		_, store := imapProfile(t, "imap.c2.example", nil,
			tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: inbox},
			tbFolder{path: "Sent", flags: tbcomplete.FlagSentMail, listed: 30})
		out := tmpDir(t)
		res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}, discard(), nil)
		if err != nil {
			t.Fatalf("a behind run failed: %v", err)
		}
		if res.Stats.Exported != 4 || res.Partial {
			t.Fatalf("the run archived %d (partial %v), want all 4 messages on disk, the deleted copy too", res.Stats.Exported, res.Partial)
		}
		r := onlyResult(t, res)
		if r.Verdict != tbcomplete.Behind || r.OnDisk != 3 || r.Listed != 70 || r.MaySayComplete() {
			t.Fatalf("result: %+v", r)
		}
		lr, _, _ := state.ReadLastRun(out)
		if lr.Status != state.RunOK || lr.Partial != nil || lr.Error != "" {
			t.Errorf("a behind run is recorded %q (partial %+v, error %q), want ok", lr.Status, lr.Partial, lr.Error)
		}
		if _, err := os.Stat(filepath.Join(out, state.AttentionName)); !os.IsNotExist(err) {
			t.Errorf("a behind run left an attention notice (%v)", err)
		}
		assure.Reached(t, len(lr.Thunderbird), "accounts in the record")
		a := lr.Thunderbird[0]
		if a.Account != "imap.c2.example" || a.Verdict != "behind" || a.Complete || a.OnDisk != 3 || a.Listed != 70 || a.Missing != 67 {
			t.Errorf("the record's account: %+v", a)
		}
		if len(a.Folders) != 2 || a.Folders[0].Folder != "INBOX" || a.Folders[0].Listed != 40 || a.Folders[0].OnDisk != 3 || a.Folders[1].Folder != "Sent" {
			t.Errorf("the record's folders (largest shortfall first): %+v", a.Folders)
		}
		h, err := state.ReadRunHistory(out)
		if err != nil || len(h) != 1 || h[0].TB == nil {
			t.Fatalf("history: %v %+v", err, h)
		}
		if got := *h[0].TB; len(got) != 1 || got[0] != (state.TBCompact{Account: "imap.c2.example", Verdict: "behind", X: 3, Y: 70}) {
			t.Errorf("the history's tb: %+v", got)
		}
		raw := lastRunRaw(t, out)
		if _, ok := raw["thunderbird"]; !ok {
			t.Errorf("the record has no thunderbird key: %v", raw)
		}
	})
}

// covers: MA-1173, MA-1180
// The count is the walk's, taken before the manifest skip and the date filter (§2.4): an
// incremental re-run that archives nothing new, and a run whose -since leaves every message
// out, count what the first run counted.
func TestTheCountIsTakenBeforeTheSkipAndTheDateFilter(t *testing.T) {
	_, store := imapProfile(t, "imap.c2.example", nil,
		tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 3, msgs: tbMsgs("in", 3)})
	out := tmpDir(t)
	opts := Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}
	first, err := Run(context.Background(), opts, discard(), nil)
	if err != nil || first.Stats.Exported != 3 || onlyResult(t, first).OnDisk != 3 {
		t.Fatalf("positive twin, first run: %v exported %d %+v", err, first.Stats.Exported, first.Thunderbird)
	}
	second, err := Run(context.Background(), opts, discard(), nil)
	if err != nil || second.Stats.Exported != 0 {
		t.Fatalf("second run: %v exported %d", err, second.Stats.Exported)
	}
	if r := onlyResult(t, second); r.OnDisk != 3 || r.Verdict != tbcomplete.CaughtUp {
		t.Errorf("an incremental re-run counted %+v, want 3 on disk, caught up", r)
	}
	filtered := opts
	filtered.Out = tmpDir(t)
	filtered.Since = time.Now().Add(24 * time.Hour)
	third, err := Run(context.Background(), filtered, discard(), nil)
	if err != nil || third.Stats.Exported != 0 {
		t.Fatalf("a -since run: %v exported %d", err, third.Stats.Exported)
	}
	if r := onlyResult(t, third); r.OnDisk != 3 {
		t.Errorf("a run whose date filter left every message out counted %d, want 3", r.OnDisk)
	}
}

// covers: MA-1180
// Thunderbird's folder list is read after the walk of every store, once per profile per run
// (TC-21): two accounts of one profile backed up in one run read folderCache.json once, and by
// then both stores' messages are in the archive.
func TestTheFolderCacheIsReadOnceAfterTheWalk(t *testing.T) {
	p := newTBProfile(t,
		tbAcct{n: 1, host: "imap.one.example", typ: "imap", dirRel: "ImapMail/imap.one.example"},
		tbAcct{n: 2, host: "imap.two.example", typ: "imap", dirRel: "ImapMail/imap.two.example"})
	one, two := filepath.Join(p, "ImapMail", "imap.one.example"), filepath.Join(p, "ImapMail", "imap.two.example")
	writeTBCache(t, p,
		tbStoreAt(t, one, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("one", 2)}),
		tbStoreAt(t, two, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("two", 2)}))
	out := tmpDir(t)
	var exportedAtRead int
	reads := countCacheReads(t, func(string) {
		m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
		if err == nil {
			exportedAtRead = m.Len()
		}
	})
	res, err := Run(context.Background(), Options{Inputs: []string{one, two}, Out: out, Mode: export.Incremental}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Thunderbird) != 2 {
		t.Fatalf("positive twin: %d results, want one per account", len(res.Thunderbird))
	}
	for _, r := range res.Thunderbird {
		if r.Verdict != tbcomplete.CaughtUp {
			t.Errorf("%s: %v (%v)", r.Account, r.Verdict, r.Reason)
		}
	}
	if *reads != 1 {
		t.Errorf("folderCache.json read %d times for one profile in one run, want 1", *reads)
	}
	if exportedAtRead != 4 {
		t.Errorf("the folder cache was read with %d of the 4 messages archived: not after the walk", exportedAtRead)
	}
}

// covers: MA-1180
// TC-19's fingerprints: a limited account and a not-kept folder are recorded with a hash of the
// setting and of the folder list, each with the time it was first seen; a second run with the
// same setting and folders carries the first-seen times forward (the previous record is read
// before the run writes its "running" one); a changed setting gets a new time, while the
// unchanged folder list keeps its own.
func TestFingerprintsAreCarriedForwardWhileUnchanged(t *testing.T) {
	folders := []tbFolder{
		{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)},
		{path: "Projects", notKept: true, listed: 12},
	}
	p, store := imapProfile(t, "imap.c2.example", map[string]string{"autosync_max_age_days": "30"}, folders...)
	out := tmpDir(t)
	opts := Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}
	run := func() state.TBAccount {
		t.Helper()
		if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
			t.Fatal(err)
		}
		lr, _, _ := state.ReadLastRun(out)
		if len(lr.Thunderbird) != 1 {
			t.Fatalf("record: %+v", lr.Thunderbird)
		}
		return lr.Thunderbird[0]
	}
	first := run()
	if first.Verdict != "limited" || first.Limit == nil || first.Limit.MaxAgeDays != 30 || first.LimitHash == "" || first.LimitSince.IsZero() {
		t.Fatalf("positive twin: the limited account's record: %+v", first)
	}
	if len(first.NotKept) != 1 || first.NotKept[0].Folder != "Projects" || first.NotKeptHash == "" || first.NotKeptSince.IsZero() {
		t.Fatalf("the not-kept folder's record: %+v", first)
	}
	time.Sleep(20 * time.Millisecond)
	second := run()
	if !second.LimitSince.Equal(first.LimitSince) || second.LimitHash != first.LimitHash {
		t.Errorf("an unchanged limit's first-seen time moved: %v → %v", first.LimitSince, second.LimitSince)
	}
	if !second.NotKeptSince.Equal(first.NotKeptSince) || second.NotKeptHash != first.NotKeptHash {
		t.Errorf("an unchanged not-kept list's first-seen time moved: %v → %v", first.NotKeptSince, second.NotKeptSince)
	}
	time.Sleep(20 * time.Millisecond)
	tbWrite(t, filepath.Join(p, "prefs.js"), tbPrefsJS(tbAcct{n: 1, host: "imap.c2.example", typ: "imap", dirRel: "ImapMail/imap.c2.example",
		extra: map[string]string{"autosync_max_age_days": "60"}}))
	third := run()
	if third.LimitHash == second.LimitHash || !third.LimitSince.After(second.LimitSince) || third.Limit.MaxAgeDays != 60 {
		t.Errorf("a changed limit kept its fingerprint or time: %+v (was %+v)", third, second)
	}
	if !third.NotKeptSince.Equal(first.NotKeptSince) {
		t.Errorf("the unchanged not-kept list's time moved with the limit's: %v → %v", first.NotKeptSince, third.NotKeptSince)
	}
}

// covers: MA-1180
// A run that does not end ok writes no completeness record (§11: the failure words win), though
// the walk's verdict is still in memory for the screen.
func TestAFailedRunRecordsNoCompleteness(t *testing.T) {
	_, store := imapProfile(t, "imap.c2.example", nil,
		tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 3, msgs: tbMsgs("in", 3)})
	out := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if lr, _, _ := state.ReadLastRun(out); len(lr.Thunderbird) != 1 {
		t.Fatalf("positive twin: %+v", lr.Thunderbird)
	}
	// A second input, a plain maildir folder, one of whose files cannot be read: the run ends
	// partial, a failure, after the Thunderbird store's walk ended.
	md := filepath.Join(tmpDir(t), "plain")
	tbWrite(t, filepath.Join(md, "cur", "m1"), tbMsg("plain-1", ""))
	tbWrite(t, filepath.Join(md, "cur", "m2"), tbMsg("plain-2", ""))
	restore := source.InjectMaildirReadFaults(func(rel string) error {
		if strings.HasSuffix(rel, "m2") {
			return syscall.EIO
		}
		return nil
	})
	defer restore()
	res, err := Run(context.Background(), Options{Inputs: []string{store, md}, Out: out, Mode: export.Incremental}, discard(), nil)
	if err == nil {
		t.Fatalf("a run with an unreadable file succeeded: %+v", res)
	}
	if r := onlyResult(t, res); r.Verdict != tbcomplete.CaughtUp {
		t.Errorf("the verdict in memory: %+v", r)
	}
	lr, _, _ := state.ReadLastRun(out)
	if lr.Status != state.RunFailed || len(lr.Thunderbird) != 0 {
		t.Errorf("a failed run's record: %q %+v", lr.Status, lr.Thunderbird)
	}
	h, _ := state.ReadRunHistory(out)
	if len(h) != 2 || h[1].TB != nil {
		t.Errorf("a failed run's history entry carries tb: %+v", h)
	}
	if strings.Contains(string(mustRead(t, filepath.Join(out, state.LastRunName))), "thunderbird") {
		t.Error("a failed run's record names thunderbird")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
