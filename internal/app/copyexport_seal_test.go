package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
)

// relaunch is a new process on the same state directory: a fresh environment, with
// no fault hooks and no memory of the run (the launch sweep and reconcile read the
// record, the journal and the ledger).
func relaunch(e *CopyExportEnv) *CopyExportEnv {
	n := NewCopyExportEnv(e.StateDir, e.Host, e.Version)
	n.syncFile = e.syncFile
	return n
}

// markerLog records the removals made through the contained writer.
type markerLog struct {
	ceFS
	mu      sync.Mutex
	removed []string
}

func (m *markerLog) Remove(rel string) error {
	m.mu.Lock()
	m.removed = append(m.removed, rel)
	m.mu.Unlock()
	return m.ceFS.Remove(rel)
}

func (m *markerLog) removedMarker() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.removed {
		if r == copyMarkerName {
			return true
		}
	}
	return false
}

// covers: MA-428, R29, R5
// Cancel stops after the message in flight, aborting its download and removing its temp
// file: the marker is rewritten CANCELLED with the counts by one atomic replacing rename,
// never removed; the manifest says cancelled and lists the exported messages and the
// not-attempted ids; the sums cover what was written; no partial file is under a final
// name; and the sealed line records the cancel (no stop reason). The positive twin: a
// complete run's marker is removed, which the same recorder sees.
func TestCopyExportCancelStopsAfterTheMessageInFlight(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	log := &markerLog{}
	e.fsWrap = func(f ceFS) ceFS { log.ceFS = f; return log }
	if res := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src); res.Status != CopyStatusComplete || !log.removedMarker() {
		t.Fatalf("the positive twin: %s, marker removed %v", res.Label, log.removedMarker())
	}

	log = &markerLog{}
	started := make(chan struct{})
	var once sync.Once
	src = newFakeSrc()
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		if m.SourceID == "M00003" {
			w.Write([]byte("partial body"))
			once.Do(func() { close(started) })
			<-ctx.Done()
			return 0, ctx.Err()
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	p := cePlan(t, e, src, ceMsgs(6))
	run := e.NewCopyRun(p, src)
	done := make(chan *CopyResult, 1)
	go func() {
		res, err := run.Run(context.Background())
		if err != nil {
			t.Errorf("the run was refused: %v", err)
		}
		done <- res
	}()
	<-started
	run.Cancel()
	res := <-done
	if res.Status != CopyStatusCancelled || res.Outcome != CopyOutcomePartial || res.Label != "Cancelled" || res.Stop != nil {
		t.Fatalf("a cancel ended as %s / %s (outcome %d, stop %+v)", res.Status, res.Label, res.Outcome, res.Stop)
	}
	if res.Counts.Exported != 2 || res.Counts.NotAttempted != 4 || res.Counts.Failed != 0 {
		t.Errorf("counts = %+v, want 2 exported, 4 not attempted, none failed", res.Counts)
	}
	marker := readText(t, res.Root, copyMarkerName)
	if !strings.Contains(marker, "CANCELLED") || !strings.Contains(marker, "2 of 6 exported") || !strings.HasPrefix(marker, copyRunLinePrefix+res.RunID) {
		t.Errorf("the marker is %q: it must say CANCELLED with the counts and keep the run id", marker)
	}
	if log.removedMarker() {
		t.Error("the cancelled run's marker was removed before it was rewritten")
	}
	man := manifestOf(t, res.Root)
	if man["run"].(map[string]any)["status"] != "cancelled" {
		t.Errorf("the manifest says %v", man["run"].(map[string]any)["status"])
	}
	na := assure.Reached(t, man["not_attempted"].([]any), "not_attempted ids")
	if len(na) != 4 || na[0] != "M00003" || na[3] != "M00006" {
		t.Errorf("not_attempted = %v, want M00003..M00006", na)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("the sums of a cancelled run do not cover what was written: %v %v %v", bad, missing, extra)
	}
	if r := tempResidue(t, res.Root); len(r) != 0 {
		t.Errorf("the in-flight message's temp was left: %v", r)
	}
	if s := mustSummary(t, e, res.RunID).Sealed; s == nil || s.Status != CopyStatusCancelled || s.Stop != nil {
		t.Errorf("the sealed line = %+v, want the cancel", s)
	}
}

