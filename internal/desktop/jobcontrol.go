package desktop

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
)

// A keyed backup in the job slot has a context of its own (program design R20): Cancel
// ends the job — the archive is left as a crash leaves it, which the next run's intent
// recovers from — and "Skip the program download" ends only the program step's wait
// for its download, offered only while that wait is on (it can hold the archive lock up
// to ten minutes). Shut down cancels it too, through the Desktop's own context.

// keyedJob is what a keyed backup run in the job slot is given.
type keyedJob struct {
	ctx    context.Context
	skip   <-chan struct{}
	onWait func(waiting bool)
}

// runKeyedJobOn runs fn as runJobOn does, with a context and a skip of its own.
func (d *dashboard) runKeyedJobOn(out string, fn func(j keyedJob, logger *log.Logger) (string, error)) error {
	ctx, cancel := context.WithCancel(d.life.Context())
	skip := make(chan struct{})
	c := d.cap
	err := d.runJobOn(out, func(logger *log.Logger) (string, error) {
		defer cancel()
		c.mu.Lock()
		c.cancel, c.skip, c.skipped = cancel, skip, false
		c.mu.Unlock()
		result, err := fn(keyedJob{ctx: ctx, skip: skip, onWait: c.setWaiting}, logger)
		if errors.Is(err, context.Canceled) && ctx.Err() != nil && d.life.Context().Err() == nil {
			// The person's Cancel, not a failure (the PG4 pass, CMP-1).
			return "The backup was cancelled. The archive is safe; the next backup finishes what this one left.", nil
		}
		return result, err
	})
	if err != nil {
		cancel()
	}
	return err
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

// cancelJobAction ends the keyed backup running in the job slot.
func (d *dashboard) cancelJobAction(w http.ResponseWriter, r *http.Request) {
	c := d.cap
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "Nothing is running that can be cancelled."})
		return
	}
	cancel()
	c.appendLine("Cancelled from MailArchive Desktop: the backup stops at its next safe point; the next backup finishes what it left.")
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
