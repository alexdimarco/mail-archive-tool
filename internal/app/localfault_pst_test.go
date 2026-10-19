package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// sourceMessages walks the source at path and returns what it yields. The source
// stays open until the test ends, because a message streams its attachments from it.
func sourceMessages(t *testing.T, path string) []*model.Message {
	t.Helper()
	s, err := source.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var msgs []*model.Message
	if err := s.Walk(func(_ []string, m *model.Message) error { msgs = append(msgs, m); return nil }); err != nil {
		t.Fatal(err)
	}
	return msgs
}

func countHTML(files map[string]string) int {
	n := 0
	for p := range files {
		if strings.HasSuffix(p, ".html") {
			n++
		}
	}
	return n
}

// covers: MA-318, MA-315, R1, R3, R7, R10, S46
// Five unreadable PST nodes in one folder are FIVE ledger gaps, 5 report rows and
// NO record or page (F33, FC7): the other twelve messages are archived, the run is
// partial (ErrPartial), and nothing is minted for the five — not a stub record, not
// a placeholder page. Twin first: the healthy copy archives all 17 with an empty
// ledger and a nil error.
func TestUnreadablePSTNodesAreLedgerGapsNotRecords(t *testing.T) {
	data := pstFixtureData(t)
	nids := pstMessageNIDs(t, data)

	healthy := tmpDir(t)
	hres, herr := runLocal(t, healthy, writeData(t, tmpDir(t), "old.pst", data))
	if herr != nil || hres.Partial || loadArchive(t, healthy).Len() != 17 || len(ledgerOf(t, healthy)) != 0 {
		t.Fatalf("twin: the healthy copy gave err %v, partial %v, %d records, %d gaps; want nil, false, 17, 0", herr, hres.Partial, loadArchive(t, healthy).Len(), len(ledgerOf(t, healthy)))
	}

	mut := data
	for _, nid := range nids[:5] {
		mut = corruptNode(t, mut, nid)
	}
	path := writeData(t, tmpDir(t), "old.pst", mut)
	out := tmpDir(t)
	res, err := runLocal(t, out, path)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("run err = %v; want ErrPartial", err)
	}
	m := loadArchive(t, out)
	if m.Len() != 12 || res.Stats.Exported != 12 {
		t.Errorf("%d records, %d exported; want the 12 readable messages and no more (no stub record)", m.Len(), res.Stats.Exported)
	}
	for k, r := range m.Entries {
		if r.Subject == "(unreadable message)" {
			t.Errorf("record %q is a stub: FC7 mints none", k)
		}
	}
	if pages := countHTML(messageFiles(t, out)); pages != 12 {
		t.Errorf("%d pages; want 12 (no placeholder page)", pages)
	}
	gaps := m.CaptureGapsSorted()
	locators := map[string]bool{}
	for _, g := range gaps {
		if g.Reason != "pst-message-unreadable" || g.Scope != state.GapScopeItem || g.Class != state.ClassTerminal || !strings.HasPrefix(g.Locator, "pst-nid:") {
			t.Errorf("gap %+v; want a terminal item gap pst-message-unreadable at a pst-nid: locator", g)
		}
		locators[g.Locator] = true
	}
	if len(gaps) != 5 || len(locators) != 5 {
		t.Errorf("%d gaps with %d distinct locators; want 5 and 5, never one collapsed entry", len(gaps), len(locators))
	}
	if res.Gaps.ItemGaps != 5 || res.Gaps.Observed != 5 || res.Gaps.New != 5 || !res.Partial {
		t.Errorf("summary %+v, partial %v; want 5 item gaps, all new, and a partial run", res.Gaps, res.Partial)
	}
	rows := 0
	for _, l := range readLines(t, assure.Reached(t, res.ReportPath, "the report"))[1:] {
		f := strings.Split(l, "\t")
		if f[0] == "pst-message-unreadable" {
			rows++
			if f[6] != state.SourceDisplay(state.PathSourceID(path)) || !strings.Contains(f[5], "why: ") || !strings.Contains(f[5], "what to do: ") || !strings.Contains(f[5], "scanpst.exe") {
				t.Errorf("report row %q: want the .pst path, a why and the in-place repair remedy", l)
			}
		}
	}
	if rows != 5 {
		t.Errorf("%d pst-message-unreadable report rows; want 5", rows)
	}
}

