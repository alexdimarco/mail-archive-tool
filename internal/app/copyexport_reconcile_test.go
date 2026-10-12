package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
)

// killedIn runs a plan of n messages in e, killed at step, and returns where it was
// left; the environment's faults are cleared again.
func killedIn(t *testing.T, e *CopyExportEnv, src *fakeSrc, step string, msgs []CopyMessage) *CopyResult {
	t.Helper()
	e.faults = &copyFaults{killAt: step}
	defer func() { e.faults = nil }()
	res := mustRun(t, e, cePlan(t, e, src, msgs), src)
	if res.Killed != step {
		t.Fatalf("killed at %q, want %q: %s", res.Killed, step, res.Label)
	}
	return res
}

// covers: MA-429, R29, R5
// The identity tuple of an export root (design §4.9, RR2-C4): the run id the marker and
// README carry is the content nonce, and file identity decides only where it can. A
// folder with a matching nonce is the run's own, whatever its inode on a vfat or exfat
// stick that numbers inodes afresh, or after a remount that changed dev; a different run
// id, a link, a non-directory, a Windows identity that differs, or a new inode on the same
// mount of a stable-inode filesystem is a positive mismatch (moved or replaced). A folder
// with no nonce is confirmed by its file identity where that can decide, and by its
// creation time too on POSIX (a recreated folder at a freed inode is not it); where
// nothing can decide it is "couldn't confirm", never a mismatch.
func TestCopyExportIdentityTuple(t *testing.T) {
	ext := ceIdentity{FSType: "ext4", Dev: 10, Ino: 100, Birth: 5000}
	vfat := ceIdentity{FSType: "vfat", Dev: 20, Ino: 7, Birth: 0}
	win := ceIdentity{FSType: "NTFS", Vol: 0xABCD, FileID: 77, Birth: 9}
	seenOf := func(cur ceIdentity, mod func(*ceFolderSeen)) ceFolderSeen {
		s := ceFolderSeen{Exists: true, IsDir: true, Nonce: "RUN1", Cur: cur}
		if mod != nil {
			mod(&s)
		}
		return s
	}
	noNonce := func(s *ceFolderSeen) { s.Nonce = "" }
	for _, tc := range []struct {
		name string
		rec  ceIdentity
		seen ceFolderSeen
		want idVerdict
	}{
		{"nonce matches", ext, seenOf(ext, nil), idConfirmed},
		{"another run id", ext, seenOf(ext, func(s *ceFolderSeen) { s.Nonce = "RUN2" }), idMismatch},
		{"a link", ext, seenOf(ext, func(s *ceFolderSeen) { s.IsLink = true }), idMismatch},
		{"not a directory", ext, seenOf(ext, func(s *ceFolderSeen) { s.IsDir = false }), idMismatch},
		{"a new inode on the same mount of ext4", ext, seenOf(ceIdentity{FSType: "ext4", Dev: 10, Ino: 101, Birth: 5000}, nil), idMismatch},
		{"ext4 after a remount, dev changed, nonce intact", ext, seenOf(ceIdentity{FSType: "ext4", Dev: 11, Ino: 999, Birth: 5000}, nil), idConfirmed},
		{"a replugged vfat stick whose inodes changed, nonce intact", vfat, seenOf(ceIdentity{FSType: "vfat", Dev: 21, Ino: 4242}, nil), idConfirmed},
		{"vfat, the same dev, a new inode, nonce intact (fat inodes are not stable)", vfat, seenOf(ceIdentity{FSType: "vfat", Dev: 20, Ino: 4242}, nil), idConfirmed},
		{"windows identity matches", win, seenOf(win, nil), idConfirmed},
		{"windows volume differs, nonce intact", win, seenOf(ceIdentity{Vol: 0x1234, FileID: 77}, nil), idMismatch},
		{"no nonce, windows identity matches", win, seenOf(win, noNonce), idConfirmed},
		{"no nonce, windows file id differs", win, seenOf(ceIdentity{Vol: 0xABCD, FileID: 78}, noNonce), idMismatch},
		{"no nonce, same dev, inode and creation time", ext, seenOf(ext, noNonce), idConfirmed},
		{"no nonce, the inode was reused by a folder made again (creation time differs)", ext, seenOf(ceIdentity{FSType: "ext4", Dev: 10, Ino: 100, Birth: 6000}, noNonce), idMismatch},
		{"no nonce, a different inode on the same mount", ext, seenOf(ceIdentity{FSType: "ext4", Dev: 10, Ino: 101, Birth: 5000}, noNonce), idMismatch},
		{"no nonce, a filesystem that keeps no creation time", ceIdentity{FSType: "ext4", Dev: 10, Ino: 100}, seenOf(ceIdentity{FSType: "ext4", Dev: 10, Ino: 100}, noNonce), idUnconfirmed},
		{"no nonce, dev changed", ext, seenOf(ceIdentity{FSType: "ext4", Dev: 11, Ino: 100, Birth: 5000}, noNonce), idUnconfirmed},
		{"no nonce, vfat", vfat, seenOf(ceIdentity{FSType: "vfat", Dev: 20, Ino: 7}, noNonce), idUnconfirmed},
	} {
		if got, why := judgeIdentity("RUN1", tc.rec, tc.seen); got != tc.want {
			t.Errorf("%s: %v (%s), want %v", tc.name, got, why, tc.want)
		}
	}

	// The real read: a directory reads a non-zero identity, and a folder made by a run is
	// confirmed against its own record by its nonce.
	e, src := newCEEnv(t), newFakeSrc()
	res := killedIn(t, e, src, "W4", ceMsgs(1))
	sum := mustSummary(t, e, res.RunID)
	seen, err := probeFolder(res.Root)
	if err != nil || !seen.Exists || !seen.IsDir || seen.Nonce != res.RunID || !seen.HasMarker {
		t.Fatalf("probeFolder = %+v (%v)", seen, err)
	}
	assure.Reached(t, sum.Header.Identity.Ino+sum.Header.Identity.FileID, "the recorded identity")
	if v, why := judgeIdentity(res.RunID, sum.Header.Identity, seen); v != idConfirmed {
		t.Errorf("a run's own folder is %v (%s)", v, why)
	}
}

