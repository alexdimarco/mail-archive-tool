package app

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// cancelledRun runs 4 messages and cancels at the third; the killAt step, when given,
// leaves the run killed there.
func cancelledRun(t *testing.T, e *CopyExportEnv, src *fakeSrc, killAt string) *CopyResult {
	t.Helper()
	if killAt != "" {
		e.faults = &copyFaults{killAt: killAt}
		defer func() { e.faults = nil }()
	}
	run := e.NewCopyRun(cePlan(t, e, src, ceMsgs(4)), src)
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		if m.SourceID == "M00003" {
			run.Cancel()
			<-ctx.Done()
			return 0, ctx.Err()
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	res, err := run.Run(context.Background())
	if err != nil {
		t.Fatalf("the cancelled run: %v", err)
	}
	return res
}

// covers: MA-437, MA-521, R29
// EW-AC7 tombstone. "Export failed messages again" read only the one run's record, so
// after part 2 had exported part 1's failed messages, part 1's card still offered the
// retry and a second retry rebuilt the same messages into part 3: the same message in
// two parts of one set, fetched again, and the set fingerprint over the duplicates. The
// retry and remaining readers (and the offers on the card) now take away what the
// chain's other sealed parts exported. The first retry, offered and run, is the twin.
func TestCopyExportRetryDoesNotRepeatWhatAnotherPartExported(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	failing := map[string]bool{"M00002": true, "M00003": true}
	failIDs(src, failing)
	p1 := cePlan(t, e, src, ceMsgs(5))
	r1 := mustRun(t, e, p1, src)
	if r1.Outcome != CopyOutcomePartial {
		t.Fatalf("part 1: %s", r1.Label)
	}
	offers := func(run string) []string {
		hist, err := e.History(nil, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range hist {
			if c.Run == run {
				return c.Actions
			}
		}
		t.Fatalf("no card for %s", run)
		return nil
	}
	if !contains(offers(r1.RunID), ActExportFailed) {
		t.Fatalf("part 1 does not offer the retry before it is made: %v", offers(r1.RunID))
	}

	delete(failing, "M00002")
	delete(failing, "M00003")
	in, err := e.RetryInput(r1.RunID, "failed", "alice@contoso.org")
	if err != nil || len(in.Messages) != 2 {
		t.Fatalf("the first retry: %v (%v)", in, err)
	}
	in.Dest = p1.Dest
	p2, err := e.PlanCopyExport(*in)
	if err != nil {
		t.Fatal(err)
	}
	if r2 := mustRun(t, e, p2, src); r2.Outcome != CopyOutcomeComplete || r2.Counts.Exported != 2 {
		t.Fatalf("part 2: %s %+v", r2.Label, r2.Counts)
	}

	if contains(offers(r1.RunID), ActExportFailed) {
		t.Errorf("part 1 still offers its failed messages after part 2 exported them: %v", offers(r1.RunID))
	}
	_, err = e.RetryInput(r1.RunID, "failed", "alice@contoso.org")
	refusedWith(t, err, "R-h", []string{"no messages left"})
	if rest, err := e.RunRemaining(r1.RunID); err != nil || len(rest) != 0 {
		t.Errorf("part 1's remaining messages after part 2 exported them: %d (%v), want none", len(rest), err)
	}
	if n := src.fetched("M00002"); n != 2 {
		t.Errorf("M00002 was fetched %d times (once failing in part 1, once in part 2), want 2", n)
	}
}

// covers: MA-448, R29, R5
// EW-AC10 tombstone. A cancelled, failed or finished-as-partial run keeps its marker by
// design (rewritten CANCELLED or STOPPED), so a reconcile of such a run that has ended
// found a sealed record and a marker and called it "Finished writing — this folder's
// marker may still need updating", offering only Check and finish and hiding the retry
// offers. A run whose journal has its `end` is settled: its card keeps its own words and
// its retry actions. The same run killed before the end (W9a: sealed, marker not yet
// rewritten) is still "Finished writing", the positive twin.
func TestCopyExportReconcileSettledRunWithItsMarker(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	killed := cancelledRun(t, e, src, "W9a")
	n := relaunch(e)
	if obs, err := n.ReconcileRun(killed.RunID); err != nil || obs.Kind != obsFinishing || !contains(obs.Actions, ActCheckAndFinish) {
		t.Fatalf("a run killed after its seal: %+v (%v)", obs, err)
	}

	e, src = newCEEnv(t), newFakeSrc()
	res := cancelledRun(t, e, src, "")
	if res.Status != CopyStatusCancelled || !e.Ended(res.RunID) || !exists(res.Root, copyMarkerName) {
		t.Fatalf("the cancelled run: %s, ended %v", res.Label, e.Ended(res.RunID))
	}
	obs, err := e.ReconcileRun(res.RunID)
	if err != nil || obs.Kind == obsFinishing || contains(obs.Actions, ActCheckAndFinish) || strings.Contains(obs.Label, "marker may still need updating") {
		t.Fatalf("an ended cancelled run reconciled as %+v (%v)", obs, err)
	}
	hist, err := e.History(map[string]CopyObservation{res.RunID: obs}, "")
	if err != nil || len(hist) != 1 || !contains(hist[0].Actions, ActExportRemaining) || !strings.Contains(hist[0].Label, "Cancelled") {
		t.Fatalf("its card after the reconcile: %+v (%v)", hist, err)
	}
}

// covers: MA-448, R29, R5
// EW-AC1 tombstone. The wedge timer can fire while a cancelled run is still finalizing
// (a slow share), and WedgeStop then appends its `stopped` line through a second handle
// of the run record. The record was opened without O_APPEND, so each handle kept its
// own offset: a `stopped` line written at the offset WedgeStop had read before the run
// appended its `sealed` line landed on top of the seal, and a complete export read back
// as unsealed (and was offered Finish as partial, which would rewrite and STOPPED-mark
// it). Appends now go to the end of the file whichever handle makes them. The run is
// held at W9, WedgeStop is held between opening the record and its append, and the run
// seals before WedgeStop writes: the exact interleaving of the finding.
func TestCopyExportWedgeStopCannotOverwriteTheSeal(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	p := cePlan(t, e, src, ceMsgs(2))
	run := e.NewCopyRun(p, src)
	var runID string
	run.OnStart = func(id string) { runID = id }

	var armed atomic.Bool
	blocked, release, wedgeDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var wedgeErr error
	var once sync.Once
	e.faults = &copyFaults{
		appendErr: func(string) error {
			if armed.CompareAndSwap(true, false) {
				close(blocked)
				<-release
			}
			return nil
		},
		hook: func(step string) {
			switch step {
			case "W9":
				armed.Store(true)
				go func() { wedgeErr = e.WedgeStop(runID); close(wedgeDone) }()
				<-blocked
			case "W9a":
				once.Do(func() { close(release) })
				<-wedgeDone
			}
		},
	}
	res, err := run.Run(context.Background())
	if err != nil || res.Status != CopyStatusComplete {
		t.Fatalf("the run: %+v (%v)", res, err)
	}
	if wedgeErr != nil {
		t.Fatalf("WedgeStop: %v", wedgeErr)
	}
	sum := mustSummary(t, e, runID)
	if sum.Sealed == nil {
		t.Fatalf("the late `stopped` line overwrote the seal: sealed=%v stopped=%v", sum.Sealed != nil, sum.Stopped != nil)
	}
	if sum.Stopped == nil {
		t.Error("WedgeStop's own line is not in the record")
	}
}