// covers: MA-319, R1, R6, R13, S46
// Recovery (P6, FC6-FC8): a PST gap closes when the item is read again — restoring the
// corrupted block at the SAME path gets the real message captured, closes its gap as
// recovered, drops its report row and writes or deletes no other file; a repaired file
// at ANOTHER path is a new store (the old gap is left, the documented duplicate); a
// repair that renumbers or removes the item closes the old gap as "no longer in the
// source"; a gap carrying another store's uid is never recovered by a message at the
// same NID; an -outlook-style rebuilt store keeps ONE per-folder gap whose Runs
// counts across the rebuilds; a partly-read record an old binary's save stripped of
// its Locator gets it back. `verify` still attests after every case.
func TestRecoveryAndClosureOfPSTGaps(t *testing.T) {
	data := pstFixtureData(t)
	nids := pstMessageNIDs(t, data)
	attests := func(t *testing.T, out string) {
		t.Helper()
		rep, err := Verify(out, VerifyOptions{}, nil, nil)
		if err != nil || !rep.Attested() {
			t.Errorf("verify after the case: attested %v, err %v, %d modified, %d missing", rep.Attested(), err, rep.Modified, rep.Missing)
		}
	}

	t.Run("restored at the same path", func(t *testing.T) {
		dir, out := tmpDir(t), tmpDir(t)
		path := writeData(t, dir, "old.pst", corruptNode(t, data, nids[0]))
		if _, err := runLocal(t, out, path); !errors.Is(err, ErrPartial) || len(ledgerOf(t, out)) != 1 {
			t.Fatalf("setup: err %v, %d gaps; want ErrPartial and one gap", err, len(ledgerOf(t, out)))
		}
		before := messageFiles(t, out)
		writeData(t, dir, "old.pst", data) // the repair, in place
		res, err := runLocal(t, out, path)
		if err != nil || res.Partial {
			t.Fatalf("run after the repair: err %v, partial %v; want nil, false", err, res.Partial)
		}
		if g := ledgerOf(t, out); len(g) != 0 {
			t.Errorf("gaps after the repair: %+v; want none", g)
		}
		if res.Gaps.Recovered != 1 || res.Stats.Exported != 1 || loadArchive(t, out).Len() != 17 {
			t.Errorf("recovered %d, exported %d, records %d; want 1, 1, 17", res.Gaps.Recovered, res.Stats.Exported, loadArchive(t, out).Len())
		}
		if res.ReportPath != "" {
			if b := readLines(t, res.ReportPath); len(b) > 1 {
				t.Errorf("the report still lists %v", b[1:])
			}
		}
		after := messageFiles(t, out)
		for p, sum := range before {
			if after[p] != sum {
				t.Errorf("recovery changed or deleted %s", p)
			}
		}
		if len(after) != len(before)+1 {
			t.Errorf("%d message files after, %d before; want exactly the one recovered page more", len(after), len(before))
		}
		attests(t, out)
	})

	t.Run("repaired at another path", func(t *testing.T) {
		out := tmpDir(t)
		bad := writeData(t, tmpDir(t), "old.pst", corruptNode(t, data, nids[0]))
		if _, err := runLocal(t, out, bad); !errors.Is(err, ErrPartial) {
			t.Fatal(err)
		}
		first := ledgerOf(t, out)
		good := writeData(t, tmpDir(t), "old.pst", data)
		res, err := runLocal(t, out, good)
		if err != nil || res.Partial {
			t.Fatalf("run of the repaired copy: err %v, partial %v", err, res.Partial)
		}
		now := ledgerOf(t, out)
		if len(now) != 1 || now[0].Token != first[0].Token || !now[0].LastRun.Equal(first[0].LastRun) || res.Gaps.Recovered != 0 {
			t.Errorf("the old gap %+v after the copy at another path (was %+v), recovered %d; want it unchanged and nothing recovered", now, first, res.Gaps.Recovered)
		}
		if got := loadArchive(t, out).Len(); got != 16+17 {
			t.Errorf("%d records; want the first store's 16 and the copy's 17 (the documented duplicate)", got)
		}
		attests(t, out)
	})

	t.Run("repair renumbers or removes the item", func(t *testing.T) {
		dir, out := tmpDir(t), tmpDir(t)
		path := writeData(t, dir, "old.pst", corruptNode(t, data, nids[0]))
		if _, err := runLocal(t, out, path); !errors.Is(err, ErrPartial) {
			t.Fatal(err)
		}
		// The NID the gap names no longer exists after the repair: the ledger holds the
		// old locator, the repaired file the item under another one.
		m := loadArchive(t, out)
		for k, g := range m.CaptureGaps {
			delete(m.CaptureGaps, k)
			g.Locator = g.Locator[:strings.LastIndex(g.Locator, "/")+1] + fmt.Sprint(nids[0]+32*5000)
			m.CaptureGaps[state.GapKey(g.Token, g.Scope, g.Locator)] = g
		}
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		writeData(t, dir, "old.pst", data)
		res, err := runLocal(t, out, path)
		if err != nil || res.Partial {
			t.Fatalf("run: err %v, partial %v", err, res.Partial)
		}
		if g := ledgerOf(t, out); len(g) != 0 || res.Gaps.ClosedGone != 1 || res.Gaps.Recovered != 0 {
			t.Errorf("gaps %+v, closed-gone %d, recovered %d; want the old gap closed as no longer in the source, nothing recovered", g, res.Gaps.ClosedGone, res.Gaps.Recovered)
		}
		if loadArchive(t, out).Len() != 17 {
			t.Errorf("%d records; want the real message archived as new (17)", loadArchive(t, out).Len())
		}
		attests(t, out)
	})

	t.Run("another store's uid is never a match", func(t *testing.T) {
		path := writeData(t, tmpDir(t), "old.pst", data)
		out := tmpDir(t)
		tok := tokenOf(t, path)
		m := loadArchive(t, out)
		m.Token(state.PathSourceID(path), tok)
		m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Drafts", Scope: state.GapScopeItem, Locator: fmt.Sprintf("pst-nid:00112233445566778899aabbccddeeff/%d", nids[0]),
			Reason: "pst-message-unreadable", Class: state.ClassTerminal}, time.Now().Add(-48*time.Hour))
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		res, err := runLocal(t, out, path)
		if err != nil || res.Partial {
			t.Fatalf("run: err %v, partial %v", err, res.Partial)
		}
		if res.Gaps.Recovered != 0 || res.Gaps.ClosedGone != 1 {
			t.Errorf("recovered %d, closed-gone %d; want 0 and 1: a message at the same NID of another store is no recovery", res.Gaps.Recovered, res.Gaps.ClosedGone)
		}
		attests(t, out)
	})

	t.Run("a rebuilt store keeps one per-folder gap across rebuilds", func(t *testing.T) {
		dir, out := tmpDir(t), tmpDir(t)
		path := writeData(t, filepath.Join(dir, "_outlook-pst"), "Account.pst", corruptNode(t, data, nids[0]))
		res, err := runLocal(t, out, path)
		if !errors.Is(err, ErrPartial) {
			t.Fatalf("run 1: %v", err)
		}
		g := ledgerOf(t, out)
		if len(g) != 1 || g[0].Reason != "pst-messages-unreadable" || g[0].Scope != state.GapScopeFolder || g[0].Runs != 1 || g[0].Skipped != 1 {
			t.Fatalf("run 1 ledger %+v; want one per-folder pst-messages-unreadable aggregate, Runs 1", g)
		}
		if res.Gaps.FolderGaps != 1 || res.Gaps.ItemGaps != 0 {
			t.Errorf("summary %+v; want one folder gap and no item gap", res.Gaps)
		}
		for _, l := range readLines(t, res.ReportPath)[1:] {
			if !strings.Contains(l, "open it in Outlook") {
				t.Errorf("an -outlook scratch row must say to open the item in Outlook, never to repair the file: %q", l)
			}
		}

		// Outlook rebuilds the scratch file every run: another run, another day, the
		// damage on another NID. The entry is the same entry.
		ageLedger(t, out, 25*time.Hour)
		writeData(t, filepath.Join(dir, "_outlook-pst"), "Account.pst", corruptNode(t, data, nids[1]))
		if _, err := runLocal(t, out, path); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 2: %v", err)
		}
		g = ledgerOf(t, out)
		if len(g) != 1 || g[0].Runs != 2 || g[0].Skipped != 1 || !strings.Contains(g[0].Detail, fmt.Sprint(nids[1])) {
			t.Errorf("run 2 ledger %+v; want the same single entry with Runs 2, Skipped 1 and the new NID", g)
		}
		attests(t, out)
	})

	t.Run("a stripped Locator is restored on re-examine", func(t *testing.T) {
		dir, out := tmpDir(t), tmpDir(t)
		path := writeData(t, dir, "old.pst", breakAttachmentTable(t, data, nids[11]))
		res, err := runLocal(t, out, path)
		if !errors.Is(err, ErrPartial) || res.Stats.Unreadable != 1 {
			t.Fatalf("run 1: err %v, unreadable %d; want ErrPartial and one partly-read record", err, res.Stats.Unreadable)
		}
		m := loadArchive(t, out)
		var key string
		for k, r := range m.Entries {
			if r.Unreadable() {
				key = k
				if r.Locator == "" {
					t.Fatalf("the partly-read record carries no Locator: %+v", r)
				}
			}
		}
		rec, _ := m.Get(assure.Reached(t, key, "the partly-read record's key"))
		rec.Locator = "" // what an older binary's save does
		m.Add(key, rec)
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		if _, err := runLocal(t, out, path); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 2: %v", err)
		}
		if r, _ := loadArchive(t, out).Get(key); r.Locator == "" {
			t.Error("the Locator stayed empty after the item was re-examined")
		}
		attests(t, out)
	})
}

