package desktop

import (
	"context"
	"errors"
	"log"
	"sync"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/wizard"
)

// The encrypted Run now, attended, with its Lock window (design-presync-attended
// §3.2, §8, §9, slice AS8; owner rulings 4, OQ-3, OQ-4, OQ-8; C-11, C-13, C-21, C-75,
// C-76, C-81, C-85). From the moment Run now takes its write lease until right before
// the run reads its first source, the person's Lock of the archive cancels the run and
// clears its key — for every encrypted Run now, whether or not its job syncs
// Thunderbird (OQ-8). After that the run goes on with its own key, as phase 3 §3.2
// says, and the Lock reply says so. The idle lock and a switch never cancel it.

// errLockedDuringSync is the cause a Lock cancels an encrypted Run now with: not the
// Cancel button's presync.ErrCancelAndQuit, so nothing is asked to quit (§3.2).
var errLockedDuringSync = errors.New("the archive was locked before this backup started reading mail")

// syncWindow is one encrypted Run now's Lock window (§3.2, C-11): its archive and
// lease, and, once the job function has started, the run's cancel and Stop. stopped
// says a Lock has cancelled it. Every field is read and written under captureState.mu.
type syncWindow struct {
	out     string
	lease   *keyring.Unlocked
	cancel  context.CancelCauseFunc
	stopRun context.CancelFunc
	stopped bool
}

// openWindow opens the Lock window of an encrypted Run now on out as its lease is
// taken (§3.2, OQ-8): before any job file is read, so it never depends on the sync
// flag (C-75 as superseded). A job already in the slot keeps its own window; this one
// is then not registered, and the Run now is refused by the slot anyway.
func (c *captureState) openWindow(out string, lease *keyring.Unlocked) *syncWindow {
	w := &syncWindow{out: out, lease: lease}
	c.mu.Lock()
	if c.lockWin == nil && !c.running {
		c.lockWin = w
	}
	c.mu.Unlock()
	return w
}

// bindWindow hands the window the run's cancel and Stop once its job function has
// started. It reports whether a Lock already cancelled the window, in which case the
// run is stopped now.
func (c *captureState) bindWindow(w *syncWindow, cancel context.CancelCauseFunc, stopRun context.CancelFunc) {
	c.mu.Lock()
	w.cancel, w.stopRun = cancel, stopRun
	stopped := w.stopped
	c.mu.Unlock()
	if stopped {
		stopRun()
		cancel(errLockedDuringSync)
	}
}

// closeWindow closes the window, under c.mu (C-11, C-75): at read start, and in the
// Run now job function's defer, which covers every return that never reaches it.
func (c *captureState) closeWindow(w *syncWindow) {
	c.mu.Lock()
	if c.lockWin == w {
		c.lockWin = nil
	}
	c.mu.Unlock()
}

// stopSyncWait is the person's Lock of out reaching a Run now (§3.2, §9): with the
// window open, under c.mu, it ends the run's Stop and cancels it with
// errLockedDuringSync — nothing is asked to quit — locks its lease, so no new use of
// the key starts and the key goes when the run lets go of it, and returns the reply's
// note; with the window closed and an encrypted Run now of out reading, the note says
// it finishes with its own key. "" when no Run now of out runs.
func (c *captureState) stopSyncWait(out, goos string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w := c.lockWin; w != nil && samePath(w.out, out) {
		w.stopped = true
		if w.stopRun != nil {
			w.stopRun()
		}
		if w.cancel != nil {
			w.cancel(errLockedDuringSync)
		}
		w.lease.Lock()
		phase := ""
		if c.runNow && !c.syncEnded {
			phase = c.syncPhase
		}
		return lockCancelNote(goos, phase, c.runNow && c.syncEnded && !c.syncNoStart, c.syncLeft, c.syncMinimized)
	}
	if c.running && c.runNow && c.encrypted && samePath(c.jobOut, out) {
		return lockReadingText
	}
	return ""
}

// The Lock reply's words (§9, C-13, C-21, C-76, OQ-3, OQ-8): the key is cleared once
// the run's current step ends, never "already gone".
const (
	lockCancelledText = "Locked. The backup was cancelled before it read any mail, and its key is being cleared once its current step ends; nothing was added."
	lockReadingText   = "Locked for new use. The backup that is running has started reading mail, so it finishes with its own key, then the key is cleared. Click Cancel to stop it now."
)