// covers: MA-440, R29, R12
// The outcome partition (extract's X1): complete 0; partial and cancelled 3; a
// run-fatal stop and a refusal 1; and the labels the page shows ("Complete", "Finished
// with problems — N messages not exported", "Cancelled", "Stopped: <reason>").
func TestCopyExportOutcomeMapping(t *testing.T) {
	for _, tc := range []struct {
		status  CopyStatus
		outcome CopyOutcome
	}{{CopyStatusComplete, 0}, {CopyStatusPartial, 3}, {CopyStatusCancelled, 3}, {CopyStatusFailed, 1}, {CopyStatusFinishedAsPartial, 1}} {
		if tc.status.Outcome() != tc.outcome {
			t.Errorf("%s maps to %d, want %d", tc.status, tc.status.Outcome(), tc.outcome)
		}
	}
	if (&CopyRefusal{}).ExitCode() != 1 {
		t.Error("a refusal does not exit 1")
	}

	e := newCEEnv(t)
	// complete
	src := newFakeSrc()
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	if res.Outcome != 0 || res.Label != "Complete" {
		t.Errorf("complete: %d %q", res.Outcome, res.Label)
	}
	// partial: two messages fail with a terminal 404 and a retryable conversion error
	src = newFakeSrc()
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		switch m.SourceID {
		case "M00002":
			return 0, &graph.StatusError{Code: 404, GraphCode: "ErrorItemNotFound", Text: "gone"}
		case "M00004":
			return 0, &graph.StatusError{Code: 400, GraphCode: "ErrorMimeContentConversionFailed", Text: "x"}
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	res = mustRun(t, e, cePlan(t, e, src, ceMsgs(5)), src)
	if res.Outcome != 3 || res.Label != "Finished with problems — 2 messages not exported" || res.Counts.Failed != 2 {
		t.Errorf("partial: %d %q %+v", res.Outcome, res.Label, res.Counts)
	}
	// failed: a run-fatal stop
	src = newFakeSrc()
	src.signedIn = func() (string, bool) { return "bob@contoso.org", true }
	res = mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	if res.Outcome != 1 || res.Label != "Stopped: the Microsoft 365 sign-in was cleared or changed" {
		t.Errorf("failed: %d %q", res.Outcome, res.Label)
	}
	// refused: a destination that cannot hold the export
	src = newFakeSrc()
	p := cePlan(t, e, src, ceMsgs(3))
	e.freeDest = func(string) (uint64, error) { return 10, nil }
	_, err := ceRunPlan(t, e, p, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) || ref.ID != "R-g2" {
		t.Fatalf("a destination with no room did not refuse with R-g2: %v", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names("nothing was written", "Free some space or choose another destination"))
	// the fingerprint finishing label
	if l := copyLabel(CopyStatusFinishedAsPartial, CopyCounts{}, &CopyStop{RID: "R-g"}); l != "Stopped: the destination is full or no longer available (finished as partial)" {
		t.Errorf("finished as partial: %q", l)
	}
	if l := copyLabel(CopyStatusFinishedAsPartial, CopyCounts{}, nil); l != "Stopped: the app stopped while this export was running (finished as partial)" {
		t.Errorf("finished as partial with no recorded reason: %q", l)
	}
}

// killedRun runs a plan of n messages, killed at step, in a fresh environment; the
// result says where it was killed, and the plan's root is where it left it.
func killedRun(t *testing.T, step string, n int) (*CopyExportEnv, *CopyResult, *CopyPlan) {
	t.Helper()
	e, src := newCEEnv(t), newFakeSrc()
	e.faults = &copyFaults{killAt: step}
	p := cePlan(t, e, src, ceMsgs(n))
	res := mustRun(t, e, p, src)
	if res.Killed != step {
		t.Fatalf("the run was killed at %q, want %q (it ended as %s)", res.Killed, step, res.Label)
	}
	return e, res, p
}

// covers: MA-429, R29, R5
// Crash ordering (it proves the order, not power-loss durability): a fault hook kills
// the run at each of W2 through W10, including W9a and W9b. At every point a directory
// that holds a message file carries the marker or is complete; a W2-W4 kill leaves an
// empty export folder; README, from W7 on, says its status is valid only while the
// marker is absent; the label a new launch reconciles to is never "complete" while the
// marker is present (W9a: "Finished writing"); temps carry the .mailexport- prefix; and
// the action each label offers settles the folder: Finish as partial for an
// interrupted one, Check and finish for one that finished writing.
func TestCopyExportCrashOrdering(t *testing.T) {
	type want struct {
		kind     string
		hasFile  bool
		marker   bool
		readme   bool
		complete bool // the folder is complete: sealed, sums verify, marker gone
	}
	table := map[string]want{
		"W2":   {kind: obsNoRecord},
		"W3":   {kind: obsEmpty},
		"W4":   {kind: obsInterrupt, marker: true},
		"W5:1": {kind: obsInterrupt, hasFile: true, marker: true},
		"W6:1": {kind: obsInterrupt, hasFile: true, marker: true},
		"W7":   {kind: obsInterrupt, hasFile: true, marker: true, readme: true},
		"W8":   {kind: obsInterrupt, hasFile: true, marker: true, readme: true},
		"W9":   {kind: obsInterrupt, hasFile: true, marker: true, readme: true},
		"W9a":  {kind: obsFinishing, hasFile: true, marker: true, readme: true},
		"W9b":  {kind: obsFinishing, hasFile: true, marker: true, readme: true},
		"W10":  {kind: obsRecovered, hasFile: true, readme: true, complete: true},
	}
	for _, step := range []string{"W2", "W3", "W4", "W5:1", "W6:1", "W7", "W8", "W9", "W9a", "W9b", "W10"} {
		w := table[step]
		e, res, p := killedRun(t, step, 3)
		root := res.Root
		if !w.hasFile {
			if files := treeFiles(t, root); len(files) != 0 && !(w.marker && len(files) == 1) {
				t.Errorf("%s: an early kill left %v", step, keysOfFiles(files))
			}
		}
		hasMarker := exists(root, copyMarkerName)
		if hasMarker != w.marker {
			t.Errorf("%s: marker present = %v, want %v", step, hasMarker, w.marker)
		}
		var msgFile bool
		for f := range treeFiles(t, root) {
			if strings.HasPrefix(f, "messages/") && !strings.Contains(f, copyTempPrefix) {
				msgFile = true
			}
		}
		if msgFile != w.hasFile {
			t.Errorf("%s: a message file present = %v, want %v", step, msgFile, w.hasFile)
		}
		// P14: a directory that holds a message file carries the marker or is complete.
		if msgFile && !hasMarker {
			if bad, missing, extra := sumsCheck(t, root); len(bad)+len(missing)+len(extra) != 0 || !w.complete {
				t.Errorf("%s: a folder with message files has no marker and is not complete (%v %v %v)", step, bad, missing, extra)
			}
		}
		if w.readme {
			if r := readText(t, root, copyReadmeName); !strings.Contains(r, "valid only while no "+copyMarkerName) {
				t.Errorf("%s: README does not say its status is conditional on the marker's absence", step)
			}
		}
		if w.hasFile && exists(root, "export-manifest.json") {
			if man := manifestOf(t, root); man["run"].(map[string]any)["valid_if_absent"] != copyMarkerName {
				t.Errorf("%s: the manifest does not say its status is valid only without the marker", step)
			}
		}
		for _, tmp := range tempResidue(t, root) {
			if !strings.Contains(tmp, copyTempPrefix) {
				t.Errorf("%s: a temp without the prefix: %s", step, tmp)
			}
		}

		// The next launch.
		n := relaunch(e)
		if _, err := n.SweepRunRecords(); err != nil {
			t.Fatalf("%s: the launch sweep: %v", step, err)
		}
		obs, err := n.ReconcileRun(res.RunID)
		if err != nil {
			t.Fatalf("%s: reconcile: %v", step, err)
		}
		kind := obs.Kind
		if step == "W3" && kind == obsUnconfirm && mustSummary(t, e, res.RunID).Header.Identity.Birth == 0 {
			kind = obsEmpty // a filesystem that keeps no creation time cannot confirm an empty folder (RR2b-N1)
		}
		if kind != w.kind {
			t.Errorf("%s: reconciled to %q (%s), want %q", step, obs.Kind, obs.Label, w.kind)
		}
		if hasMarker && strings.HasPrefix(obs.Label, "Complete") {
			t.Errorf("%s: labelled %q while the marker is present", step, obs.Label)
		}
		if step == "W9a" && !strings.HasPrefix(obs.Label, "Finished writing (Complete)") {
			t.Errorf("W9a: the label is %q, want \"Finished writing (Complete)…\"", obs.Label)
		}
		_ = p

		// The offered action settles it.
		switch {
		case contains(obs.Actions, ActFinishAsPartial):
			fin, err := n.FinishAsPartial(res.RunID)
			if err != nil {
				t.Fatalf("%s: Finish as partial: %v", step, err)
			}
			if fin.Status != CopyStatusFinishedAsPartial || !strings.Contains(readText(t, root, copyMarkerName), "STOPPED") {
				t.Errorf("%s: Finish as partial ended as %s with marker %q", step, fin.Status, readText(t, root, copyMarkerName))
			}
			if !n.journalHas(res.RunID, "end") {
				t.Errorf("%s: Finish as partial left no journal end", step)
			}
		case contains(obs.Actions, ActCheckAndFinish):
			if _, err := n.CheckAndFinish(res.RunID); err != nil {
				t.Fatalf("%s: Check and finish: %v", step, err)
			}
			if exists(root, copyMarkerName) || !n.journalHas(res.RunID, "end") {
				t.Errorf("%s: Check and finish left the marker or no end", step)
			}
			if bad, missing, extra := sumsCheck(t, root); len(bad)+len(missing)+len(extra) != 0 {
				t.Errorf("%s: the finished folder does not verify: %v %v %v", step, bad, missing, extra)
			}
		}
	}
}

func keysOfFiles(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// covers: MA-429, MA-447, R29, R5
// A cancelled run and a partial run, each killed at W9a and between W10 and W11: the
// cancelled run's marker is never removed (it is rewritten CANCELLED, and a new launch
// calls it "Finished writing (Cancelled)", then reconciles it), and the partial run is
// never labelled complete (it reads "Finished with problems", recovered).
func TestCopyExportKilledCancelledAndPartialRuns(t *testing.T) {
	for _, kind := range []string{"cancelled", "partial"} {
		for _, step := range []string{"W9a", "W10"} {
			e, src := newCEEnv(t), newFakeSrc()
			e.faults = &copyFaults{killAt: step}
			p := cePlan(t, e, src, ceMsgs(4))
			var run *CopyRun
			if kind == "cancelled" {
				run = e.NewCopyRun(p, src)
				src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
					if m.SourceID == "M00003" {
						run.Cancel()
						<-ctx.Done()
						return 0, ctx.Err()
					}
					n, err := w.Write(ceBody(m.SourceID))
					return int64(n), err
				}
			} else {
				run = e.NewCopyRun(p, src)
				src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
					if m.SourceID == "M00003" {
						return 0, &graph.StatusError{Code: 404, GraphCode: "ErrorItemNotFound", Text: "gone"}
					}
					n, err := w.Write(ceBody(m.SourceID))
					return int64(n), err
				}
			}
			res, err := run.Run(context.Background())
			if err != nil || res.Killed != step {
				t.Fatalf("%s killed at %s: %v %+v", kind, step, err, res)
			}
			n := relaunch(e)
			obs, err := n.ReconcileRun(res.RunID)
			if err != nil {
				t.Fatal(err)
			}
			hasMarker := exists(res.Root, copyMarkerName)
			switch kind {
			case "cancelled":
				if !hasMarker || !strings.Contains(readText(t, res.Root, copyMarkerName), "CANCELLED") && step == "W10" {
					t.Errorf("cancelled killed at %s: the marker is %v / %q", step, hasMarker, readText(t, res.Root, copyMarkerName))
				}
				if !strings.HasPrefix(obs.Label, "Finished writing (Cancelled)") {
					t.Errorf("cancelled killed at %s reconciled as %q", step, obs.Label)
				}
			case "partial":
				if strings.HasPrefix(obs.Label, "Complete") || !strings.Contains(obs.Label, "Finished with problems") {
					t.Errorf("partial killed at %s reconciled as %q, never complete", step, obs.Label)
				}
				if step == "W10" && hasMarker {
					t.Errorf("partial killed after W10 still has its marker")
				}
			}
			if !n.ledgerOf(res.RunID) {
				t.Errorf("%s killed at %s: reconcile left no ledger line", kind, step)
			}
		}
	}
}

// ledgerOf reports whether the ledger has a line for run.
func (e *CopyExportEnv) ledgerOf(run string) bool {
	l, _ := e.readLedger()
	return ledgerHas(l, run)
}

// covers: MA-447, R29, R5, S52
// The run record: its header and plan are fsynced before the marker's temp file is
// written (the sync seam sees the record first); one line per finished message carries
// every file's role, SHA-256 and size; the manifest and sums are built from it, so a
// .eml replaced on the destination after its line still fails `sha256sum -c` against
// the sums; no .mailexport-entries.tmp exists in the root at any step; the sealed
// fingerprint is sha256 of the in-memory sums; and the presence check makes a run
// partial when a recorded file vanished (a fault hook), naming it in the failures.
func TestCopyExportRunRecordIsTheTruth(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	var mu sync.Mutex
	var synced []string
	e.syncFile = func(f *os.File) error {
		mu.Lock()
		synced = append(synced, filepath.Base(f.Name()))
		mu.Unlock()
		return nil
	}
	p := cePlan(t, e, src, ceMsgs(4))
	root := filepath.Join(p.Dest, p.ExportName)
	var forged, vanished string
	e.faults = &copyFaults{hook: func(step string) {
		// No destination spool at any point.
		for f := range treeFiles(t, root) {
			if strings.Contains(f, ".mailexport-entries.tmp") {
				t.Errorf("at %s the root holds %s", step, f)
			}
		}
		if step == "W7pre" {
			files := treeFiles(t, root)
			var msgs []string
			for f := range files {
				if strings.HasSuffix(f, ".eml") {
					msgs = append(msgs, f)
				}
			}
			sortStrings(msgs)
			forged, vanished = msgs[0], msgs[1]
			os.WriteFile(filepath.Join(root, forged), []byte("forged bytes"), 0o600)
			os.Remove(filepath.Join(root, vanished))
		}
	}}
	res := mustRun(t, e, p, src)

	mu.Lock()
	first := -1
	for i, f := range synced {
		if f == res.RunID+".jsonl" && first < 0 {
			first = i
		}
		if strings.HasPrefix(f, copyTempPrefix) {
			if first < 0 {
				t.Errorf("a file was written (%s) before the run record's header and plan were fsynced: %v", f, synced)
			}
			break
		}
	}
	mu.Unlock()

	// The forged file is in the sums by its recorded hash, so the recipient's check
	// fails on it; the vanished one is partial, failed and absent from the sums.
	if res.Outcome != CopyOutcomePartial || res.Counts.Failed != 1 || res.Counts.Exported != 3 {
		t.Fatalf("a vanished recorded file ended the run as %s, %+v", res.Label, res.Counts)
	}
	bad, missing, extra := sumsCheck(t, res.Root)
	if len(bad) != 1 || bad[0] != forged || len(missing) != 0 || len(extra) != 0 {
		t.Errorf("sums check: changed %v, missing %v, unlisted %v; want only the forged %s", bad, missing, extra, forged)
	}
	fail := assure.Reached(t, firstFailure(t, res.Root), "the vanished file's failure")
	if !strings.Contains(fail["reason"].(string), filepath.Base(vanished)) || fail["class"] != classRetryable {
		t.Errorf("the failure is %v, want it to name %s", fail, vanished)
	}
	sum := mustSummary(t, e, res.RunID)
	want := sha256.Sum256([]byte(readText(t, res.Root, copySumsName)))
	if sum.Sealed == nil || sum.Sealed.Fingerprint != hex.EncodeToString(want[:]) {
		t.Errorf("the sealed fingerprint is not sha256 of SHA256SUMS.txt: %+v", sum.Sealed)
	}
	// Every file of a message is recorded with role, sha256 and size.
	n := 0
	walkMessages(e.recordPath(res.RunID), func(pl *cePlanLine, m *ceMsgLine) error {
		if m != nil && m.Status == statusExported {
			n++
			f := m.Files[0]
			if f.Role != roleOriginal || len(f.SHA256) != 64 || f.Size != int64(len(ceBody(pl.SourceID))) {
				t.Errorf("message %d's file line = %+v", pl.N, f)
			}
		}
		return nil
	})
	if n != 3 {
		t.Errorf("%d exported lines, want 3", n)
	}
}

func firstFailure(t *testing.T, root string) map[string]any {
	t.Helper()
	fs := manifestOf(t, root)["failures"].([]any)
	if len(fs) == 0 {
		return nil
	}
	return fs[0].(map[string]any)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// covers: MA-447, R29, R5, S52
// The seal-and-finish step is idempotent and its parts survive a kill: a kill between
// W9a and W9b leaves, after the next launch's reconcile, exactly one ledger line equal
// to the sealed fingerprint (carrying the chain id, the set state, the set fingerprint
// and the parts the sealed line stored) and no journal end, the marker still present,
// and Check and finish then appends the end and removes the marker; a kill between W10
// and W11 of a complete run leaves one ledger line, and an end once reconcile sees the
// marker absent; and a ledger append that fails (a fault hook) stops the step with R-d2,
// the marker kept, which Check and finish then completes: the line, the marker action
// and the end.
func TestCopyExportSealAndFinishSurvivesAKill(t *testing.T) {
	// W9a..W9b
	e, res, _ := killedRun(t, "W9a", 3)
	n := relaunch(e)
	if l, _ := n.readLedger(); len(l) != 0 {
		t.Fatalf("the ledger already has %d lines before the reconcile", len(l))
	}
	obs, err := n.ReconcileRun(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	sealed := mustSummary(t, n, res.RunID).Sealed
	ledger, _ := n.readLedger()
	if len(ledger) != 1 || ledger[0].Run != res.RunID || ledger[0].Fingerprint != sealed.Fingerprint || ledger[0].Kind != "sealed" {
		t.Fatalf("after reconcile the ledger = %+v, want one line equal to the sealed fingerprint", ledger)
	}
	if l := ledger[0]; l.Chain != sealed.Chain || l.Chain == "" || l.SetState != sealed.SetState || l.SetFingerprint != sealed.SetFingerprint || strings.Join(l.Parts, ",") != strings.Join(sealed.Parts, ",") || len(l.Parts) != 1 {
		t.Errorf("the reconciled ledger line %+v does not carry what the sealed line stored %+v", l, sealed)
	}
	if n.journalHas(res.RunID, "end") || !exists(res.Root, copyMarkerName) || obs.Kind != obsFinishing {
		t.Errorf("end %v, marker %v, kind %s: want no end, the marker, and Finished writing", n.journalHas(res.RunID, "end"), exists(res.Root, copyMarkerName), obs.Kind)
	}
	if _, err := n.CheckAndFinish(res.RunID); err != nil {
		t.Fatal(err)
	}
	if l, _ := n.readLedger(); len(l) != 1 || !n.journalHas(res.RunID, "end") || exists(res.Root, copyMarkerName) {
		t.Errorf("Check and finish left %d ledger lines, end %v, marker %v", len(l), n.journalHas(res.RunID, "end"), exists(res.Root, copyMarkerName))
	}

	// W10..W11: the marker action is done, the end is not.
	e, res, _ = killedRun(t, "W10", 3)
	n = relaunch(e)
	if l, _ := n.readLedger(); len(l) != 1 || n.journalHas(res.RunID, "end") || exists(res.Root, copyMarkerName) {
		t.Fatalf("after W10: ledger %d lines, end %v, marker %v", len(l), n.journalHas(res.RunID, "end"), exists(res.Root, copyMarkerName))
	}
	obs, err = n.ReconcileRun(res.RunID)
	if err != nil || obs.Kind != obsRecovered || obs.Label != "Complete (recovered)" || !n.journalHas(res.RunID, "end") {
		t.Errorf("reconcile after W10: %+v (%v), end %v", obs, err, n.journalHas(res.RunID, "end"))
	}
	if l, _ := n.readLedger(); len(l) != 1 {
		t.Errorf("%d ledger lines after reconcile, want 1", len(l))
	}
	// Reconciling again changes nothing: the outcome is terminal and is not probed again.
	again, _ := n.ReconcileRun(res.RunID)
	jl, _ := n.readJournal()
	recs := 0
	for _, l := range jl {
		if l.Run == res.RunID && l.Event == "reconciled" {
			recs++
		}
	}
	if !again.Terminal || recs != 1 {
		t.Errorf("a terminal outcome was journaled %d times and returned %+v", recs, again)
	}

	// A ledger append that fails stops the step with R-d2 and keeps the marker.
	e, src := newCEEnv(t), newFakeSrc()
	var failing = true
	e.faults = &copyFaults{appendErr: func(path string) error {
		if failing && strings.HasSuffix(path, "export-fingerprints.jsonl") {
			return syscall.ENOSPC
		}
		return nil
	}}
	res = mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	if res.Stop == nil || res.Stop.RID != "R-d2" || !strings.HasPrefix(res.Label, "Finished writing") {
		t.Fatalf("a failed ledger append ended as %s (%+v), want R-d2 and \"Finished writing\"", res.Label, res.Stop)
	}
	if !exists(res.Root, copyMarkerName) || mustSummary(t, e, res.RunID).Sealed == nil {
		t.Error("the marker must stay and the seal stand")
	}
	failing = false
	if _, err := e.CheckAndFinish(res.RunID); err != nil {
		t.Fatal(err)
	}
	if l, _ := e.readLedger(); len(l) != 1 || !e.journalHas(res.RunID, "end") || exists(res.Root, copyMarkerName) {
		t.Errorf("Check and finish: %d ledger lines, end %v, marker %v", len(l), e.journalHas(res.RunID, "end"), exists(res.Root, copyMarkerName))
	}
}

// covers: MA-447, R29, R5, S52
// Check and finish verifies before it acts: a SHA256SUMS.txt that was re-hashed, a
// changed file and a missing one each refuse with the marker kept, and the seal-and-
// finish step then does not run.
func TestCopyExportCheckAndFinishRefusesAFolderThatChanged(t *testing.T) {
	for _, tamper := range []string{"sums", "file", "gone"} {
		e, res, _ := killedRun(t, "W9a", 3)
		var victim string
		for f := range treeFiles(t, res.Root) {
			if strings.HasSuffix(f, ".eml") {
				victim = f
			}
		}
		switch tamper {
		case "sums":
			os.WriteFile(filepath.Join(res.Root, copySumsName), []byte("re-hashed\n"), 0o600)
		case "file":
			os.WriteFile(filepath.Join(res.Root, victim), []byte("changed"), 0o600)
		case "gone":
			os.Remove(filepath.Join(res.Root, victim))
		}
		_, err := relaunch(e).CheckAndFinish(res.RunID)
		var ck *errCheckFailed
		if !errors.As(err, &ck) {
			t.Fatalf("%s: Check and finish accepted a folder that changed: %v", tamper, err)
		}
		assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names("the marker is kept"),
			assure.NoSideEffect(func() bool { return exists(res.Root, copyMarkerName) && !relaunch(e).journalHas(res.RunID, "end") }))
	}
}

// covers: MA-447, R29, R5, S52
// The retry chain's set state (RR-2b re-verification): the set fingerprint is SHA-256 over
// the SHA256SUMS.txt files of the chain's sealed parts in the order they sealed, resumed
// from the chain's latest ledger line by chain id. Part 1 is killed unsealed, part 2
// (which names part 1's chain) seals, then part 1 is finished as partial: its ledger line
// names the parts in seal order, part 2 then part 1, and carries the set fingerprint over
// part 2's sums then part 1's.
func TestCopyExportSetFingerprintFollowsSealOrder(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	e.faults = &copyFaults{killAt: "W9"}
	p1 := cePlan(t, e, src, ceMsgs(3))
	r1 := mustRun(t, e, p1, src)
	if r1.Killed != "W9" {
		t.Fatalf("part 1 was not killed: %+v", r1)
	}
	e.faults = nil
	p2 := cePlan(t, e, src, ceMsgs(2))
	p2.Chain = CopyChain{ID: r1.RunID, RetryOf: r1.RunID, RootName: p1.ExportName + "-part2",
		Parts: []CopyChainRef{{Part: 1, RunID: r1.RunID, Root: p1.ExportName}}}
	p2.ExportName = p2.Chain.RootName
	r2 := mustRun(t, e, p2, src)
	if r2.Status != CopyStatusComplete {
		t.Fatalf("part 2: %s", r2.Label)
	}
	if _, err := e.FinishAsPartial(r1.RunID); err != nil {
		t.Fatal(err)
	}
	sums1, sums2 := []byte(readText(t, r1.Root, copySumsName)), []byte(readText(t, r2.Root, copySumsName))
	h := sha256.New()
	h.Write(sums2)
	h.Write(sums1)
	want := hex.EncodeToString(h.Sum(nil))
	ledger, _ := e.readLedger()
	if len(ledger) != 2 {
		t.Fatalf("the ledger has %d lines, want 2", len(ledger))
	}
	l2, l1 := ledger[0], ledger[1]
	if l2.Run != r2.RunID || l1.Run != r1.RunID || l1.Chain != r1.RunID || l2.Chain != r1.RunID {
		t.Fatalf("ledger lines out of order or without the chain id: %+v", ledger)
	}
	if strings.Join(l1.Parts, ",") != r2.RunID+","+r1.RunID || l1.SetFingerprint != want {
		t.Errorf("part 1's ledger line: parts %v, set fingerprint %s; want [part 2, part 1] and %s", l1.Parts, l1.SetFingerprint, want)
	}
	if strings.Join(l2.Parts, ",") != r2.RunID || l2.SetFingerprint != hexSHA256(sums2) {
		t.Errorf("part 2's ledger line: parts %v, set fingerprint %s; want itself alone", l2.Parts, l2.SetFingerprint)
	}
	if man := manifestOf(t, r2.Root); len(man["run"].(map[string]any)["chain"].([]any)) != 2 || man["run"].(map[string]any)["retry_of"] != r1.RunID {
		t.Errorf("part 2's manifest chain = %v", man["run"])
	}
}
