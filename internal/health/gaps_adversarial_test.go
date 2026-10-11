package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// The status tombstones of the FI-S3 adversarial pass
// (docs/review-fault-isolation-s3-adversarial.md).

const inboxDeletionNote = "deleted messages in Inbox are not being detected while this persists; after saving a copy, move or delete the item out of Inbox in the mailbox to resume deletion tracking there"

// covers: MA-326, R18, S48
// S3-AI3 (a): OR10 says status names a persisting deferral of gone-detection. A
// TERMINAL gap that defers it (an export-panic item no record matched) can never be
// "stuck" (only fillable gaps escalate), so the sentence reached the owner for no
// terminal gap and the sweep stayed off, for as long as the item persisted, in
// silence. The terminal line now carries it, once per folder; a terminal gap that
// defers nothing, and a fillable gap that is not stuck yet, say nothing of it.
func TestStatusTerminalGapThatDefersGoneNamesIt(t *testing.T) {
	panicked := func(loc string, defers bool) state.CaptureGap {
		g := fetchFailed(loc)
		g.Reason, g.Class, g.DefersGone = "export-panic", state.ClassTerminal, defers
		return g
	}
	rep := Assess(gapInput(panicked("graph:P1", true), panicked("graph:P2", true)), gapNow)
	i := codeIndex(rep, "capture_gaps_terminal")
	if i < 0 {
		t.Fatalf("no terminal line in %v", rep.Codes)
	}
	if line := rep.Reasons[i]; strings.Count(line, inboxDeletionNote) != 1 {
		t.Errorf("the terminal line must carry the deletion-tracking sentence once: %q", line)
	}
	if rep.Posture != "WARN" {
		t.Errorf("the posture is %s, want WARN: the sentence never makes a gap RED", rep.Posture)
	}

	rep = Assess(gapInput(panicked("graph:P3", false)), gapNow)
	if line := rep.Reasons[codeIndex(rep, "capture_gaps_terminal")]; strings.Contains(line, "deletion tracking") {
		t.Errorf("a terminal gap that defers nothing says %q", line)
	}
}

// covers: MA-326, R18, S48
// S3-AI3 (b), second round: a stuck FOLDER-scope gap that defers gone-detection (the
// archive could not create the folder's directory) is a local problem with no item
// in it to move or delete, and its remedy already says to make the archive location
// writable. The deletion-tracking sentence it carries must say what it costs and when
// it ends, not send the owner to move or delete mail in the mailbox over a disk fault.
// An item gap keeps the sentence it always had.
func TestStatusStuckFolderUnwritableGapDoesNotSendTheOwnerToMoveMail(t *testing.T) {
	folderGap := func() state.CaptureGap {
		g := fetchFailed("folder:Inbox")
		g.Scope, g.Reason, g.Subject, g.Received, g.Detail = state.GapScopeFolder, "folder-unwritable", "", "", "the folder's directory could not be created"
		g.Runs, g.FirstRun, g.DefersGone = 6, gapNow.Add(-10*24*time.Hour), true
		return g
	}
	rep := Assess(gapInput(folderGap()), gapNow)
	i := codeIndex(rep, "capture_gaps_stuck")
	if i < 0 {
		t.Fatalf("no stuck line in %v", rep.Codes)
	}
	line := rep.Reasons[i]
	if !strings.Contains(line, "deleted messages in Inbox are not being detected while this persists") {
		t.Errorf("the stuck folder line must still say deletion tracking is off: %q", line)
	}
	for _, bad := range []string{"move or delete the item", "after saving a copy"} {
		if strings.Contains(line, bad) {
			t.Errorf("a stuck folder-unwritable line says %q, advice for a message in a mailbox and not for a local directory: %q", bad, line)
		}
	}
	if !strings.Contains(line, "writable") || !strings.Contains(line, "resumes once") {
		t.Errorf("the stuck folder line must say deletion tracking resumes once the archive location is writable: %q", line)
	}

	item := fetchFailed("graph:N1")
	item.Runs, item.FirstRun, item.DefersGone = 6, gapNow.Add(-10*24*time.Hour), true
	rep = Assess(gapInput(item), gapNow)
	if line := rep.Reasons[codeIndex(rep, "capture_gaps_stuck")]; strings.Count(line, inboxDeletionNote) != 1 {
		t.Errorf("an item gap keeps its sentence: %q", line)
	}
}

// covers: MA-326, R18, S48
// S3-AI5: a Graph item that is listed on every run and answers 404 on every $value
// is classed vanished-at-fetch, whose words say the message was deleted while the
// run was reading it and that the row closes on the next run. Once the gap is stuck
// (it failed on three runs over three days) those words are false: the item is
// still in the mailbox, the service will not export it, and the row stays. The stuck
// line, the headline and the list say so and give the way to save a copy; a gap that
// is not stuck yet keeps the first-time words.
func TestStatusStuckVanishedAtFetchDoesNotPromiseTheRowCloses(t *testing.T) {
	vanished := func(stuck bool) state.CaptureGap {
		g := fetchFailed("graph:V")
		g.Reason, g.Subject = "vanished-at-fetch", "subj-V"
		if stuck {
			g.Runs, g.FirstRun = 6, gapNow.Add(-10*24*time.Hour)
		}
		return g
	}

	in := gapInput(vanished(false))
	if line := gapLines(in, gapNow)[0]; !strings.Contains(line, "deleted from the mailbox while the run was reading it") || !strings.Contains(line, "the row closes on the next run") {
		t.Errorf("a first-time vanished-at-fetch gap keeps its words: %q", line)
	}

	in = gapInput(vanished(true))
	rep := Assess(in, gapNow)
	i := codeIndex(rep, "capture_gaps_stuck")
	if i < 0 {
		t.Fatalf("no stuck line in %v", rep.Codes)
	}
	lines := append([]string{rep.Reasons[i]}, gapLines(in, gapNow)...)
	for _, line := range lines {
		for _, bad := range []string{"closes on the next run", "deleted from the mailbox while the run was reading it"} {
			if strings.Contains(line, bad) {
				t.Errorf("a stuck vanished-at-fetch gap says %q: %q", bad, line)
			}
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "still lists") || !strings.Contains(strings.Join(lines, "\n"), "save a copy from Outlook on the web") {
		t.Errorf("the stuck words must say the mailbox still lists the item and how to save a copy: %q", lines)
	}
}