// covers: MA-429, MA-448, R29, R5
// Reconciliation reads the record, the journal and a bounded probe of the folder, and only
// the marker-and-seal outcomes are terminal: a folder not found, one that was replaced by
// another folder, and one moved away are observations, cached by the caller and never
// journaled, probed again at the next call; once the folder is back its outcome is
// journaled once and is not probed again.
func TestCopyExportReconcileObservationsAreNotTerminal(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	res := killedIn(t, e, src, "W4", ceMsgs(2))
	moved := res.Root + ".away"
	reconciles := func() int {
		n := 0
		lines, _ := e.readJournal()
		for _, l := range lines {
			if l.Run == res.RunID && l.Event == "reconciled" {
				n++
			}
		}
		return n
	}

	os.Rename(res.Root, moved)
	obs, err := e.ReconcileRun(res.RunID)
	if err != nil || obs.Kind != obsNotFound || obs.Terminal || !contains(obs.Actions, ActLookAgain) || !contains(obs.Actions, ActGone) || !contains(obs.Actions, ActLocate) {
		t.Fatalf("a folder that is not there: %+v (%v)", obs, err)
	}
	if !strings.HasPrefix(obs.Label, "folder not found (at ") {
		t.Errorf("label = %q", obs.Label)
	}

	os.Mkdir(res.Root, 0o700) // another folder at the path
	obs, _ = e.ReconcileRun(res.RunID)
	if obs.Kind != obsMoved || obs.Terminal || !contains(obs.Actions, ActLocate) || !contains(obs.Actions, ActGone) {
		t.Fatalf("a different folder at the path: %+v", obs)
	}
	os.Remove(res.Root)
	os.Symlink(moved, res.Root) // a link
	obs, _ = e.ReconcileRun(res.RunID)
	if obs.Kind != obsMoved || obs.Terminal {
		t.Fatalf("a link at the path: %+v", obs)
	}
	os.Remove(res.Root)
	if reconciles() != 0 {
		t.Fatalf("an observation was journaled: %d reconciled lines", reconciles())
	}

	// The folder is back, as when the right stick is replugged: found, terminal, journaled once.
	os.Rename(moved, res.Root)
	obs, _ = e.ReconcileRun(res.RunID)
	if obs.Kind != obsInterrupt || !obs.Terminal || !contains(obs.Actions, ActFinishAsPartial) || reconciles() != 1 {
		t.Fatalf("the folder put back: %+v, %d reconciled lines", obs, reconciles())
	}
	// Terminal means not probed again: the folder can go and the answer stays.
	os.Rename(res.Root, moved)
	again, _ := e.ReconcileRun(res.RunID)
	if again.Kind != obsInterrupt || !again.Terminal || reconciles() != 1 {
		t.Errorf("a terminal outcome was probed again: %+v, %d lines", again, reconciles())
	}
}

