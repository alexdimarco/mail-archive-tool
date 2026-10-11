package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
)

// More of the friction review's fixes in the engine's words (F7, F8, F12, F13, F17, F22).

// covers: MA-412, MA-518, R29, R12, S52
// F17: the preview raises the sentence Start would refuse with. A destination with too little
// free space gets R-g2's sentence as a warning, one that cannot be written to gets R-w's, and
// R-g2 shows enough digits for its two numbers to differ. The positive twin first: a roomy,
// writable destination warns of neither.
func TestFricPreviewFlagsADestinationThatWillNotWork(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := func() *CopyPlan {
		t.Helper()
		return cePlanAt(t, e, src, ceMsgs(3), dest)
	}
	warned := func(p *CopyPlan, want string) bool { return strings.Contains(strings.Join(p.Warnings, "\n"), want) }

	if p := plan(); warned(p, "free, but this export needs") || warned(p, "couldn't create a folder") {
		t.Fatalf("a roomy, writable destination was warned about: %v", p.Warnings)
	}

	e.SetFreeBytes(func(string) (uint64, error) { return 1000, nil }, nil)
	p := plan()
	for _, want := range []string{"free, but this export needs", "plus room to finish", "Free some space or choose another destination"} {
		if !warned(p, want) {
			t.Errorf("a destination with too little room: the preview's warnings lack %q: %v", want, p.Warnings)
		}
	}
	e.SetFreeBytes(nil, nil)

	// the same sentence at Start (the preview's words are Start's words)
	if want := refusalG2(dest, 1000, p.EstimateBytes+p.Reserve).Message; !warned(p, want) {
		t.Errorf("the preview's sentence is not Start's: want %q in %v", want, p.Warnings)
	}

	// both numbers rounded to "80.6 kB": the bytes are shown instead
	msg := refusalG2(dest, 80_612, 80_640).Message
	assure.Refused(t, 1, msg, assure.Code(1), assure.Names("80,612 bytes", "80,640 bytes", "Free some space or choose another destination"))

	// a destination that cannot be written to
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a read-only folder is detected only for a user who is not root, on a system with permission bits")
	}
	if err := os.Chmod(dest, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dest, 0o755) })
	p = plan()
	for _, want := range []string{"couldn't create a folder in", "write-protected", "Nothing was written"} {
		if !warned(p, want) {
			t.Errorf("a read-only destination: the preview's warnings lack %q: %v", want, p.Warnings)
		}
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("the preview wrote into the destination: %v", entries)
	}
}

