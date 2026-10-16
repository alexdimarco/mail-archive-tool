package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
)

// failIDs makes src fail the given messages with Microsoft's retryable conversion error,
// and serve every other message as the fake does.
func failIDs(src *fakeSrc, failing map[string]bool) {
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		if failing[m.SourceID] {
			return 0, &graph.StatusError{Code: 400, GraphCode: "ErrorMimeContentConversionFailed", Text: "x"}
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
}

// emlOf returns one message file of an export root.
func emlOf(t *testing.T, root string) string {
	t.Helper()
	var victim string
	for f := range treeFiles(t, root) {
		if strings.HasSuffix(f, ".eml") && (victim == "" || f < victim) {
			victim = f
		}
	}
	return assure.Reached(t, victim, "a message file in the export")
}

// rewriteHeader rewrites the header line of a run record, to stand for a record made
// where the folder had another identity.
func rewriteHeader(t *testing.T, e *CopyExportEnv, run string, edit func(*ceHeaderLine)) {
	t.Helper()
	path := e.recordPath(run)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first, rest, _ := bytes.Cut(b, []byte("\n"))
	var h ceHeaderLine
	if err := json.Unmarshal(first, &h); err != nil {
		t.Fatal(err)
	}
	edit(&h)
	nb, _ := json.Marshal(h)
	if err := os.WriteFile(path, append(append(nb, '\n'), rest...), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-520, R29, S52
// Check this export reads the recorded folder, confirmed by §4.9's tuple, and re-hashes
// every file SHA256SUMS.txt lists, compares sha256 of the sums on disk with the sealed
// fingerprint, and lists the files nothing lists: a clean export is GREEN; a flipped
// byte, a missing file and a planted file are each RED and name the file; a re-hashed
// SUMS file is RED although every listed file then matches; a run that was never sealed
// says "fingerprint not recorded at export time" and is never GREEN; a folder that was
// replaced is R-r and one that is not there is R-r2, with nothing read.
func TestCheckThisExportReportsWhatChanged(t *testing.T) {
	fresh := func() (*CopyExportEnv, *CopyResult) {
		e, src := newCEEnv(t), newFakeSrc()
		return e, mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	}
	e, res := fresh()
	chk, err := e.CheckExport(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !chk.OK || chk.Files != 5 || !chk.FingerprintOK || chk.Fingerprint != res.Fingerprint || chk.MarkerPresent || len(chk.Problems) != 0 {
		t.Fatalf("a clean export: %+v, want GREEN with 5 files (3 messages, README, manifest)", chk)
	}

	for _, tc := range []struct {
		name    string
		tamper  func(root, victim string)
		changed bool
		missing bool
		extra   bool
		fpBad   bool
	}{
		{"a flipped byte", func(root, v string) {
			p := filepath.Join(root, v)
			b, _ := os.ReadFile(p)
			b[len(b)-3] ^= 1
			os.WriteFile(p, b, 0o600)
		}, true, false, false, false},
		{"a missing file", func(root, v string) { os.Remove(filepath.Join(root, v)) }, false, true, false, false},
		{"a planted unlisted file", func(root, v string) { os.WriteFile(filepath.Join(root, "planted.txt"), []byte("x"), 0o600) }, false, false, true, false},
		{"a forged file with the sums re-hashed to match", func(root, v string) {
			p := filepath.Join(root, v)
			os.WriteFile(p, []byte("forged"), 0o600)
			sum := sha256.Sum256([]byte("forged"))
			sums := readText(t, root, copySumsName)
			var out []string
			for _, line := range strings.Split(strings.TrimRight(sums, "\n"), "\n") {
				if strings.HasSuffix(line, "  "+v) {
					line = hex.EncodeToString(sum[:]) + "  " + v
				}
				out = append(out, line)
			}
			os.WriteFile(filepath.Join(root, copySumsName), []byte(strings.Join(out, "\n")+"\n"), 0o600)
		}, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, res := fresh()
			victim := emlOf(t, res.Root)
			tc.tamper(res.Root, victim)
			chk, err := e.CheckExport(res.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if chk.OK {
				t.Fatalf("%s is GREEN: %+v", tc.name, chk)
			}
			if tc.changed && (len(chk.Changed) != 1 || chk.Changed[0] != victim) ||
				tc.missing && (len(chk.Missing) != 1 || chk.Missing[0] != victim) ||
				tc.extra && (len(chk.Unlisted) != 1 || chk.Unlisted[0] != "planted.txt") {
				t.Errorf("%s: changed %v, missing %v, unlisted %v; want it named", tc.name, chk.Changed, chk.Missing, chk.Unlisted)
			}
			if tc.fpBad != !chk.FingerprintOK || (tc.fpBad && len(chk.Changed)+len(chk.Missing) != 0) {
				t.Errorf("%s: fingerprint ok %v, changed %v: the re-hashed sums must fail the fingerprint alone", tc.name, chk.FingerprintOK, chk.Changed)
			}
			said := filepath.Base(victim)
			switch {
			case tc.fpBad:
				said = "not the one that was sealed"
			case tc.extra:
				said = "planted.txt"
			}
			if !strings.Contains(strings.Join(chk.Problems, "\n"), said) {
				t.Errorf("%s: the findings do not say %q: %q", tc.name, said, chk.Problems)
			}
		})
	}

	// A run that was never sealed has no fingerprint to compare, and is never GREEN.
	e2, src := newCEEnv(t), newFakeSrc()
	k := killedIn(t, e2, src, "W9", ceMsgs(2))
	chk, err = e2.CheckExport(k.RunID)
	if err != nil || !chk.NoFingerprint || chk.OK || !chk.MarkerPresent || !strings.Contains(strings.Join(chk.Problems, "\n"), "fingerprint not recorded at export time") {
		t.Errorf("an unsealed run: %+v (%v)", chk, err)
	}

	// The recorded folder is what is checked: a replaced folder is R-r, a missing one R-r2.
	e3, res3 := fresh()
	moved := res3.Root + ".away"
	os.Rename(res3.Root, moved)
	_, err = e3.CheckExport(res3.RunID)
	refusedWith(t, err, "R-r2", []string{"couldn't confirm", "Look again"})
	os.Mkdir(res3.Root, 0o700)
	_, err = e3.CheckExport(res3.RunID)
	refusedWith(t, err, "R-r", []string{"moved or replaced", "Locate"})
	if _, err := e3.CheckExport("no-such-run"); err == nil {
		t.Error("an unknown run was checked")
	}
}

// covers: MA-520, R29, S52
// Check and finish removes the app's own marker only when sha256(SUMS) equals the sealed
// fingerprint and every listed file matches: on a sealed partial run it removes the
// marker; on a sealed cancelled run it verifies and then rewrites the marker CANCELLED,
// and never removes it; and a SUMS file that was re-hashed to match a forged file is
// refused with the marker kept and no journal end.
func TestCheckAndFinishActsOnTheSealedStatus(t *testing.T) {
	// partial: the marker goes
	e, src := newCEEnv(t), newFakeSrc()
	failIDs(src, map[string]bool{"M00002": true})
	e.faults = &copyFaults{killAt: "W9a"}
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	if res.Killed != "W9a" {
		t.Fatalf("partial run: %+v", res)
	}
	n := relaunch(e)
	if _, err := n.CheckAndFinish(res.RunID); err != nil {
		t.Fatal(err)
	}
	if exists(res.Root, copyMarkerName) || !n.journalHas(res.RunID, "end") {
		t.Errorf("a sealed partial run: marker %v, end %v; want the marker removed and the end appended", exists(res.Root, copyMarkerName), n.journalHas(res.RunID, "end"))
	}

	// cancelled: the marker is rewritten CANCELLED, never removed
	e, src = newCEEnv(t), newFakeSrc()
	e.faults = &copyFaults{killAt: "W9a"}
	p := cePlan(t, e, src, ceMsgs(4))
	run := e.NewCopyRun(p, src)
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		if m.SourceID == "M00003" {
			run.Cancel()
			<-ctx.Done()
			return 0, ctx.Err()
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	cres, err := run.Run(context.Background())
	if err != nil || cres.Killed != "W9a" {
		t.Fatalf("cancelled run: %v %+v", err, cres)
	}
	n = relaunch(e)
	log := &markerLog{}
	n.fsWrap = func(f ceFS) ceFS { log.ceFS = f; return log }
	if _, err := n.CheckAndFinish(cres.RunID); err != nil {
		t.Fatal(err)
	}
	if log.removedMarker() || !strings.Contains(readText(t, cres.Root, copyMarkerName), "CANCELLED") {
		t.Errorf("a sealed cancelled run: the marker was removed (%v) or is %q; it is rewritten CANCELLED, never removed", log.removedMarker(), readText(t, cres.Root, copyMarkerName))
	}

	// re-hashed sums: refused, the marker kept
	e, src = newCEEnv(t), newFakeSrc()
	e.faults = &copyFaults{killAt: "W9a"}
	res = mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	victim := emlOf(t, res.Root)
	os.WriteFile(filepath.Join(res.Root, victim), []byte("forged"), 0o600)
	fp := sha256.Sum256([]byte("forged"))
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(readText(t, res.Root, copySumsName), "\n"), "\n") {
		if strings.HasSuffix(line, "  "+victim) {
			line = hex.EncodeToString(fp[:]) + "  " + victim
		}
		lines = append(lines, line)
	}
	os.WriteFile(filepath.Join(res.Root, copySumsName), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	n = relaunch(e)
	_, err = n.CheckAndFinish(res.RunID)
	var ck *errCheckFailed
	if !errors.As(err, &ck) {
		t.Fatalf("a re-hashed SUMS file was accepted: %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names("the marker is kept"),
		assure.NoSideEffect(func() bool { return exists(res.Root, copyMarkerName) && !n.journalHas(res.RunID, "end") }))
}

// covers: MA-521, R29, S52
// A retry of a retry is the next part: "-part2", then "-part3", each a new directory
// whose manifest names retry_of and carries the whole chain, whose README lists the
// siblings; Export activity's set fingerprint equals SHA-256 over the parts' SUMS files
// in seal order, and is still right when part 1's run record was deleted (compaction)
// before part 3 sealed. A part marked gone before it sealed never enters the set; one
// marked gone after it sealed stays in it, named as gone beside the set fingerprint, and
// the receipt lists each other part's own fingerprint.
func TestRetryChainIsNamedAndFingerprinted(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	failing := map[string]bool{"M00002": true}
	failIDs(src, failing)
	p1 := cePlan(t, e, src, ceMsgs(3))
	r1 := mustRun(t, e, p1, src)
	if r1.Outcome != CopyOutcomePartial {
		t.Fatalf("part 1: %s", r1.Label)
	}

	run := func(from string) (*CopyPlan, *CopyResult) {
		in, err := e.RetryInput(from, "failed", "alice@contoso.org")
		if err != nil {
			t.Fatalf("RetryInput(%s): %v", from, err)
		}
		in.Dest = p1.Dest
		p, err := e.PlanCopyExport(*in)
		if err != nil {
			t.Fatalf("the retry preview: %v", err)
		}
		return p, mustRun(t, e, p, src)
	}
	p2, r2 := run(r1.RunID)
	if want := p1.ExportName + "-part2"; p2.ExportName != want || filepath.Base(r2.Root) != want {
		t.Fatalf("part 2 is named %q (made %q), want %q", p2.ExportName, filepath.Base(r2.Root), want)
	}
	p3, _ := func() (*CopyPlan, *CopyResult) { // part 3 is made from part 2, which failed the message again
		in, err := e.RetryInput(r2.RunID, "failed", "alice@contoso.org")
		if err != nil {
			t.Fatal(err)
		}
		p, err := e.PlanCopyExport(*in)
		if err != nil {
			t.Fatal(err)
		}
		return p, nil
	}()
	if want := p1.ExportName + "-part3"; p3.ExportName != want {
		t.Fatalf("a retry of a retry is named %q, want %q", p3.ExportName, want)
	}
	// Part 1's record goes (compaction, once its ledger line exists) before part 3 seals.
	if err := os.Remove(e.recordPath(r1.RunID)); err != nil {
		t.Fatal(err)
	}
	delete(failing, "M00002")
	r3 := mustRun(t, e, p3, src)
	if r3.Outcome != CopyOutcomeComplete {
		t.Fatalf("part 3: %s", r3.Label)
	}

	for i, r := range []*CopyResult{r1, r2, r3} {
		man := manifestOf(t, r.Root)
		chain := man["run"].(map[string]any)["chain"].([]any)
		if len(chain) != i+1 {
			t.Errorf("part %d's manifest chain has %d entries, want %d", i+1, len(chain), i+1)
		}
		if i > 0 && man["run"].(map[string]any)["retry_of"] != []*CopyResult{r1, r2}[i-1].RunID {
			t.Errorf("part %d names retry_of %v", i+1, man["run"].(map[string]any)["retry_of"])
		}
	}
	readme := readText(t, r3.Root, copyReadmeName)
	if !strings.Contains(readme, "part 1, "+p1.ExportName) || !strings.Contains(readme, "part 2, "+p2.ExportName) {
		t.Errorf("part 3's README does not list its siblings: %q", readme)
	}

	h := sha256.New()
	for _, r := range []*CopyResult{r1, r2, r3} {
		h.Write([]byte(readText(t, r.Root, copySumsName)))
	}
	want := hex.EncodeToString(h.Sum(nil))
	hist, err := e.History(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var card *CopyRunInfo
	for i := range hist {
		if hist[i].Run == r3.RunID {
			card = &hist[i]
		}
	}
	if card == nil || card.Set == nil || card.Set.Fingerprint != want || len(card.Set.Parts) != 3 {
		t.Fatalf("part 3's card: %+v, want the set fingerprint %s over three parts", card, want)
	}
	for i, p := range card.Set.Parts {
		if p.Run != []string{r1.RunID, r2.RunID, r3.RunID}[i] || p.Gone || p.RootName == "" || len(p.Fingerprint) != 64 {
			t.Errorf("set part %d = %+v", i+1, p)
		}
	}
	if card.Set.Parts[0].RootName != p1.ExportName {
		t.Errorf("part 1, whose record is deleted, is named %q from the journal, want %q", card.Set.Parts[0].RootName, p1.ExportName)
	}

	// A part marked gone after it sealed stays in the set, named as gone, and the receipt
	// lists each part's own fingerprint.
	if _, err := e.MarkGone(r2.RunID); err != nil {
		t.Fatal(err)
	}
	hist, _ = e.History(nil, "")
	for _, c := range hist {
		if c.Run == r3.RunID {
			if c.Set == nil || c.Set.Fingerprint != want || !c.Set.Parts[1].Gone || c.Set.Parts[0].Gone {
				t.Errorf("after part 2 is marked gone the set is %+v: it must keep part 2 and name it gone", c.Set)
			}
		}
	}
	receipt, err := e.Receipt(r3.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{GroupFingerprint(want), "marked gone", GroupFingerprint(r1.Fingerprint), GroupFingerprint(r2.Fingerprint), GroupFingerprint(r3.Fingerprint), p2.ExportName} {
		if !strings.Contains(receipt, need) {
			t.Errorf("the receipt lacks %q:\n%s", need, receipt)
		}
	}

	// A part marked gone before it sealed never enters the set.
	e2, src2 := newCEEnv(t), newFakeSrc()
	failIDs(src2, map[string]bool{"M00002": true})
	q1 := cePlan(t, e2, src2, ceMsgs(3))
	a1 := mustRun(t, e2, q1, src2)
	in, _ := e2.RetryInput(a1.RunID, "failed", "alice@contoso.org")
	in.Dest = q1.Dest
	e2.faults = &copyFaults{killAt: "W9"}
	qp, _ := e2.PlanCopyExport(*in)
	a2 := mustRun(t, e2, qp, src2) // part 2 never seals
	e2.faults = nil
	if _, err := e2.MarkGone(a2.RunID); err != nil {
		t.Fatal(err)
	}
	failIDs(src2, nil)
	in3, err := e2.RetryInput(a1.RunID, "failed", "alice@contoso.org")
	if err != nil {
		t.Fatal(err)
	}
	in3.Dest = q1.Dest
	p3b, _ := e2.PlanCopyExport(*in3)
	a3 := mustRun(t, e2, p3b, src2)
	h2 := sha256.New()
	h2.Write([]byte(readText(t, a1.Root, copySumsName)))
	h2.Write([]byte(readText(t, a3.Root, copySumsName)))
	hist, _ = e2.History(nil, "")
	for _, c := range hist {
		if c.Run == a3.RunID {
			if c.Set == nil || len(c.Set.Parts) != 2 || c.Set.Parts[0].Run != a1.RunID || c.Set.Parts[1].Run != a3.RunID || c.Set.Fingerprint != hex.EncodeToString(h2.Sum(nil)) {
				t.Errorf("a part marked gone before it sealed entered the set: %+v", c.Set)
			}
		}
	}
}

// covers: MA-430, R29, R12, S52
// After a restart each run is labelled from its sealed line: Complete; "Finished with
// problems — N messages not exported"; and the runs a run-fatal stop ended (R-g, R-u,
// R-c and R-z) are each "Stopped: <reason>", with that R-id's remedy in the sealed
// stop and the actions their status allows (the remaining messages are offered), and an
// R-z stop between messages records its reason with no message in flight.
func TestActivityLabelsFollowTheSealedLine(t *testing.T) {
	e := newCEEnv(t)
	run := func(src *fakeSrc, n int, mod func(p *CopyPlan)) *CopyResult {
		p := cePlan(t, e, src, ceMsgs(n))
		if mod != nil {
			mod(p)
		}
		return mustRun(t, e, p, src)
	}
	complete := run(newFakeSrc(), 2, nil)
	psrc := newFakeSrc()
	failIDs(psrc, map[string]bool{"M00002": true})
	partial := run(psrc, 3, nil)

	body := len(ceBody("M00001"))
	fake := newFaultFS()
	fake.fullAfter = int64(2*body) + int64(body)/2
	e.fsWrap = func(f ceFS) ceFS { fake.ceFS = f; return fake }
	rg := run(newFakeSrc(), 6, nil)
	e.fsWrap = nil

	e.window = 150 * time.Millisecond
	usrc := newFakeSrc()
	usrc.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		return 0, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
	}
	usrc.probeFn = func(ctx context.Context) error { return &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED} }
	ru := run(usrc, 3, nil)
	e.window = 300 * time.Millisecond

	csrc := newFakeSrc()
	csrc.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		return 0, &graph.StatusError{Code: 401, Text: "x"}
	}
	csrc.refresh = func(ctx context.Context) error { return errors.New("the refresh token was refused") }
	rc := run(csrc, 3, nil)

	zsrc := newFakeSrc()
	var polls atomic.Int32
	zsrc.signedIn = func() (string, bool) {
		if polls.Add(1) >= 2 {
			return "bob@contoso.org", true
		}
		return "alice@contoso.org", true
	}
	rz := run(zsrc, 150, nil)

	for rid, res := range map[string]*CopyResult{"R-g": rg, "R-u": ru, "R-c": rc, "R-z": rz} {
		if res.Stop == nil || res.Stop.RID != rid {
			t.Fatalf("the %s run ended as %s (%+v)", rid, res.Label, res.Stop)
		}
	}

	n := relaunch(e)
	rec := n.NewReconciler()
	if err := rec.Launch(); err != nil {
		t.Fatal(err)
	}
	rec.Wait()
	hist, err := n.History(rec.Observations(), "")
	if err != nil {
		t.Fatal(err)
	}
	card := map[string]CopyRunInfo{}
	for _, c := range hist {
		card[c.Run] = c
	}
	for _, c := range []struct {
		res    *CopyResult
		label  string
		remedy string
		actHas []string
		actNot []string
	}{
		{complete, "Complete", "", []string{ActOpenFolder, ActCheckThis}, []string{ActExportFailed, ActExportRemaining}},
		{partial, "Finished with problems — 1 messages not exported", "", []string{ActExportFailed, ActCheckThis}, []string{ActExportRemaining}},
		{rg, "Stopped: the destination is full or no longer available", "Export the remaining messages", []string{ActExportRemaining}, []string{ActExportFailed}},
		{ru, "Stopped: the connection to Microsoft 365 was lost", "Export the remaining messages", []string{ActExportRemaining}, nil},
		{rc, "Stopped: the Microsoft 365 sign-in was refused", "Sign in again", []string{ActExportRemaining}, nil},
		{rz, "Stopped: the Microsoft 365 sign-in was cleared or changed", "Sign in as alice@contoso.org", []string{ActExportRemaining}, nil},
	} {
		got, ok := card[c.res.RunID]
		if !ok {
			t.Fatalf("the run %s is not in the history", c.res.RunID)
		}
		if got.Label != c.label {
			t.Errorf("after a restart %s is labelled %q, want %q", c.res.RunID[:6], got.Label, c.label)
		}
		if c.remedy != "" && (got.Stop == nil || !strings.Contains(got.Stop.Message, c.remedy)) {
			t.Errorf("%q: the stop's words lack its remedy %q: %+v", c.label, c.remedy, got.Stop)
		}
		for _, a := range c.actHas {
			if !hasAction(got.Actions, a) {
				t.Errorf("%q: the card lacks the action %s: %v", c.label, a, got.Actions)
			}
		}
		for _, a := range c.actNot {
			if hasAction(got.Actions, a) {
				t.Errorf("%q: the card offers %s: %v", c.label, a, got.Actions)
			}
		}
	}
	if s := mustSummary(t, n, rz.RunID).Sealed; s == nil || s.Stop == nil || s.Stop.RID != "R-z" || s.Stop.InFlight != "" {
		t.Errorf("an R-z stop between messages: %+v, want its reason with no message in flight", s)
	}
}

// covers: MA-430, R29, R12, S52
// A recovered run is labelled from its sealed status, never complete unless that is the
// status: a partial run killed after its marker action, once its marker is seen absent,
// is "Finished with problems … (recovered)"; a run with no sealed record whose marker was
// deleted, beside a planted manifest that says complete, is never shown as complete: it
// says its marker is gone and the fingerprint was not recorded, and offers Finish as
// partial.
func TestActivityRecoveredNeverSaysComplete(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	failIDs(src, map[string]bool{"M00002": true})
	e.faults = &copyFaults{killAt: "W10"}
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	if res.Killed != "W10" {
		t.Fatalf("not killed: %+v", res)
	}
	n := relaunch(e)
	rec := n.NewReconciler()
	rec.Launch()
	rec.Wait()
	hist, _ := n.History(rec.Observations(), "")
	got := assure.Reached(t, hist, "the history")[0]
	if !strings.HasPrefix(got.Label, "Finished with problems") || !strings.HasSuffix(got.Label, "(recovered)") || strings.HasPrefix(got.Label, "Complete") {
		t.Errorf("a recovered partial run is labelled %q", got.Label)
	}

	// no sealed record, marker deleted, a manifest that says complete
	e2, src2 := newCEEnv(t), newFakeSrc()
	k := killedIn(t, e2, src2, "W9", ceMsgs(2))
	os.Remove(filepath.Join(k.Root, copyMarkerName))
	man := strings.Replace(readText(t, k.Root, copyManifestName), `"status":"failed"`, `"status":"complete"`, 1)
	os.WriteFile(filepath.Join(k.Root, copyManifestName), []byte(man), 0o600)
	n2 := relaunch(e2)
	rec2 := n2.NewReconciler()
	rec2.Launch()
	rec2.Wait()
	hist, _ = n2.History(rec2.Observations(), "")
	got = assure.Reached(t, hist, "the history")[0]
	if strings.Contains(got.Label, "Complete") || !strings.Contains(got.Label, "its marker is gone") || !strings.Contains(got.Label, "fingerprint not recorded at export time") ||
		!hasAction(got.Actions, ActFinishAsPartial) || got.Fingerprint != "" {
		t.Errorf("a run with no seal and a planted manifest: label %q, fingerprint %q, actions %v", got.Label, got.Fingerprint, got.Actions)
	}
	if l, _ := n2.readLedger(); len(l) != 0 {
		t.Errorf("a fingerprint was computed from the disk for a run that never sealed: %+v", l)
	}
}

// covers: MA-430, R29, R12, S52
// "Open folder" takes no path from the request: it opens only the run's recorded root,
// after the identity check. A folder with the run's nonce whose dev and inode changed
// (a fake remount, or vfat renumbering) opens; another run's id, a symlink, a
// non-directory and a different inode on a stable-inode filesystem are R-r; a folder that
// is not there, or has neither marker nor manifest, or a no-nonce one on a filesystem
// whose inodes are not stable, is R-r2, never R-r; an unknown run is refused. The empty
// export folder of an early kill is confirmed by its file identity where that can decide
// and offers Finish as partial; on a fake vfat that renumbered it is "couldn't confirm"
// and does not.
func TestOpenFolderRefusesWhatItCannotConfirm(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	tgt, err := e.OpenTarget(res.RunID)
	if err != nil || tgt.Root != res.Root || tgt.Readme != filepath.Join(res.Root, copyReadmeName) {
		t.Fatalf("a run's own folder: %+v (%v)", tgt, err)
	}
	if _, err := e.OpenTarget("not-a-run"); err == nil {
		t.Error("an unknown run was opened")
	}

	// dev and inode changed between launches, the nonce intact: opens
	rewriteHeader(t, e, res.RunID, func(h *ceHeaderLine) {
		h.Identity.Dev += 7
		h.Identity.Ino += 11
		h.FSType = "vfat"
		h.Identity.FSType = "vfat"
	})
	if _, err := e.OpenTarget(res.RunID); err != nil {
		t.Errorf("a remounted folder with its nonce was refused: %v", err)
	}
	// a different inode on the same mount of a stable-inode filesystem: R-r
	sum := mustSummary(t, e, res.RunID)
	cur := idOfDir(t, res.Root)
	rewriteHeader(t, e, res.RunID, func(h *ceHeaderLine) {
		h.Identity = ceIdentity{FSType: "ext4", Dev: cur.Dev, Ino: cur.Ino + 5, Birth: sum.Header.Identity.Birth}
	})
	_, err = e.OpenTarget(res.RunID)
	refusedWith(t, err, "R-r", []string{"moved or replaced", "Locate", "This folder is gone"})
	rewriteHeader(t, e, res.RunID, func(h *ceHeaderLine) { h.Identity = cur })

	// another run's id in the README: R-r
	readme := filepath.Join(res.Root, copyReadmeName)
	orig, _ := os.ReadFile(readme)
	os.WriteFile(readme, []byte(strings.Replace(string(orig), res.RunID, "someone-elses-run", 1)), 0o600)
	_, err = e.OpenTarget(res.RunID)
	refusedWith(t, err, "R-r", []string{"moved or replaced"})
	os.WriteFile(readme, orig, 0o600)

	// neither marker nor manifest: R-r2, never R-r
	os.Remove(filepath.Join(res.Root, copyManifestName))
	_, err = e.OpenTarget(res.RunID)
	refusedWith(t, err, "R-r2", []string{"couldn't confirm", "Look again"})

	// a symlink and a non-directory at the path: R-r
	e2, src2 := newCEEnv(t), newFakeSrc()
	r2 := mustRun(t, e2, cePlan(t, e2, src2, ceMsgs(1)), src2)
	away := r2.Root + ".away"
	os.Rename(r2.Root, away)
	if os.Symlink(away, r2.Root) == nil {
		_, err = e2.OpenTarget(r2.RunID)
		refusedWith(t, err, "R-r", []string{"moved or replaced"})
		os.Remove(r2.Root)
	}
	os.WriteFile(r2.Root, []byte("a file"), 0o600)
	_, err = e2.OpenTarget(r2.RunID)
	refusedWith(t, err, "R-r", []string{"moved or replaced"})
	os.Remove(r2.Root)
	_, err = e2.OpenTarget(r2.RunID)
	refusedWith(t, err, "R-r2", []string{"couldn't confirm"})

	// an empty export folder (a kill before the marker): its file identity decides
	e3, src3 := newCEEnv(t), newFakeSrc()
	k := killedIn(t, e3, src3, "W3", ceMsgs(2))
	if obs, _ := e3.ReconcileRun(k.RunID); obs.Kind != obsEmpty || !hasAction(obs.Actions, ActFinishAsPartial) {
		t.Errorf("an empty root with an unchanged identity: %+v, want Finish as partial", obs)
	}
	rewriteHeader(t, e3, k.RunID, func(h *ceHeaderLine) {
		h.Identity = ceIdentity{FSType: "vfat", Dev: h.Identity.Dev, Ino: h.Identity.Ino + 9}
	})
	obs, _ := n3Reconcile(t, e3, k.RunID)
	if obs.Kind != obsUnconfirm || hasAction(obs.Actions, ActFinishAsPartial) {
		t.Errorf("an empty root on a vfat that renumbered it: %+v, want \"couldn't confirm\" and no Finish as partial", obs)
	}
	_, err = e3.OpenTarget(k.RunID)
	refusedWith(t, err, "R-r2", []string{"couldn't confirm"})
}

// n3Reconcile reconciles run in a fresh launch, so a terminal outcome journaled by an
// earlier probe does not answer for it.
func n3Reconcile(t *testing.T, e *CopyExportEnv, run string) (CopyObservation, error) {
	t.Helper()
	n := relaunch(e)
	os.Remove(n.journalPath())
	if _, err := n.SweepRunRecords(); err != nil {
		t.Fatal(err)
	}
	return n.ReconcileRun(run)
}

// covers: MA-430, MA-448, R29, R12, S52
// A settled run whose record is deleted (§4.19) is still opened, located and marked gone
// from the journal line: its run id is the nonce, its dest_dir and root_id the recorded
// identity. A complete export whose folder was replaced refuses with R-r; Locate… then
// accepts the real folder, appends a `located` journal line, and Open folder opens it;
// This folder is gone on the same run appends a `reconciled` journal line.
func TestOpenFolderFromTheJournalWhenTheRecordIsGone(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	if err := os.Remove(e.recordPath(res.RunID)); err != nil { // compaction deleted the settled run's record
		t.Fatal(err)
	}
	if _, err := e.OpenTarget(res.RunID); err != nil {
		t.Fatalf("a settled run's folder from its journal line: %v", err)
	}
	away := res.Root + ".away"
	os.Rename(res.Root, away)
	os.Mkdir(res.Root, 0o700) // another folder at the path
	_, err := e.OpenTarget(res.RunID)
	refusedWith(t, err, "R-r", []string{"moved or replaced", "Locate", "This folder is gone"}) // a new inode on the same mount: positive mismatch

	if err := e.Locate(res.RunID, away); err != nil {
		t.Fatalf("Locate… from the journal line: %v", err)
	}
	if !e.journalHas(res.RunID, "located") {
		t.Error("no `located` journal line")
	}
	tgt, err := e.OpenTarget(res.RunID)
	if err != nil || tgt.Root != away {
		t.Errorf("after Locate… the folder opens at %+v (%v), want %s", tgt, err, away)
	}
	if err := e.Locate(res.RunID, res.Root); err == nil {
		t.Error("another folder was accepted as this export's")
	}
	if obs, err := e.MarkGone(res.RunID); err != nil || obs.Kind != obsGone || !e.terminalGone(res.RunID) {
		t.Errorf("This folder is gone from the journal line: %+v (%v)", obs, err)
	}
}

// covers: MA-430, R29, R12, S52
// A deleted journal leaves exports intact, and after the next launch an interrupted run
// and a sealed run both reappear (the sweep adopts their run records and rebuilds their
// start lines), with Finish as partial offered for the first and Open folder for the
// second; a record no journal line names (a kill inside W3) is adopted as an interrupted
// empty export folder.
func TestActivityReappearsAfterALostJournal(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	done := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	pending := killedIn(t, e, src, "W6:1", ceMsgs(3))
	early := killedIn(t, e, src, "W3", ceMsgs(1))
	if err := os.Remove(e.journalPath()); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{done.Root, pending.Root, early.Root} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("a lost journal lost an export: %v", err)
		}
	}
	n := relaunch(e)
	rec := n.NewReconciler()
	if err := rec.Launch(); err != nil {
		t.Fatal(err)
	}
	rec.Wait()
	hist, err := n.History(rec.Observations(), "")
	if err != nil {
		t.Fatal(err)
	}
	card := map[string]CopyRunInfo{}
	for _, c := range hist {
		card[c.Run] = c
	}
	if c := card[pending.RunID]; !hasAction(c.Actions, ActFinishAsPartial) || !strings.HasPrefix(c.Label, "interrupted") {
		t.Errorf("the interrupted run after a lost journal: %q %v", c.Label, c.Actions)
	}
	if c := card[early.RunID]; !hasAction(c.Actions, ActFinishAsPartial) || !strings.Contains(c.Label, "empty export folder") {
		t.Errorf("the run killed inside W3: %q %v", c.Label, c.Actions)
	}
	if c := card[done.RunID]; c.Label != "Complete (recovered)" || !hasAction(c.Actions, ActOpenFolder) {
		t.Errorf("the sealed run: %q %v", c.Label, c.Actions)
	}
}

// covers: MA-437, MA-427, R29, S52
// Retry reads the RUN RECORD, never the destination's manifest: "Export failed messages
// again" yields exactly a partial run's retryable failures (a terminal one stays out),
// also on a cancelled run that has some; "Export the remaining messages" yields a
// cancelled run's not-attempted ids, a stopped run's not-attempted ids and then its
// retryable failures with the message in flight last, and after R-u2 holds the 25 failed
// messages; each goes to a new part directory whose manifest names retry_of, and the
// earlier export is byte-unchanged; a signer mismatch is R-l with nothing made; the same
// ids come back after a restart and with the destination's manifest deleted or planted;
// the activity page offers each status its own actions.
func TestRetryReadsTheRunRecord(t *testing.T) {
	ids := func(in *CopyPlanInput) []string {
		var out []string
		for _, m := range in.Messages {
			out = append(out, m.SourceID)
		}
		return out
	}
	e, src := newCEEnv(t), newFakeSrc()
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		switch m.SourceID {
		case "M00002":
			return 0, &graph.StatusError{Code: 400, GraphCode: "ErrorMimeContentConversionFailed", Text: "x"}
		case "M00003":
			return 0, &graph.StatusError{Code: 404, GraphCode: "ErrorItemNotFound", Text: "x"}
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	p1 := cePlan(t, e, src, ceMsgs(4))
	partial := mustRun(t, e, p1, src)
	before := treeFiles(t, partial.Root)
	destBefore := lsTree(t, p1.Dest)

	in, err := e.RetryInput(partial.RunID, "failed", "alice@contoso.org")
	if err != nil || strings.Join(ids(in), ",") != "M00002" {
		t.Fatalf("failed messages again: %v (%v), want only the retryable M00002 (the 404 is terminal)", ids(in), err)
	}
	if in.Chain.RetryOf != partial.RunID || in.Chain.RootName != p1.ExportName+"-part2" || len(in.Chain.Parts) != 1 || in.Chain.Parts[0].Fingerprint != partial.Fingerprint {
		t.Errorf("the retry's chain = %+v", in.Chain)
	}
	if in.Dest != p1.Dest || len(in.Folders) != 1 || in.Folders[0].Messages != 1 {
		t.Errorf("the retry's destination and selection = %q, %+v", in.Dest, in.Folders)
	}
	// the destination's manifest, deleted or planted, changes nothing
	manifest := readText(t, partial.Root, copyManifestName)
	os.Remove(filepath.Join(partial.Root, copyManifestName))
	if gone, err := relaunch(e).RetryInput(partial.RunID, "failed", "alice@contoso.org"); err != nil || strings.Join(ids(gone), ",") != "M00002" {
		t.Errorf("after a restart and a deleted manifest: %v (%v)", ids(gone), err)
	}
	os.WriteFile(filepath.Join(partial.Root, copyManifestName), []byte(`{"failures":[{"source_id":"M00004"}],"not_attempted":["M00001"]}`), 0o600)
	again, err := relaunch(e).RetryInput(partial.RunID, "failed", "alice@contoso.org")
	if err != nil || strings.Join(ids(again), ",") != "M00002" {
		t.Errorf("after a restart and a planted manifest: %v (%v)", ids(again), err)
	}
	os.WriteFile(filepath.Join(partial.Root, copyManifestName), []byte(manifest), 0o600)

	// a signer mismatch is R-l; nothing is made
	_, err = e.RetryInput(partial.RunID, "failed", "bob@contoso.org")
	refusedWith(t, err, "R-l", []string{"alice@contoso.org", "bob@contoso.org", "Load folders again"})
	if got := lsTree(t, p1.Dest); !equalStrings(got, destBefore) {
		t.Errorf("a refused retry made something: %v -> %v", destBefore, got)
	}
	// the earlier export is byte-unchanged by the retry that ran
	p2, err := e.PlanCopyExport(*in)
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, e, p2, src)
	if after := treeFiles(t, partial.Root); !equalMaps(after, before) {
		t.Errorf("the earlier export changed: %v -> %v", before, after)
	}

	// cancelled with a retryable failure: failed again and the remaining are both offered
	e2, src2 := newCEEnv(t), newFakeSrc()
	var run *CopyRun
	src2.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		switch m.SourceID {
		case "M00002":
			return 0, &graph.StatusError{Code: 400, GraphCode: "ErrorMimeContentConversionFailed", Text: "x"}
		case "M00004":
			run.Cancel()
			<-ctx.Done()
			return 0, ctx.Err()
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	cp := cePlan(t, e2, src2, ceMsgs(6))
	run = e2.NewCopyRun(cp, src2)
	cres, err := run.Run(context.Background())
	if err != nil || cres.Status != CopyStatusCancelled {
		t.Fatalf("the cancelled run: %v %+v", err, cres)
	}
	if f, _ := e2.RetryInput(cres.RunID, "failed", ""); strings.Join(ids(f), ",") != "M00002" {
		t.Errorf("a cancelled run's failed messages: %v", ids(f))
	}
	if r, _ := e2.RetryInput(cres.RunID, "rest", ""); strings.Join(ids(r), ",") != "M00004,M00005,M00006" {
		t.Errorf("a cancelled run's remaining messages: %v", ids(r))
	}
	hist, _ := e2.History(nil, "")
	if c := hist[0]; !hasAction(c.Actions, ActExportFailed) || !hasAction(c.Actions, ActExportRemaining) {
		t.Errorf("a cancelled run with a retryable failure offers %v, want both", c.Actions)
	}

	// R-u2: 25 server errors in a row; the next part holds those 25 and the rest
	e3, src3 := newCEEnv(t), newFakeSrc()
	src3.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		if m.SourceID <= "M00025" {
			return 0, &graph.StatusError{Code: 500, Text: "x"}
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	sres := mustRun(t, e3, cePlan(t, e3, src3, ceMsgs(30)), src3)
	if sres.Stop == nil || sres.Stop.RID != "R-u2" {
		t.Fatalf("25 server errors ended as %s (%+v)", sres.Label, sres.Stop)
	}
	rest, err := e3.RetryInput(sres.RunID, "rest", "")
	if err != nil || len(rest.Messages) != 30 || rest.Messages[0].SourceID != "M00026" || rest.Messages[len(rest.Messages)-1].SourceID != "M00025" {
		t.Errorf("after R-u2 the next part holds %d messages (%v), want the 5 not attempted, then the 25 failed, the in-flight one last", len(rest.Messages), err)
	}
	if h, _ := e3.History(nil, ""); !hasAction(h[0].Actions, ActExportRemaining) || hasAction(h[0].Actions, ActExportFailed) {
		t.Errorf("a stopped run offers %v", h[0].Actions)
	}

	// a part marked gone: its whole plan minus what other sealed parts exported
	e4, src4 := newCEEnv(t), newFakeSrc()
	g := killedIn(t, e4, src4, "W9", ceMsgs(3))
	if _, err := e4.MarkGone(g.RunID); err != nil {
		t.Fatal(err)
	}
	gone, err := e4.RetryInput(g.RunID, "rest", "")
	if err != nil || len(gone.Messages) != 3 {
		t.Errorf("a gone part's remaining messages: %d (%v), want its whole plan of 3", len(gone.Messages), err)
	}
	if h, _ := e4.History(nil, ""); h[0].Label != "This folder is gone" || !hasAction(h[0].Actions, ActExportThese) || h[0].Remaining != 3 {
		t.Errorf("a gone run's card: %q %v remaining %d", h[0].Label, h[0].Actions, h[0].Remaining)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// covers: MA-530, R17, R29, S41, S52
// A stop for a sign-in that was cleared or changed in the dashboard aborts the message in
// flight and ends the run with R-z, naming both accounts: the run is finalized (manifest
// `failed`, marker STOPPED), the stop is sealed with the message that was in flight, and
// the run's record yields the remaining messages for a retry. It is never read as the
// user's Cancel.
func TestStopForSignInChangeEndsTheRunWithRZ(t *testing.T) {
	for _, tc := range []struct{ name, now, say string }{{"cleared", "", "now: signed out"}, {"changed", "bob@contoso.org", "now: bob@contoso.org"}} {
		e, src := newCEEnv(t), newFakeSrc()
		var run *CopyRun
		var once sync.Once
		src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
			if m.SourceID == "M00003" {
				w.Write([]byte("partial"))
				once.Do(func() { run.StopForSignInChange(tc.now) })
				<-ctx.Done()
				return 0, ctx.Err()
			}
			n, err := w.Write(ceBody(m.SourceID))
			return int64(n), err
		}
		p := cePlan(t, e, src, ceMsgs(6))
		run = e.NewCopyRun(p, src)
		res, err := run.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if res.Stop == nil || res.Stop.RID != "R-z" || res.Status != CopyStatusFailed || res.Outcome != CopyOutcomeFailed {
			t.Fatalf("%s: ended as %s (%+v), want R-z and failed, never cancelled", tc.name, res.Label, res.Stop)
		}
		assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("alice@contoso.org", tc.say, "Export the remaining messages"))
		if !strings.Contains(readText(t, res.Root, copyMarkerName), "STOPPED") || manifestOf(t, res.Root)["run"].(map[string]any)["status"] != "failed" {
			t.Errorf("%s: the folder is not finalized as stopped", tc.name)
		}
		if s := mustSummary(t, e, res.RunID).Sealed; s == nil || s.Stop == nil || s.Stop.RID != "R-z" || s.Stop.InFlight != "M00003" {
			t.Errorf("%s: sealed stop %+v, want R-z with M00003 in flight", tc.name, s)
		}
		rest, _ := e.RunRemaining(res.RunID)
		if len(rest) != 4 || rest[len(rest)-1].SourceID != "M00003" {
			t.Errorf("%s: %d messages remain, want M00004..M00006 and then M00003", tc.name, len(rest))
		}
	}
}

// covers: MA-448, R29, R12, S52
// The reconciler works off the request path: with a probe that blocks (a stat on a dead
// mount) Launch returns at once, the history is served without waiting, an unprobed run
// says it is looking for its folder, and a run that can never be probed in time is
// recorded as "the folder did not answer", not terminal, and probed again by Look again;
// each unresolved run is probed once per launch, however often the history is read.
func TestReconcilerRunsOffTheRequestPath(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	k := killedIn(t, e, src, "W4", ceMsgs(2))
	n := relaunch(e)

	// A probe that blocks for as long as the 3 s bound lets it: Launch and the history do
	// not wait for it, so they answer in well under that.
	release := make(chan struct{})
	var probes atomic.Int32
	restore := SetProbeHook(func(path string) {
		probes.Add(1)
		<-release
	}, 3*time.Second)
	rec := n.NewReconciler()
	began := time.Now()
	if err := rec.Launch(); err != nil {
		t.Fatal(err)
	}
	if time.Since(began) > 500*time.Millisecond {
		t.Fatalf("Launch waited %v for a blocked probe", time.Since(began))
	}
	for i := 0; i < 5; i++ {
		began = time.Now()
		hist, err := n.History(rec.Observations(), "")
		if err != nil || len(hist) != 1 || time.Since(began) > 500*time.Millisecond {
			t.Fatalf("the history while a probe blocks: %d runs in %v (%v)", len(hist), time.Since(began), err)
		}
		if !strings.Contains(hist[0].Label, "looking for its folder") {
			t.Errorf("an unprobed run says %q", hist[0].Label)
		}
	}
	close(release) // the stat answers
	rec.Wait()
	restore()
	obs := rec.Observations()[k.RunID]
	if obs.Kind != obsInterrupt || !obs.Terminal || !hasAction(obs.Actions, ActFinishAsPartial) {
		t.Fatalf("the probe's answer: %+v", obs)
	}
	if probes.Load() != 1 {
		t.Errorf("the run was probed %d times at launch, want once, however often the history was read", probes.Load())
	}

	// A probe that does not answer within its bound is recorded as such, is not terminal,
	// and is probed again by Look again.
	e2, src2 := newCEEnv(t), newFakeSrc()
	k2 := killedIn(t, e2, src2, "W4", ceMsgs(2))
	n2 := relaunch(e2)
	release2 := make(chan struct{})
	var probes2 atomic.Int32
	restore2 := SetProbeHook(func(path string) {
		probes2.Add(1)
		<-release2
	}, 100*time.Millisecond)
	rec2 := n2.NewReconciler()
	rec2.Launch()
	rec2.Wait()
	obs = rec2.Observations()[k2.RunID]
	if obs.Kind != obsNoAnswer || obs.Terminal || !strings.HasPrefix(obs.Label, "the folder did not answer (at ") || !hasAction(obs.Actions, ActLookAgain) {
		t.Fatalf("a probe past its bound: %+v", obs)
	}
	close(release2)
	again := rec2.LookAgain(k2.RunID)
	restore2()
	if again.Kind != obsInterrupt || !again.Terminal || !hasAction(again.Actions, ActFinishAsPartial) || probes2.Load() != 2 {
		t.Errorf("Look again: %+v after %d probes", again, probes2.Load())
	}
}
