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

// busyTask names the work this process is doing, for the Quit question, or "" when
// none (MB-2 item 4: every holder, not only the job slot; MB-5). The task's kind is
// named where this slice knows it; slice S2's job-slot kind names the rest.
func (d *dashboard) busyTask() (what, after string) {
	c := d.cap
	c.mu.Lock()
	running, runNow, slot := c.running, c.runNow, c.slot
	c.mu.Unlock()
	switch {
	case running && d.rtb.isSlot(slot):
		return "a restore to Thunderbird is running", "The restore stops and removes what it wrote, so nothing is added to Thunderbird."
	case running && runNow:
		return "a backup is running", "The backup stops and is recorded as interrupted."
	case running:
		return "a task is running", "The task stops and is recorded as interrupted."
	case d.running.any() != nil:
		return "an archive is being deleted", "MailArchive waits for the delete to finish first."
	case d.exportRunning():
		return "an export is running", "The export stops and is recorded as cancelled."
	case wizard.ExportsRunning():
		return "the wizard is copying mail", "The copy stops and is recorded as interrupted; running the wizard again finishes it."
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
	what, after := d.busyTask()
	if what == "" {
		return ""
	}
	return "MailArchive is busy (" + what + "). Quit anyway? " + after
}

// instanceDashboardMade, when set (a test), is told the dashboard an instance made, so a
// test can hold a job in its slot and drive a stop path (MA-1424).
var instanceDashboardMade func(*dashboard)

// The answers to the Quit question (Q4: Keep running is the default).
const (
	keepRunningLabel = "Keep running"
	quitLabel        = "Quit"
)
