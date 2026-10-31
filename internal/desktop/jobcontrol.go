package desktop

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"mail-archive-tool/internal/presync"
)

// A keyed backup in the job slot has a context of its own (program design R20): Cancel
// ends the job — the archive is left as a crash leaves it, which the next run's intent
// recovers from — and "Skip the program download" ends only the program step's wait
// for its download, offered only while that wait is on (it can hold the archive lock up
// to ten minutes). Shut down cancels it too, through the Desktop's own context.

// keyedJob is what a cancellable backup run in the job slot is given: its own
// context (Cancel ends it with presync.ErrCancelAndQuit as the cause), the
// Desktop's own context as its Stop (Shut down ends it: the Thunderbird sync stops
// at once and asks nothing to quit, design-presync-attended §3.1), and the program
// step's skip.
type keyedJob struct {
	ctx    context.Context
	stop   context.Context
	skip   <-chan struct{}
	onWait func(waiting bool)
	// cancel ends ctx with a cause: the encrypted Run now's Lock window uses it
	// (errLockedDuringSync, design-presync-attended §3.2).
	cancel context.CancelCauseFunc
}

// cancelledText is what a backup the person cancelled says (the PG4 pass, CMP-1).
const cancelledText = "The backup was cancelled. The archive is safe; the next backup finishes what this one left."

// runCancellableJobOn runs fn as runJobOn does, with a context and a skip of its own:
// both Run nows, plaintext and encrypted, and an encrypted archive's first backup
// (design-presync-attended §3.1, generalised from the keyed-only slot).
func (d *dashboard) runCancellableJobOn(out string, fn func(j keyedJob, logger *log.Logger) (string, error)) error {
	return d.runCancellableJobOnClaiming(out, nil, fn)
}

// runCancellableJobOnClaiming is runCancellableJobOn with claimed called under the
// slot's lock as the job takes the slot (runJobOnClaiming): the encrypted Run now's
// Lock window.
func (d *dashboard) runCancellableJobOnClaiming(out string, claimed func(), fn func(j keyedJob, logger *log.Logger) (string, error)) error {
	ctx, cancel := context.WithCancelCause(d.life.Context())
	skip := make(chan struct{})
	c := d.cap
	err := d.runJobOnClaiming(out, claimed, func(logger *log.Logger) (string, error) {
		defer cancel(nil)
		c.mu.Lock()
		c.cancel, c.skip, c.skipped = cancel, skip, false
		c.mu.Unlock()
		result, err := fn(keyedJob{ctx: ctx, stop: d.life.Context(), skip: skip, onWait: c.setWaiting, cancel: cancel}, logger)
		if errors.Is(err, context.Canceled) && ctx.Err() != nil && d.life.Context().Err() == nil {
			// The person's Cancel, not a failure (the PG4 pass, CMP-1).
			return cancelledText, nil
		}
		return result, err
	})
	if err != nil {
		cancel(nil)
	}
	return err
}

// runKeyedJobOn is runCancellableJobOn for a keyed backup.
func (d *dashboard) runKeyedJobOn(out string, fn func(j keyedJob, logger *log.Logger) (string, error)) error {
	return d.runCancellableJobOn(out, fn)
}

// programLinePrefix begins the one line a backup logs when it wrote or removed copies of
// MailArchive in the archive (app's ProgramOutcome.Summary, P3-8 friction review, B23).
const programLinePrefix = "Copies of MailArchive: "

// programLine passes a run's log through, keeping the program step's one line.
type programLine struct {
	w    io.Writer
	mu   sync.Mutex
	line string
}

func (p *programLine) Write(b []byte) (int, error) {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, programLinePrefix) {
		p.mu.Lock()
		p.line = s
		p.mu.Unlock()
	}
	return p.w.Write(b)
}

// withProgramLine runs a keyed backup on logger and ends its result with the line saying
// the copies of MailArchive it wrote or removed — tens of megabytes on the archive's
// drive — when it changed any (B23): the wizard's run says only its counts. A result
// that already carries the line is left as it is.
func withProgramLine(logger *log.Logger, run func(*log.Logger) (string, error)) (string, error) {
	p := &programLine{w: logger.Writer()}
	res, err := run(log.New(p, logger.Prefix(), logger.Flags()))
	p.mu.Lock()
	line := p.line
	p.mu.Unlock()
	if err != nil || line == "" || strings.Contains(res, line) {
		return res, err
	}
	return strings.TrimSpace(res+" "+line) + ".", nil
}

func (c *captureState) setWaiting(waiting bool) {
	c.mu.Lock()
	c.waiting = waiting
	c.mu.Unlock()
}

// cancelJobAction ends the backup running in the job slot. Its cause is the Cancel
// button's, presync.ErrCancelAndQuit (design-presync-attended §3.1): during a Run
// now's Thunderbird sync that skips the whole backup and leaves the Thunderbird it
// started open (never-close, every platform until slice AS-Q); while it reads, it is
// today's cancel. The line it adds is chosen by the sync's phase.
func (d *dashboard) cancelJobAction(w http.ResponseWriter, r *http.Request) {
	c := d.cap
	c.mu.Lock()
	cancel := c.cancel
	phase := ""
	if c.runNow && !c.syncEnded {
		phase = c.syncPhase
	}
	c.mu.Unlock()
	if cancel == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "Nothing is running that can be cancelled."})
		return
	}
	cancel(presync.ErrCancelAndQuit)
	if phase == presync.PhaseStarting || phase == presync.PhaseWaiting {
		c.appendLine("Cancelled from MailArchive Desktop: the whole backup is skipped; the Thunderbird it started is left open.")
	} else {
		c.appendLine("Cancelled from MailArchive Desktop: the backup stops at its next safe point; the next backup finishes what it left.")
	}
	writeJSON(w, map[string]any{"ok": true})
}

// skipProgramAction ends only the program step's wait for its download: the backup goes
// on with this computer's own copy, and the download carries on for the next one.
func (d *dashboard) skipProgramAction(w http.ResponseWriter, r *http.Request) {
	c := d.cap
	c.mu.Lock()
	ok := c.skip != nil && c.waiting && !c.skipped
	if ok {
		c.skipped = true
		close(c.skip)
	}
	c.mu.Unlock()
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": "No download of MailArchive is being waited for."})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