// lockCancelNote is the reply to a Lock inside the window, by what the sync had done
// (§9, C-64, OQ-2): nothing about Thunderbird before its sync began, or for a job with
// no sync (OQ-8); "may still be starting" while it started; "left open" once the child
// was proven (waiting) or the sync ended leaving it open. On Windows the window is
// called minimized only when the sync ended saying so: while it waits that is not
// known, so the reply says its window may still be on screen.
func lockCancelNote(goos, phase string, ended bool, left string, minimized bool) string {
	w := runNowWordsFor(goos)
	open := func(minimizedKnown bool) string {
		if w.windows && !minimizedKnown {
			return " Thunderbird, which it started, was left open; its window may still be on screen — quit it from " + w.place + " before the next Run now."
		}
		return " Thunderbird, which it started " + w.hidden + ", was left open — quit it from " + w.place + " before the next Run now."
	}
	starting := " Thunderbird may still be starting; if it appears in " + w.place + ", quit it."
	switch {
	case !ended && phase == presync.PhaseWaiting:
		return lockCancelledText + open(false)
	case !ended && (phase == presync.PhaseStarting || phase == presync.PhaseCancelling):
		return lockCancelledText + starting
	case ended && (presync.Left(left) == presync.LeftOpen || presync.Left(left) == presync.LeftOpenByRule):
		return lockCancelledText + open(minimized)
	case ended && presync.Left(left) == presync.LeftMayBeStarting:
		return lockCancelledText + starting
	}
	return lockCancelledText
}

// runNowPause is a test's seam at named steps of an encrypted Run now ("heal", before
// the heal; "apply", once the key is handed to the run, before its discovery and
// decrypting open; "read-start", after the window closed and before app.Run's second
// check), so a test can Lock there (MA-913). A no-op in production.
var runNowPause = func(point string) {}

// runApply hands the lease's key to the run (keyring.Apply); a seam, so a test can see
// the key the run holds outlive a Lock until the run lets go of it (MA-913, C-5/T8).
var runApply = keyring.Apply

// runNowKeyed is the encrypted Run now in the job slot (§3.2, §8, slice AS8): healed
// first, then wizard.RunJobInProcessKeyed, attended (C-81), with this run's own Stop —
// ended by Shut down and by a Lock inside the window — and the Run now's callbacks
// bound to its slot (C-71). Its window closes at read start, before app.Run's second
// check (C-76).
func (d *dashboard) runNowKeyed(j keyedJob, out, jobPath string, lease *keyring.Unlocked, win *syncWindow, logger *log.Logger) (string, error) {
	c := d.cap
	goos := d.cfg.goos()
	stopCtx, stopRun := context.WithCancel(j.stop)
	defer stopRun()
	rn, slot, endWait := d.runNowHooks(goos)
	defer endWait()
	c.bindWindow(win, j.cancel, stopRun)
	onEnd := rn.OnEnd
	rn.OnEnd = func(o app.PresyncOutcome) {
		onEnd(o)
		d.keepLastSync(out, o)
	}
	rn.OnReadStart = func() {
		c.closeWindow(win)
		runNowPause("read-start")
	}
	runNowPause("heal")
	if lease.NeedsHeal() && !lease.Forked() {
		if herr := keyring.Heal(lease); herr != nil {
			logger.Printf("warning: one copy of this archive's key file is damaged and could not be rewritten: %v", herr)
		} else {
			logger.Printf("The damaged copy of this archive's key file was rewritten from the good one.")
		}
	}
	res, err := withProgramLine(logger, func(logger *log.Logger) (string, error) {
		return wizard.RunJobInProcessKeyed(j.ctx, stopCtx, jobPath, logger, rn, func(o *app.Options) (func(), error) {
			o.ProgramSkip, o.OnProgramWait, o.ProgramDownload = j.skip, j.onWait, d.life.Context()
			done, aerr := runApply(o, lease)
			if aerr == nil {
				runNowPause("apply")
			}
			return done, aerr
		})
	})
	if err != nil && j.ctx.Err() != nil && errors.Is(err, keyring.ErrLocked) {
		// A Lock before the key was handed to the run: nothing was opened or read (§3.2).
		err = context.Canceled
	}
	if words, ok := d.runNowCancelled(j, slot, goos, err); ok {
		return words, nil
	}
	return runNowResultHere(res), err
}

// keepLastSync keeps an encrypted Run now's sync phrase (§4.5's closed map) for its
// archive's card and health view while the archive stays unlocked in this Desktop
// session (§4.4, §4.7, C-27): on the key's entry, so any lock clears it. Nothing is
// written anywhere.
func (d *dashboard) keepLastSync(out string, o app.PresyncOutcome) {
	if d.cfg.Keys == nil {
		return
	}
	if e := d.cfg.Keys.get(out); e != nil {
		e.setLastSync(presync.TagPhrase(o.Name))
	}
}

// lastSyncMu guards every key entry's lastSync.
var lastSyncMu sync.Mutex

func (e *keyEntry) setLastSync(phrase string) {
	lastSyncMu.Lock()
	e.lastSync = phrase
	lastSyncMu.Unlock()
}

func (e *keyEntry) lastSyncPhrase() string {
	lastSyncMu.Lock()
	defer lastSyncMu.Unlock()
	return e.lastSync
}