// covers: MA-324, R1, R17, S46
// The ledger is inert on every local source (P5): a manifest seeded with a gap whose
// locator matches a healthy item does not stop that item's capture — the item is
// archived, the gap closes as recovered — for a PST item (its pst-nid: locator), an
// mbox item and a maildir item (the ident: locator of an export that failed before).
func TestLedgerIsInertOnLocalSources(t *testing.T) {
	data := pstFixtureData(t)
	parent := tmpDir(t)
	mbox := p14Mbox(t, parent)
	maildir := filepath.Join(parent, "md", "Inbox")
	maildirMessages(t, maildir, 3)
	pstPath := writeData(t, parent, "old.pst", data)

	cases := []struct {
		name, path string
		locator    func(m *model.Message) string
		reason     string
	}{
		{"pst", pstPath, func(m *model.Message) string { return m.Locator }, "pst-message-unreadable"},
		{"mbox", mbox, func(m *model.Message) string { return "ident:" + m.Identity() }, "export-failed"},
		{"maildir", maildir, func(m *model.Message) string { return "ident:" + m.Identity() }, "export-failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			all := sourceMessages(t, c.path)
			target := all[len(all)/2]
			loc := assure.Reached(t, c.locator(target), "the seeded item's locator")
			folder := ""
			if c.name == "pst" {
				folder = "Drafts"
			}
			tok := tokenOf(t, c.path)
			out := tmpDir(t)
			m := loadArchive(t, out)
			m.Token(state.PathSourceID(c.path), tok)
			m.ObserveGap(state.CaptureGap{Token: tok, Folder: folder, Scope: state.GapScopeItem, Locator: loc, Reason: c.reason, Class: state.ClassFillable}, time.Now().Add(-72*time.Hour))
			if err := m.Save(); err != nil {
				t.Fatal(err)
			}
			res, err := runLocal(t, out, c.path)
			if err != nil || res.Partial {
				t.Fatalf("run with a seeded gap: err %v, partial %v; want nil, false", err, res.Partial)
			}
			if res.Stats.Exported != len(all) || loadArchive(t, out).Len() != len(all) {
				t.Errorf("exported %d, %d records; want all %d: the gap must not stop any capture", res.Stats.Exported, loadArchive(t, out).Len(), len(all))
			}
			if g := ledgerOf(t, out); len(g) != 0 || res.Gaps.Recovered != 1 {
				t.Errorf("gaps %+v, recovered %d; want the gap closed as recovered", g, res.Gaps.Recovered)
			}
		})
	}
}