// covers: MA-438, MA-430, R12, S52
// F8: what the owner reads is what happened, not the system call that met it. A missing
// folder, a write-protected one and a full disk are said in words (the system's own text stays
// in brackets for the full disk); R-w puts this computer's advice first and keeps the other; R-g
// with nothing to add has no empty brackets; and a run cut off by the sign-in lists the message
// that was in flight as not tried again, not as a failed message.
func TestFricOwnerTextNamesWhatHappenedNotTheSystemCall(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want []string
		not  []string
	}{
		{"gone", &fs.PathError{Op: "openat", Path: "x", Err: syscall.ENOENT}, []string{"the folder isn't there any more"}, []string{"openat", "no such file"}},
		{"write-protected", &fs.PathError{Op: "mkdirat", Path: "x", Err: syscall.EACCES}, []string{"write-protected", ": permission denied"}, []string{"mkdirat", "(permission denied)"}},
		{"full", &fs.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}, []string{"the disk is full", "no space left"}, []string{"write:"}},
		{"other", &fs.PathError{Op: "write", Path: "x", Err: syscall.EIO}, []string{"input/output error"}, []string{"write:"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := leaveName(c.err)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("leaveName = %q, want %q", got, w)
				}
			}
			for _, n := range c.not {
				if strings.Contains(got, n) {
					t.Errorf("leaveName = %q still says %q", got, n)
				}
			}
		})
	}

	w := refusalW(`E:\Exports`, "the folder is write-protected").Message
	local, other := "System Settings", "Controlled folder access"
	if runtime.GOOS == "windows" {
		local, other = other, local
	}
	if runtime.GOOS != "linux" && (!strings.Contains(w, local) || !strings.Contains(w, other) || strings.Index(w, local) > strings.Index(w, other)) {
		t.Errorf("R-w does not lead with this computer's advice (%s first, then %s): %q", local, other, w)
	}
	plan := &CopyPlan{Dest: `E:\Exports`, Source: CopySourceInfo{Kind: "m365", Mailbox: "alex@contoso.com"}}
	if g := refusalText("R-g", 3, 7, "", plan, "state"); strings.Contains(g, "()") || !strings.Contains(g, "full or no longer available") {
		t.Errorf("R-g with nothing to add: %q", g)
	}

	// a run cut off by the sign-in: the in-flight message is not a failed message
	e := newCEEnv(t)
	csrc := newFakeSrc()
	csrc.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		return 0, &graph.StatusError{Code: 401, Text: "x"}
	}
	csrc.refresh = func(ctx context.Context) error { return errors.New("the refresh token was refused") }
	res := mustRun(t, e, cePlan(t, e, csrc, ceMsgs(4)), csrc)
	if res.Stop == nil || res.Stop.RID != "R-c" {
		t.Fatalf("the run ended as %s (%+v)", res.Label, res.Stop)
	}
	hist, err := relaunch(e).History(nil, "")
	if err != nil || len(hist) != 1 {
		t.Fatalf("history: %v %v", hist, err)
	}
	if c := hist[0]; c.Exported != 0 || c.Failed != 0 || c.NotAttempted != 4 {
		t.Errorf("a sign-in refusal is counted as a bad message: %d exported, %d failed, %d not attempted", c.Exported, c.Failed, c.NotAttempted)
	}
}

// covers: MA-427, R29, R5, R12
// F8: "the export stopped after N of M messages" counts the messages that were exported. Five
// write failures in a row stop a run that exported nothing: it says "after 0 of 8", not "after
// 5 of 8" or "8 of 8", and says nothing of a system call. The positive twin: a run that
// exported four before the failures says four.
func TestFricAStopCountsTheMessagesExportedNotAttempted(t *testing.T) {
	run := func(okFirst int) *CopyResult {
		e, src := newCEEnv(t), newFakeSrc()
		fake := newFaultFS()
		n := 0
		fake.placeErr = func(dir, tmp, final string) error {
			if !strings.HasPrefix(dir, "messages") {
				return nil
			}
			n++
			if n > okFirst {
				return &os.LinkError{Op: "rename", Err: syscall.EPERM}
			}
			return nil
		}
		e.fsWrap = func(f ceFS) ceFS { fake.ceFS = f; return fake }
		return mustRun(t, e, cePlan(t, e, src, ceMsgs(12)), src)
	}
	for _, c := range []struct {
		okFirst int
		want    string
	}{{4, "after 4 of 12 messages"}, {0, "after 0 of 12 messages"}} {
		res := run(c.okFirst)
		if res.Stop == nil || res.Stop.RID != "R-g" {
			t.Fatalf("%d first: %s (stop %+v), want R-g", c.okFirst, res.Label, res.Stop)
		}
		assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names(c.want, "operation not permitted", "Export the remaining messages"),
			assure.Forbid("rename:", "openat", "mkdirat"))
	}
}