// covers: MA-429, MA-430, MA-448, R29, R5
// Locate accepts a moved folder only when the identity check passes, and appends its path
// to the run record, after which the run is reconciled and finished at that path; a
// different export's folder (another run id) and an empty folder are refused with nothing
// recorded. "This folder is gone" is the user's terminal answer: a `gone` line in the run
// record (so it outlives a lost journal), a `gone` line in the ledger for a run that never
// sealed, and a journal record; the run is never probed again, and its remaining messages
// are the part's whole plan minus what other sealed parts exported.
func TestCopyExportLocateAndThisFolderIsGone(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	res := killedIn(t, e, src, "W4", ceMsgs(3))
	other := mustRun(t, e, cePlan(t, e, src, ceMsgs(1)), src) // a different export

	newPath := filepath.Join(filepath.Dir(res.Root), "moved-here")
	os.Rename(res.Root, newPath)
	assure.Refused(t, 1, errString(e.Locate(res.RunID, other.Root)), assure.Code(1), assure.Names("moved or replaced", "Locate"),
		assure.NoSideEffect(func() bool { return len(mustSummary(t, e, res.RunID).Located) == 0 }))
	emptyDir := filepath.Join(t.TempDir(), "empty")
	os.Mkdir(emptyDir, 0o700)
	if err := e.Locate(res.RunID, emptyDir); err == nil || len(mustSummary(t, e, res.RunID).Located) != 0 {
		t.Fatalf("an empty folder was accepted: %v", err)
	}
	if err := e.Locate(res.RunID, "relative/path"); err == nil {
		t.Fatal("a relative path was accepted")
	}
	if err := e.Locate(res.RunID, newPath); err != nil {
		t.Fatalf("the right folder at a new path: %v", err)
	}
	obs, _ := e.ReconcileRun(res.RunID)
	if obs.Kind != obsInterrupt {
		t.Fatalf("after Locate: %+v", obs)
	}
	fin, err := e.FinishAsPartial(res.RunID)
	if err != nil || fin.Root != newPath || !exists(newPath, "README.txt") {
		t.Fatalf("Finish as partial at the located path: %+v (%v)", fin, err)
	}

	// This folder is gone, for a run that never sealed.
	res2 := killedIn(t, e, src, "W4", ceMsgs(3))
	g, err := e.MarkGone(res2.RunID)
	if err != nil || g.Kind != obsGone || !g.Terminal || !contains(g.Actions, ActExportThese) {
		t.Fatalf("MarkGone: %+v (%v)", g, err)
	}
	if !mustSummary(t, e, res2.RunID).Gone || !e.terminalGone(res2.RunID) {
		t.Error("the answer is not in the run record and the journal")
	}
	ledger, _ := e.readLedger()
	var gone int
	for _, l := range ledger {
		if l.Run == res2.RunID && l.Kind == "gone" && l.SetState == "" {
			gone++
		}
	}
	if gone != 1 {
		t.Errorf("the ledger has %d gone lines for the run, want 1 (without a set state)", gone)
	}
	// Never probed again, though the folder is there.
	if obs, _ := e.ReconcileRun(res2.RunID); obs.Kind != obsGone {
		t.Errorf("a gone run reconciled as %+v", obs)
	}
	// Its lost journal does not lose the answer: the launch sweep re-adopts it as gone.
	os.Remove(e.journalPath())
	n := relaunch(e)
	adopted, err := n.SweepRunRecords()
	if err != nil || !contains(adopted, res2.RunID) || !n.terminalGone(res2.RunID) {
		t.Errorf("the sweep adopted %v (%v), gone terminal %v", adopted, err, n.terminalGone(res2.RunID))
	}
	// The remaining messages of a gone part: its whole plan, because no other part exported them.
	if rest, _ := n.RunRemaining(res2.RunID); len(rest) != 3 {
		t.Errorf("the gone part's remaining messages are %d, want its 3", len(rest))
	}
}