// covers: MA-318, R1, R3, R7, R10, S46
// An Export panic is never log-only (F33): it increments Stats.ExportPanics and
// records a terminal export-panic gap at the message's locator, and the run goes on
// with the next message and ends partial.
func TestExportPanicIsALedgerGapNeverLogOnly(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 4)
	out := tmpDir(t)
	seen := 0
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(path string) error {
			if strings.HasSuffix(path, ".html") {
				if seen++; seen == 2 {
					panic("renderer fault")
				}
			}
			return nil
		}
	}
	t.Cleanup(func() { exporterSetup = nil })
	res, err := runLocal(t, out, src)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("run err = %v; want ErrPartial", err)
	}
	if res.Stats.ExportPanics != 1 || res.Stats.Exported != 3 {
		t.Errorf("panics %d, exported %d; want 1 and 3: the walk goes on", res.Stats.ExportPanics, res.Stats.Exported)
	}
	g := ledgerOf(t, out)
	if len(g) != 1 || g[0].Reason != "export-panic" || g[0].Class != state.ClassTerminal || !strings.HasPrefix(g[0].Locator, "ident:") || !strings.Contains(g[0].Detail, "renderer fault") {
		t.Errorf("ledger %+v; want one terminal export-panic gap at the message's ident: locator", g)
	}
}