// covers: MA-430, MA-448, R12, S52
// F8: a stopped run's card says why it stopped and what to do, also once the reconciler has
// looked at its folder (its label is then the observation's, which used to replace the stop's
// words); F13: an interrupted card says what to do next, the first of three clicks, and a card
// whose export was fully written says it needs one; F12: Check this export is offered on that
// card, a RED says what to do, and a folder that was never finished says so.
func TestFricInterruptedAndFinishedWritingCardsSayWhatToDo(t *testing.T) {
	// an interrupted run that stopped for the destination (a wedged run records R-g, never ends)
	e, res, _ := killedRun(t, "W6:1", 4)
	n := relaunch(e)
	if _, err := n.SweepRunRecords(); err != nil {
		t.Fatal(err)
	}
	if err := n.WedgeStop(res.RunID); err != nil {
		t.Fatal(err)
	}
	obs, err := n.ReconcileRun(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	hist, err := n.History(map[string]CopyObservation{res.RunID: obs}, "")
	if err != nil || len(hist) != 1 {
		t.Fatalf("history: %v %v", hist, err)
	}
	c := hist[0]
	if !contains(c.Actions, ActFinishAsPartial) {
		t.Fatalf("the interrupted card: %q %v", c.Label, c.Actions)
	}
	if c.Stop == nil || !strings.Contains(c.Stop.Message, "destination is full or no longer available") || !strings.Contains(c.Stop.Message, "Export the remaining messages") {
		t.Errorf("the observation replaced the stop's words: %+v", c.Stop)
	}
	for _, want := range []string{"To continue", "Finish as partial", "Export the remaining messages", "Start export"} {
		if !strings.Contains(c.Next, want) {
			t.Errorf("the interrupted card does not say %q: %q", want, c.Next)
		}
	}

	// a folder that was never finished is checked: it says so, and what to do
	chk, err := n.CheckExport(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if chk.OK || !strings.Contains(strings.Join(chk.Problems, " "), "unfinished") || !strings.Contains(chk.Remedy, "Do not hand this folder over") {
		t.Errorf("Check on a folder that was never finished: %+v", chk)
	}

	// an export that was fully written but whose last step is owed
	e2, res2, _ := killedRun(t, "W9a", 3)
	n2 := relaunch(e2)
	if _, err := n2.SweepRunRecords(); err != nil {
		t.Fatal(err)
	}
	obs2, err := n2.ReconcileRun(res2.RunID)
	if err != nil {
		t.Fatal(err)
	}
	hist2, _ := n2.History(map[string]CopyObservation{res2.RunID: obs2}, "")
	c2 := hist2[0]
	if !strings.HasPrefix(c2.Label, "Finished writing (Complete)") || !strings.Contains(c2.Label, "click Check and finish") || strings.Contains(c2.Label, "marker") {
		t.Errorf("the finished-writing label: %q", c2.Label)
	}
	if !contains(c2.Actions, ActCheckAndFinish) || !contains(c2.Actions, ActCheckThis) {
		t.Errorf("the finished-writing card offers %v, want Check and finish and Check this export", c2.Actions)
	}
	if !strings.Contains(c2.Next, "Check and finish") {
		t.Errorf("the finished-writing card does not say what to click: %q", c2.Next)
	}

	// a RED says what to do; a GREEN says nothing
	e3, src := newCEEnv(t), newFakeSrc()
	r3 := mustRun(t, e3, cePlan(t, e3, src, ceMsgs(3)), src)
	if g, err := e3.CheckExport(r3.RunID); err != nil || !g.OK || g.Remedy != "" {
		t.Fatalf("a clean export: %+v %v", g, err)
	}
	victim := filepath.Join(r3.Root, filepath.FromSlash(emlOf(t, r3.Root)))
	orig, _ := os.ReadFile(victim)
	os.WriteFile(victim, append(orig, 'x'), 0o600)
	red, err := e3.CheckExport(r3.RunID)
	if err != nil || red.OK || !strings.Contains(red.Remedy, "Do not hand this folder over") || !strings.Contains(red.Remedy, "export the messages again") {
		t.Errorf("a changed file: %+v %v", red, err)
	}
}

// covers: MA-430, MA-448, R12, S52
// F12/F13, the branch the test above never reached: a card whose export was fully written
// before the app was killed, read before the reconciler has looked at its folder (no
// observation yet, which is what the page shows first after a restart), already offers Check
// this export beside Check and finish and says what to click. The positive twin first: a
// finished export's card offers neither.
func TestFricFinishedWritingCardBeforeTheReconcilerOffersTheChecks(t *testing.T) {
	e0, src := newCEEnv(t), newFakeSrc()
	done := mustRun(t, e0, cePlan(t, e0, src, ceMsgs(2)), src)
	hist0, err := e0.History(nil, "")
	if err != nil || len(hist0) != 1 || contains(hist0[0].Actions, ActCheckAndFinish) || hist0[0].Next != "" {
		t.Fatalf("a finished export's card: %+v %v", hist0, err)
	}
	_ = done

	e, res, _ := killedRun(t, "W9a", 3)
	n := relaunch(e)
	if _, err := n.SweepRunRecords(); err != nil {
		t.Fatal(err)
	}
	hist, err := n.History(nil, "")
	if err != nil || len(hist) != 1 || hist[0].Run != res.RunID {
		t.Fatalf("history: %v %v", hist, err)
	}
	c := hist[0]
	if !strings.HasPrefix(c.Label, "Finished writing") {
		t.Fatalf("the card before the reconciler: %q %v", c.Label, c.Actions)
	}
	if !contains(c.Actions, ActCheckAndFinish) || !contains(c.Actions, ActCheckThis) {
		t.Errorf("the finished-writing card (no observation) offers %v, want Check and finish and Check this export", c.Actions)
	}
	if !strings.Contains(c.Next, "Check and finish") {
		t.Errorf("the finished-writing card (no observation) does not say what to click: %q", c.Next)
	}
}

// covers: MA-422, MA-430, R29, S52
// F7, F13, F22: the README of an export with failures names them (count, the manifest's
// "failures" list, the first few with subject, folder and reason), lists the selected folders,
// opens (after the run-id line the identity check reads) with status and counts and puts how to
// check it before the technical notes, and its status line reads as a sentence; the card's
// "Which ones?" has the same messages with their reasons.
func TestFricReadmeAndCardListTheMessagesThatWereNotExported(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	failIDs(src, map[string]bool{"M00002": true})
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(4)), src)
	readme := readText(t, res.Root, copyReadmeName)
	for _, want := range []string{
		"Selected folders: Inbox (4)", "Messages that were not exported: 1", `under "failures"`, "subject M00002 (Inbox):",
		"Status: finished with problems — 1 messages not exported. This status is valid only while no " + copyMarkerName,
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q:\n%s", want, readme)
		}
	}
	if !strings.HasPrefix(readme, copyRunLinePrefix+res.RunID+"\n") {
		t.Errorf("README does not start with the run-id line the identity check reads")
	}
	if strings.Index(readme, "How to check this export") > strings.Index(readme, "What the files are") || strings.Index(readme, "Counts:") > strings.Index(readme, "How to check this export") {
		t.Error("README puts the technical notes before how to check the export")
	}
	if strings.Contains(readme, "— and valid only") {
		t.Error("README's status line still reads as a run-on")
	}
	hist, err := e.History(nil, "")
	if err != nil || len(hist) != 1 {
		t.Fatalf("history: %v %v", hist, err)
	}
	c := hist[0]
	if len(c.Failures) != 1 || c.Failures[0].Subject != "subject M00002" || c.Failures[0].Folder != "Inbox" || c.Failures[0].Reason == "" || c.FailuresMore != 0 {
		t.Errorf("the card's \"Which ones?\": %+v (more %d)", c.Failures, c.FailuresMore)
	}
	if plainStatus("Stopped: the app stopped while this export was running (finished as partial)") != "stopped — the app stopped while this export was running (finished as partial)" {
		t.Errorf("plainStatus: %q", plainStatus("Stopped: the app stopped while this export was running (finished as partial)"))
	}
}