// terminalGone reports whether the journal records run as gone.
func (e *CopyExportEnv) terminalGone(run string) bool {
	j := e.terminalReconciled(run)
	return j != nil && j.Status == obsGone
}

// covers: MA-429, MA-430, MA-448, R29, R5
// The launch sweep (RR2-C12): a lost journal does not hide a run whose record survives:
// the sweep adopts every record no journal line names, rebuilding its start line, appends
// every ledger line that is missing, and deletes a record only when its run is settled and
// its chain complete. A sealed run without its end keeps its record until reconcile sees its
// marker absent; a partial run keeps it while "Export failed messages again" is offered,
// and the record goes once the retry part has exported what failed.
func TestCopyExportLaunchSweepAdoptsAndDeletes(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	a := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)                                            // complete
	b := killedIn(t, e, src, "W9a", ceMsgs(2))                                                       // sealed, no ledger line, marker present
	c := killedIn(t, e, src, "W4", ceMsgs(2))                                                        // interrupted
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) { // a partial run
		if m.SourceID == "M00002" {
			return 0, &graph.StatusError{Code: 400, GraphCode: "ErrorMimeContentConversionFailed", Text: "x"}
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	d := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	src.fetchFn = nil
	if d.Outcome != CopyOutcomePartial {
		t.Fatalf("the partial run: %s", d.Label)
	}
	os.Remove(e.journalPath()) // the journal is lost
	os.Remove(e.ledgerPath())  // and so is the ledger

	n := relaunch(e)
	adopted, err := n.SweepRunRecords()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*CopyResult{a, b, c, d} {
		if !contains(adopted, r.RunID) {
			t.Errorf("run %s was not adopted: %v", r.RunID, adopted)
		}
		if !n.journalHas(r.RunID, "start") {
			t.Errorf("run %s has no rebuilt start line", r.RunID)
		}
	}
	for _, r := range []*CopyResult{a, b, d} {
		if !n.ledgerOf(r.RunID) {
			t.Errorf("sealed run %s has no ledger line after the sweep", r.RunID)
		}
	}
	if n.ledgerOf(c.RunID) {
		t.Errorf("an unsealed run has a ledger line without an answer")
	}
	// Nothing is deleted yet: no run has its end, so none is settled.
	for _, r := range []*CopyResult{a, b, c, d} {
		if _, err := os.Stat(n.recordPath(r.RunID)); err != nil {
			t.Errorf("run %s's record was deleted before it was settled: %v", r.RunID, err)
		}
	}
	// Reconcile A and D (their markers are gone): each gets its end.
	for _, r := range []*CopyResult{a, d} {
		if obs, _ := n.ReconcileRun(r.RunID); obs.Kind != obsRecovered {
			t.Fatalf("run %s reconciled as %+v", r.RunID, obs)
		}
	}
	if _, err := n.SweepRunRecords(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(n.recordPath(a.RunID)); err == nil {
		t.Error("A is settled and its chain complete, but its record was kept")
	}
	for name, r := range map[string]*CopyResult{"B (sealed, marker owed)": b, "C (interrupted)": c, "D (its card offers Export failed messages again)": d} {
		if _, err := os.Stat(n.recordPath(r.RunID)); err != nil {
			t.Errorf("%s: the record was deleted: %v", name, err)
		}
	}
	// The retry part exports what failed: both settle and both records go.
	p2 := cePlan(t, n, src, idMsgs("M00002"))
	p2.Chain = CopyChain{ID: d.RunID, RetryOf: d.RunID, RootName: filepath.Base(d.Root) + "-part2",
		Parts: []CopyChainRef{{Part: 1, RunID: d.RunID, Root: filepath.Base(d.Root)}}}
	p2.ExportName = p2.Chain.RootName
	r2 := mustRun(t, n, p2, src)
	if r2.Status != CopyStatusComplete {
		t.Fatalf("the retry part: %s", r2.Label)
	}
	n.SweepRunRecords()
	for name, r := range map[string]*CopyResult{"D": d, "its retry part": r2} {
		if _, err := os.Stat(n.recordPath(r.RunID)); err == nil {
			t.Errorf("%s: settled, chain complete, but its record was kept", name)
		}
	}
}

// covers: MA-429, MA-430, R29, R5
// A refused start (R-d): when the journal's start line cannot be written the run is refused
// naming the state directory and leaving the empty export folder in place; a `refused`
// ledger line is appended and then the run record is removed, so the launch sweep never
// adopts it; if the ledger append fails too, the record is kept and the next launch adopts
// it as an interrupted empty export folder, which Finish as partial then settles.
func TestCopyExportRefusedStartLeavesNoAdoptableRecord(t *testing.T) {
	for _, ledgerFails := range []bool{false, true} {
		e, src := newCEEnv(t), newFakeSrc()
		e.faults = &copyFaults{appendErr: func(path string) error {
			switch {
			case strings.HasSuffix(path, "desktop-exports.jsonl"):
				return syscall.ENOSPC
			case ledgerFails && strings.HasSuffix(path, "export-fingerprints.jsonl"):
				return syscall.ENOSPC
			}
			return nil
		}}
		p := cePlan(t, e, src, ceMsgs(2))
		_, err := ceRunPlan(t, e, p, src)
		var ref *CopyRefusal
		if !errors.As(err, &ref) || ref.ID != "R-d" || ref.Root == "" {
			t.Fatalf("ledgerFails=%v: a journal that cannot be written did not refuse with R-d (%v)", ledgerFails, err)
		}
		assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(e.StateDir, "nothing was exported", "Start export again"))
		if !exists(ref.Root, ".") {
			t.Error("the empty export folder was not left in place")
		}
		e.faults = nil
		n := relaunch(e)
		adopted, _ := n.SweepRunRecords()
		if !ledgerFails {
			if len(adopted) != 0 {
				t.Errorf("a refused start was adopted: %v", adopted)
			}
			if l, _ := n.readLedger(); len(l) != 1 || l[0].Kind != "refused" {
				t.Errorf("the ledger = %+v, want one refused line", l)
			}
			continue
		}
		if len(adopted) != 1 {
			t.Fatalf("the kept record was not adopted: %v", adopted)
		}
		obs, _ := n.ReconcileRun(adopted[0])
		if obs.Kind != obsEmpty && obs.Kind != obsUnconfirm {
			t.Errorf("the adopted record reconciles as %+v, want an interrupted empty export folder", obs)
		}
		if obs.Kind == obsEmpty {
			if fin, err := n.FinishAsPartial(adopted[0]); err != nil || fin.Counts.Exported != 0 || fin.Status != CopyStatusFinishedAsPartial {
				t.Errorf("Finish as partial on the empty folder: %+v (%v)", fin, err)
			}
		}
	}
}

// covers: MA-429, MA-430, R29, R5
// Journal compaction (W12): once the journal passes its bound it keeps the last runs and
// every unresolved run whatever its age, and copies lines it cannot read or whose v it does
// not know verbatim; a dropped run's record is deleted only after the new journal is in place
// and only if the run is settled and its chain complete, and a torn tail is truncated before
// the next append.
func TestCopyExportCompactionKeepsTheUnresolved(t *testing.T) {
	defer func(a, b int) { copyJournalCompactAt, copyKeepRuns = a, b }(copyJournalCompactAt, copyKeepRuns)
	copyJournalCompactAt, copyKeepRuns = 10, 3
	e, src := newCEEnv(t), newFakeSrc()
	old := killedIn(t, e, src, "W4", ceMsgs(1)) // unresolved, and the oldest
	var done []*CopyResult
	for i := 0; i < 6; i++ {
		r := mustRun(t, e, cePlan(t, e, src, ceMsgs(1)), src)
		done = append(done, r)
	}
	// A torn tail, an unknown version and a line that is not JSON.
	f, _ := os.OpenFile(e.journalPath(), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"v":2,"run":"newer","event":"start","future":true}` + "\n" + "not json at all\n" + `{"v":1,"run":"torn"`)
	f.Close()
	if err := e.journalAppend(ceJournalLine{Run: done[5].RunID, Event: "reconciled", Status: "x"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(e.journalPath())
	if strings.Contains(string(raw), `"run":"torn"`) {
		t.Error("the torn tail was not truncated before the next append")
	}
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if l != "not json at all" && !json.Valid([]byte(l)) {
			t.Errorf("a line that is not whole: %q", l)
		}
	}

	var atW12 []string
	e.faults = &copyFaults{hook: func(step string) {
		if step == "W12" {
			for _, r := range done {
				if _, err := os.Stat(e.recordPath(r.RunID)); err == nil {
					atW12 = append(atW12, r.RunID)
				}
			}
		}
	}}
	if err := e.CompactJournal(); err != nil {
		t.Fatal(err)
	}
	if len(atW12) != len(done) {
		t.Errorf("at W12, when the new journal is in place, %d of %d records existed: none may be deleted before it", len(atW12), len(done))
	}
	kept := map[string]bool{}
	lines, _ := e.readJournal()
	for _, l := range lines {
		kept[l.Run] = true
	}
	if !kept[old.RunID] {
		t.Error("an unresolved run's lines were dropped by compaction")
	}
	for _, r := range done[:3] { // the oldest settled runs are dropped; their records go
		if kept[r.RunID] {
			t.Errorf("run %s should have been dropped", r.RunID)
		}
		if _, err := os.Stat(e.recordPath(r.RunID)); err == nil {
			t.Errorf("run %s's record was kept after its lines were dropped", r.RunID)
		}
	}
	for _, r := range done[3:] {
		if !kept[r.RunID] {
			t.Errorf("run %s is among the last three but was dropped", r.RunID)
		}
	}
	after, _ := os.ReadFile(e.journalPath())
	if !strings.Contains(string(after), `"v":2,"run":"newer"`) || !strings.Contains(string(after), "not json at all") {
		t.Error("lines of an unknown version or that are not JSON were not copied verbatim")
	}
	if _, err := os.Stat(e.recordPath(old.RunID)); err != nil {
		t.Errorf("the unresolved run's record was deleted: %v", err)
	}
}

// covers: MA-429, MA-430, R29, R5
// A run record made by a newer MailArchive Desktop is refused legibly and never touched:
// reconcile says to update, Finish as partial refuses, and the launch sweep neither adopts
// nor deletes it; a record's torn tail is truncated before the next append.
func TestCopyExportARecordOfANewerFormatIsLeftAlone(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	res := killedIn(t, e, src, "W4", ceMsgs(2))
	path := e.recordPath(res.RunID)
	raw, _ := os.ReadFile(path)
	newer := strings.Replace(string(raw), `"format_version":1`, `"format_version":99`, 1)
	os.WriteFile(path, []byte(newer), 0o600)
	os.Remove(e.journalPath())
	n := relaunch(e)
	if _, err := readSummary(path); !errors.Is(err, errCopyNewerFormat) {
		t.Fatalf("readSummary = %v", err)
	}
	obs, err := n.ReconcileRun(res.RunID)
	if err != nil || obs.Kind != obsNewer || !strings.Contains(obs.Label, "update to retry") {
		t.Errorf("reconcile = %+v (%v)", obs, err)
	}
	if _, err := n.FinishAsPartial(res.RunID); !errors.Is(err, errCopyNewerFormat) {
		t.Errorf("Finish as partial = %v", err)
	}
	if adopted, _ := n.SweepRunRecords(); len(adopted) != 0 {
		t.Errorf("the sweep adopted a record it cannot read: %v", adopted)
	}
	if b, _ := os.ReadFile(path); string(b) != newer {
		t.Error("a record of a newer format was changed")
	}

	// A torn tail on a readable record is cut before the next append.
	os.WriteFile(path, raw, 0o600)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"t":"msg","n":1,"stat`)
	f.Close()
	rec, err := n.openRecord(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.append(ceGoneLine{T: "gone"}); err != nil {
		t.Fatal(err)
	}
	rec.Close()
	if s, err := readSummary(path); err != nil || !s.Gone || s.Progress != 0 {
		t.Errorf("after the torn tail: %+v (%v)", s, err)
	}
}

// covers: MA-429, R29, S52
// The plan is a dry run that binds the run: it refuses an empty selection (R-h), a
// destination that is not an existing absolute folder (R-e), a selection past 200,000
// (R-i) and the options the build does not have (extracted attachments, until slice E6);
// it names the first ten target paths
// and writes nothing, to the destination and to the state directory alike; and it reports
// the free space, the reserve, the record bound and a cloud-sync warning.
func TestCopyExportPlanIsADryRun(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	dest := filepath.Join(t.TempDir(), "OneDrive - Contoso", "dest")
	os.MkdirAll(dest, 0o755)
	destBefore, stateBefore := treeFiles(t, filepath.Dir(dest)), treeFiles(t, filepath.Dir(e.StateDir))
	in := CopyPlanInput{Source: src.Describe(), Messages: ceMsgs(25), Dest: dest, Options: CopyOptions{PreserveFolders: true}}
	p, err := e.PlanCopyExport(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.SamplePaths) != 10 || p.LongestPath == 0 || p.FreeBytes == 0 || p.Reserve <= 0 || p.RecordBound <= 0 || p.SelectionHash == "" || p.ID == "" || p.DestIdentity.Ino == 0 && p.DestIdentity.FileID == 0 {
		t.Errorf("plan = %+v", p)
	}
	if !strings.HasPrefix(p.ExportName, "MailExport-") {
		t.Errorf("export name = %q", p.ExportName)
	}
	found := false
	for _, w := range p.Warnings {
		found = found || strings.Contains(w, "OneDrive")
	}
	if !found {
		t.Errorf("no cloud-sync warning naming OneDrive: %v", p.Warnings)
	}
	if got := treeFiles(t, filepath.Dir(dest)); len(got) != len(destBefore) {
		t.Errorf("the plan wrote to the destination: %v", got)
	}
	if _, err := os.Stat(e.StateDir); err == nil {
		if got := treeFiles(t, filepath.Dir(e.StateDir)); len(got) != len(stateBefore) {
			t.Errorf("the plan wrote to the state directory: %v", got)
		}
	}
	for name, tc := range map[string]struct {
		mod func(*CopyPlanInput)
		id  string
	}{
		"empty":       {func(i *CopyPlanInput) { i.Messages = nil }, "R-h"},
		"missing":     {func(i *CopyPlanInput) { i.Dest = filepath.Join(dest, "nope") }, "R-e"},
		"relative":    {func(i *CopyPlanInput) { i.Dest = "dest" }, "R-e"},
		"attachments": {func(i *CopyPlanInput) { i.Options.Attachments = true }, "R-h"},
	} {
		bad := in
		tc.mod(&bad)
		_, err := e.PlanCopyExport(bad)
		var ref *CopyRefusal
		if !errors.As(err, &ref) || ref.ID != tc.id {
			t.Errorf("%s: %v, want %s", name, err, tc.id)
		}
	}
	// Readable HTML is built (slice E5): the plan carries it, sized for the copies
	withHTML := in
	withHTML.Options.ReadableHTML = true
	ph, err := e.PlanCopyExport(withHTML)
	if err != nil || !ph.Options.ReadableHTML || ph.Reserve <= p.Reserve || ph.EstimateBytes != 2*p.EstimateBytes {
		t.Errorf("a plan with Readable HTML: %v, %+v", err, ph)
	}
}