// covers: MA-318, R1, R3, R5, R7, S46
// KEEP twin (P15), on a PST copy whose attachment table now fails: a -mode full run
// over a message archived complete leaves its page and zip byte-identical (equal to
// their recorded Fixity), the record unchanged, and records one info refresh-failed
// gap — and it is not a partial run, since nothing is missing from the archive.
func TestFullModeKeepsAnArchivedRecordWhenThePSTNowReadsWorse(t *testing.T) {
	data := pstFixtureData(t)
	nids := pstMessageNIDs(t, data)
	dir, out := tmpDir(t), tmpDir(t)
	path := writeData(t, dir, "old.pst", data)
	if _, err := runLocal(t, out, path); err != nil {
		t.Fatal(err)
	}
	m0 := loadArchive(t, out)
	before := messageFiles(t, out)
	zips := 0
	for p := range before {
		if strings.HasSuffix(p, "-attachments.zip") {
			zips++
		}
	}
	if zips != 1 {
		t.Fatalf("setup: %d attachment zips; the fixture has one message with attachments", zips)
	}

	writeData(t, dir, "old.pst", breakAttachmentTable(t, data, nids[11]))
	res, err := runLocalFull(t, out, path)
	if err != nil || res.Partial {
		t.Fatalf("full run over the worse read: err %v, partial %v; want nil, false (an info gap only)", err, res.Partial)
	}
	if res.Stats.Kept != 1 {
		t.Errorf("Kept = %d; want the one degraded capture held back", res.Stats.Kept)
	}
	after := messageFiles(t, out)
	if len(after) != len(before) {
		t.Errorf("%d message files after, %d before", len(after), len(before))
	}
	for p, sum := range before {
		if after[p] != sum {
			t.Errorf("%s changed: the archived page or zip was overwritten by a worse read", p)
		}
	}
	if rep, verr := Verify(out, VerifyOptions{}, nil, nil); verr != nil || !rep.Attested() {
		t.Errorf("verify: attested %v, err %v: the files must still equal their recorded Fixity", rep.Attested(), verr)
	}
	m1 := loadArchive(t, out)
	if m1.Len() != m0.Len() || m1.UnreadableCount() != 0 {
		t.Errorf("%d records (was %d), %d partly read; want the record unchanged", m1.Len(), m0.Len(), m1.UnreadableCount())
	}
	g := ledgerOf(t, out)
	if len(g) != 1 || g[0].Reason != "refresh-failed" || g[0].Class != state.ClassInfo {
		t.Errorf("ledger %+v; want one info refresh-failed gap", g)
	}
}

