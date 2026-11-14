package desktop

import (
	"context"
	"errors"
	"time"

	"mail-archive-tool/internal/wizard"
)

// Every stop waits for this process's running work (design-menubar §10.4 as MB-2
// amends it, R52). The one stop is Lifecycle.Stop: the Shut down button, the macOS
// terminate request (sign-out, an installer), Windows' end of session and Restart
// Manager's close request, and the OS's own termination (SIGTERM, Ctrl-C) all reach
// it, and serveInstance then waits for Drained. Drains are registered for every
// holder: the job slot (here), the Extraction workspace's export, the restore into
// Thunderbird, a delete under way (dashboard.go), and the wizard's export run in this
// process (wizard.WaitExports). A cancellable job is cancelled through the Desktop's
// own context (its context is a child of it) and the engine writes its record whole
// as "cancelled" (write-then-rename, state.WriteLastRun); a check or a rebuild is not
// cancellable and is waited for until the bound, after which the process ends and
// its record stays as a crash leaves it ("running", which `status` reads as a run
// that never finished), and the next run recovers the archive. Only a person's own
// stop (Shut down, and the menu's Quit in a later slice) asks first; the others
// never show a dialog.

// slotPoll is how often the job-slot drain looks whether the job has ended.
const slotPoll = 50 * time.Millisecond

// interruptedText is what a job in the slot ended by a stop says (MB-2 item 5).
const interruptedText = "Interrupted: MailArchive was closed while this ran. The archive is safe; the next backup finishes what this one left."

// drainSlot waits for the job in the slot to end, within ctx's budget (MB-2 item 4).
// The job's own context is already cancelled by the stop when it is cancellable; a
// job still running at the bound gets the interrupted line, and ctx's error is
// returned.
func (d *dashboard) drainSlot(ctx context.Context) error {
	c := d.cap
	for {
		c.mu.Lock()
		running := c.running
		c.mu.Unlock()
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			c.appendLine(interruptedText)
			return ctx.Err()
		case <-time.After(slotPoll):
		}
	}
}

// stopping reports whether err ended a job because the process is stopping: the
// Desktop's own context is done (MB-2).
func (d *dashboard) stopping(err error) bool {
	return err != nil && errors.Is(err, context.Canceled) && d.life.Context().Err() != nil
}

// The work a stop meets, each as the choice's "Now:" line names it and as the Quit
// question asks about it (the S1 friction review's words, F4 and F9; one constant each).
// The backup's "picks up where it left off" is the engine's: a cancelled run commits
// what it copied before it returns (app.Run's cancel path; R5, R2), its record reads
// "cancelled", and the next run copies only what is missing.
const (
	taskBackup      = "a backup is running"
	quitBackupText  = "A backup is running. Quit anyway? The backup stops; the next backup picks up where it left off."
	taskRestore     = "a restore to Thunderbird is running"
	quitRestoreText = "A restore to Thunderbird is running. Quit anyway? The restore stops and removes what it wrote, so nothing is added to Thunderbird."
	taskOther       = "a task is running"
	quitOtherText   = "A task is running. Quit anyway? The task stops and is recorded as interrupted."
	taskDelete      = "an archive is being deleted"
	quitDeleteText  = "An archive is being deleted. Quit anyway? MailArchive waits for the delete to finish first."
	taskExport      = "an export is running"
	quitExportText  = "An export is running. Quit anyway? The export stops and is recorded as cancelled."
	taskWizardCopy  = "the wizard is copying mail"
	quitWizCopyText = "The wizard is copying mail. Quit anyway? The copy stops; run the wizard again to finish it."
	// An open wizard is work too (the S1 adversarial pass A4, friction F5 and F9).
	taskWizardOpen  = "the wizard is open"
	quitWizOpenText = "The wizard is open. Quit anyway? The wizard closes, and its unanswered questions are dropped."
)

// busyTask names the work this process is doing — the choice's "Now:" words and the
// Quit question — or "" when none (MB-2 item 4: every holder, not only the job slot;
// MB-5). The task's kind is named where this slice knows it; slice S2's job-slot kind
// names the rest. An open wizard of this process counts (A4).
func (d *dashboard) busyTask() (what, question string) {
	c := d.cap
	c.mu.Lock()
	running, runNow, slot := c.running, c.runNow, c.slot
	c.mu.Unlock()
	switch {
	case running && d.rtb.isSlot(slot):
		return taskRestore, quitRestoreText
	case running && runNow:
		return taskBackup, quitBackupText
	case running:
		return taskOther, quitOtherText
	case d.running.any() != nil:
		return taskDelete, quitDeleteText
	case d.exportRunning():
		return taskExport, quitExportText
	case wizard.ExportsRunning():
		return taskWizardCopy, quitWizCopyText
	case wizard.OpenHere():
		return taskWizardOpen, quitWizOpenText
	}
	return "", ""
}

// exportRunning reports whether the Extraction workspace's export is running.
func (d *dashboard) exportRunning() bool {
	if d.ex == nil {
		return false
	}
	d.ex.mu.Lock()
	s := d.ex.slot
	d.ex.mu.Unlock()
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.finished && !s.released
}

// quitQuestion is the question a person's own stop asks while work runs (the fold's
// first contradiction: no archive name, like the choice window), and "" when nothing
// runs. Keep running is the default answer.
func (d *dashboard) quitQuestion() string {
	_, q := d.busyTask()
	return q
}

// nowTask is what runs, for the choice's "Now:" line, "" when nothing does.
func (d *dashboard) nowTask() string {
	what, _ := d.busyTask()
	return what
}

// instanceDashboardMade, when set (a test), is told the dashboard an instance made, so a
// test can hold a job in its slot and drive a stop path (MA-1424).
var instanceDashboardMade func(*dashboard)

// shutdownNothingRanText is what Quit says when the work it asked about had ended by
// the time the person answered (the S1 friction review, F4).
const shutdownNothingRanText = "Nothing was running any more, so MailArchive stopped. Your archives and schedules are untouched."

// The answers to the Quit question (Q4: Keep running is the default).
const (
	keepRunningLabel = "Keep running"
	quitLabel        = "Quit"
)