// covers: MA-318, R1, R3, R7, S46
// Healing (FC6): a partly-read PST item and its healthy read are ONE record in either
// order. Partial first, then healthy: the record is filled in place — same key, no #fp
// duplicate, its zip now written. Healthy first, then failing: the KEEP twin above (one
// record, nothing replaced).
func TestPartlyReadPSTItemHealsInPlaceWithoutADuplicate(t *testing.T) {
	data := pstFixtureData(t)
	nids := pstMessageNIDs(t, data)
	dir, out := tmpDir(t), tmpDir(t)
	path := writeData(t, dir, "old.pst", breakAttachmentTable(t, data, nids[11]))
	res, err := runLocal(t, out, path)
	if !errors.Is(err, ErrPartial) || res.Stats.Unreadable != 1 {
		t.Fatalf("partial first: err %v, unreadable %d", err, res.Stats.Unreadable)
	}
	m0 := loadArchive(t, out)
	if m0.Len() != 17 || m0.UnreadableCount() != 1 {
		t.Fatalf("partial first: %d records, %d partly read; want 17 and 1", m0.Len(), m0.UnreadableCount())
	}
	for p := range messageFiles(t, out) {
		if strings.HasSuffix(p, "-attachments.zip") {
			t.Fatalf("the partly-read message wrote a zip %s although its attachment table failed", p)
		}
	}

	writeData(t, dir, "old.pst", data)
	res, err = runLocal(t, out, path)
	if err != nil || res.Partial {
		t.Fatalf("healthy after partial: err %v, partial %v; want nil, false", err, res.Partial)
	}
	m1 := loadArchive(t, out)
	if m1.Len() != 17 || m1.UnreadableCount() != 0 || res.Stats.Filled != 1 {
		t.Errorf("%d records, %d partly read, filled %d; want 17, 0 and 1: healed in place, no #fp duplicate", m1.Len(), m1.UnreadableCount(), res.Stats.Filled)
	}
	for k := range m1.Entries {
		if strings.Count(k, "\x00") > 2 {
			t.Errorf("record %q carries a fingerprint qualifier: a duplicate of the partly-read one", k)
		}
	}
	zips := 0
	for p := range messageFiles(t, out) {
		if strings.HasSuffix(p, "-attachments.zip") {
			zips++
		}
	}
	if zips != 1 {
		t.Errorf("%d attachment zips after the heal; want the one it filled", zips)
	}
}

// covers: MA-318, R1, R3, R7, S46
// A partly-read record of an item with NO Message-ID (its identity folds the body and
// the attachment count, so the healthy read lands on a NEW key) is re-classed
// "superseded" with RecoveredBy, by metadata only: no file is written, rendered or
// deleted for it. The fixture gives every item a Message-ID, so the test clears it from
// what the reader yields; Locator and ReadGaps are the reader's own.
func TestHealthyReadSupersedesANoMessageIDPartialRecord(t *testing.T) {
	data := pstFixtureData(t)
	nids := pstMessageNIDs(t, data)
	dir := tmpDir(t)
	bad := writeData(t, dir, "bad.pst", breakAttachmentTable(t, data, nids[11]))
	good := writeData(t, dir, "good.pst", data)
	partial, whole := sourceMessages(t, bad)[11], sourceMessages(t, good)[11]
	if len(partial.ReadGaps) != 1 || partial.Locator == "" || partial.Locator != whole.Locator || len(whole.ReadGaps) != 0 {
		t.Fatalf("setup: partial gaps %v locator %q, whole locator %q", partial.ReadGaps, partial.Locator, whole.Locator)
	}
	partial.InternetMessageID, whole.InternetMessageID = "", ""
	if partial.Identity() == whole.Identity() {
		t.Fatal("setup: the two reads share an identity; the case needs the healthy read on a new key")
	}

	out := tmpDir(t)
	manifest := loadArchive(t, out)
	tok := manifest.Token("path:/x/old.pst", "old")
	e := &export.Exporter{OutDir: out, Manifest: manifest, Log: discard()}
	if _, err := e.Export(tok, []string{"Sent Messages"}, partial); err != nil {
		t.Fatal(err)
	}
	before := messageFiles(t, out)
	if _, err := e.Export(tok, []string{"Sent Messages"}, whole); err != nil {
		t.Fatal(err)
	}
	pKey, wKey := state.Key(tok, "Sent Messages", partial.Identity()), state.Key(tok, "Sent Messages", whole.Identity())
	pr, _ := manifest.Get(pKey)
	if pr.RecoveredBy != wKey || len(pr.Missing) != 0 || len(pr.Terminal) != 1 || pr.Terminal[0] != state.LabelSuperseded || pr.Locator != "" {
		t.Errorf("partial record %+v; want superseded, RecoveredBy %q", pr, wKey)
	}
	after := messageFiles(t, out)
	for p, sum := range before {
		if after[p] != sum {
			t.Errorf("superseding changed or deleted %s: it is metadata only", p)
		}
	}
	if len(after) != len(before)+2 { // the whole message's page and its zip
		t.Errorf("%d message files after, %d before; want the healthy read's page and zip only", len(after), len(before))
	}
}
