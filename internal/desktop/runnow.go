package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/wizard"
)

// The plaintext Run now with the Thunderbird sync (design-presync-attended §3.1,
// §4.6, §7, slice AS4; owner rulings OQ-1, OQ-2, OQ-6, OQ-7). It is attended where
// this platform's AttendedStartAllowed entry is true (C-49) and never closes
// Thunderbird (never-close on every platform until slice AS-Q, C-48, C-50). Its
// note is built here, on the server, so the Go tests read every word the person
// sees (C-19): CI has no JavaScript engine. Draft wording, settled by the friction
// review (§14 gate 3).

// syncMaxMinutes is the most the waiting note counts to: the session's longest wait
// for mail (presync's defMaxWait, 20 minutes).
const syncMaxMinutes = 20

// syncWaitWords is how long a Run now's wait for mail takes, as the tooltips, the
// encrypted notice and the release line say it (friction review, §14 gate 3). The lab
// record docs/lab/presync-attended-start-darwin.md measured a first fetch at +6 s on
// one run and at about +9.5 min (the periodic check) on another, so "usually 13 to
// 20" claimed a lower bound the lab does not show: up to 20 (no new mail), often about
// 13 (the periodic fetch plus 3 minutes of quiet).
const syncWaitWords = "up to 20 minutes, often about 13"

// runNowWords are a Run now text's platform words (§4.6, §2.7, C-24, C-61): how
// Thunderbird runs while left open, and where to quit it from. Windows names the
// taskbar and the notification area, never the Dock (OQ-2).
type runNowWords struct {
	hidden, place string
	windows       bool
}

func runNowWordsFor(goos string) runNowWords {
	if goos == "windows" {
		return runNowWords{hidden: "minimized to the taskbar", place: "the taskbar or the notification area", windows: true}
	}
	return runNowWords{hidden: "out of sight", place: "the Dock"}
}

// runNowSyncs reports whether this platform's Run now starts Thunderbird.
func runNowSyncs(goos string) bool {
	return (goos == "darwin" || goos == "windows") && presync.AttendedStartAllowed(goos)
}

// runNowPlainTitle is Run now's tooltip with the sync off, or where the sync never
// runs (Linux, C-24).
const runNowPlainTitle = "Run this archive's scheduled backup right now, instead of waiting for its schedule."

// runNowStartOffTitle is Run now's tooltip, sync on, where this platform's attended
// start is not allowed yet (C-49, OQ-7).
const runNowStartOffTitle = "Run this archive's backup now. On this computer Run now does not start Thunderbird yet, so it backs up the mail already here; scheduled backups still sync Thunderbird first."

// runNowTitle is Run now's tooltip (§7, C-50, C-61, C-69): the never-close texts —
// Thunderbird is left open for the person to quit; the close variants arrive with
// slice AS-Q.
func runNowTitle(goos string, syncOn bool) string {
	switch {
	case !syncOn || (goos != "darwin" && goos != "windows"):
		return runNowPlainTitle
	case !presync.AttendedStartAllowed(goos):
		return runNowStartOffTitle
	case goos == "windows":
		return "Run this archive's backup now. Because Thunderbird sync is on, it first starts Thunderbird minimized to the taskbar (its window may appear for a moment) — even while you are using the computer — and waits for its mail to arrive (" + syncWaitWords + "), then leaves it open for you to quit from the taskbar or the notification area. A Thunderbird that is already open is left alone. You can cancel while it waits; that skips the backup."
	}
	return "Run this archive's backup now. Because Thunderbird sync is on, it first starts Thunderbird out of sight — even while you are using the computer — and waits for its mail to arrive (" + syncWaitWords + "), then leaves it open for you to quit from the Dock. A Thunderbird that is already open is left alone. You can cancel while it waits; that skips the backup."
}

// runNowFirstSyncLine is the one-time line beside Run now (§4.6, C-32, C-83): shown
// for a sync-on job where Run now starts Thunderbird, until the first attended Run
// now on this computer has started it. The close variant arrives with slice AS-Q.
func runNowFirstSyncLine(goos string) string {
	w := runNowWordsFor(goos)
	return "New: Run now starts Thunderbird " + w.hidden + ", even while you are using the computer, waits up to 20 minutes for its mail, and leaves it open for you to quit from " + w.place + " when you are done."
}

// jobCancelTitle is the job slot's Cancel tooltip outside a Run now's sync.
const jobCancelTitle = "Stop this backup now. The archive is left safe; the next backup finishes what this one left."

// stopWaitTitle is the "Stop waiting and back up now" button's tooltip (§7, OQ-6).
const stopWaitTitle = "Stop waiting for Thunderbird and back up the mail that has arrived so far. Thunderbird is left open; mail still arriving will be in the next backup."

// cancelSyncTitle is Cancel's tooltip while a Run now's sync starts or waits (§7,
// C-23, C-69, OQ-6): it says the whole backup is skipped, not only the wait.
func cancelSyncTitle(goos, phase string) string {
	s := "Stop this backup now. Nothing is added to the archive: the whole backup is skipped, not only the wait. Thunderbird, started by this backup, is left open for you to quit from " + runNowWordsFor(goos).place + "."
	if phase == presync.PhaseWaiting {
		s += " To back up without waiting, click Stop waiting and back up now instead."
	}
	return s
}

// startNotAllowedLine is the note's line for a sync-on Run now where this platform's
// attended start is not allowed (§4.6, C-49): no hook ran and no outcome is recorded.
const startNotAllowedLine = "Thunderbird was not started: on this computer Run now does not start Thunderbird yet, so the backup read the mail already here. Scheduled backups still sync Thunderbird first."

// runNowSyncNote is the note while the sync runs (§4.6, C-19, C-69, C-74, OQ-6): by
// phase, the waiting count in whole minutes from the start of the wait, never above
// 20; after a "Stop waiting" press, the backing-up line until the run ends.
func runNowSyncNote(goos, phase string, since, now time.Time, waitStopped bool) string {
	w := runNowWordsFor(goos)
	if waitStopped {
		return "Backing up the mail that has arrived… Thunderbird was left open " + w.hidden + "."
	}
	switch phase {
	case presync.PhaseStarting:
		if w.windows {
			return "Starting Thunderbird minimized to the taskbar… its window may appear for a moment."
		}
		return "Starting Thunderbird out of sight…"
	case presync.PhaseWaiting:
		n := int(now.Sub(since) / time.Minute)
		if n < 0 {
			n = 0
		}
		if n > syncMaxMinutes {
			n = syncMaxMinutes
		}
		return fmt.Sprintf("Syncing Thunderbird… waiting for mail to arrive (%d min so far, up to %d). Thunderbird may fetch at once, or wait about 10 minutes for its regular mail check, so this often takes about 13 minutes; with no new mail it takes the full 20. Thunderbird is running %s and stays open after the wait. To back up now with the mail that has arrived, click Stop waiting and back up now.", n, syncMaxMinutes, w.hidden)
	case presync.PhaseCancelling:
		return "Cancelling… Thunderbird is still starting; it will be left open."
	}
	return ""
}

// runNowCancelWords is a cancelled Run now's result (§3.1, C-23, C-64, C-73): by what
// its sync left. A cancel during the sync skipped the whole backup; one while reading
// keeps today's words.
func runNowCancelWords(goos, name, left string, minimized bool) string {
	if name != presync.OutcomeCancelled.Name {
		return cancelledText
	}
	w := runNowWordsFor(goos)
	switch presync.Left(left) {
	case presync.LeftMayBeStarting:
		return "Cancelled. Thunderbird may still be starting; quit it from " + w.place + "."
	case presync.LeftOpenByRule, presync.LeftOpen:
		how := "left open " + w.hidden
		if w.windows && !minimized {
			how = "left open; its window may still be on screen"
		}
		return "Cancelled. Thunderbird, started by this backup, was " + how + "; quit it from " + w.place + ". The archive is unchanged."
	case presync.LeftExited:
		// Started, then gone on its own before the Cancel reached it (gate-2 pass 1
		// re-check): never "before Thunderbird was started".
		return "Cancelled. Thunderbird, started by this backup, was no longer running. The archive is unchanged."
	}
	return "Cancelled before Thunderbird was started. The archive is unchanged."
}

// runNowEndNote is the note once a Run now has ended (§4.6, C-19): the sync's line,
// then the result, the failure or the cancel words — a failed run no longer hides
// the sync's line. A sync that was itself cancelled is told by the cancel words alone.
func runNowEndNote(syncLine, syncName, result, errMsg string) string {
	end := result
	switch {
	case errMsg != "":
		end = "Backup failed: " + errMsg
	case end == "":
		end = "Backup finished."
	}
	if syncLine == "" || syncName == presync.OutcomeCancelled.Name {
		return end
	}
	return syncLine + " " + end
}

// runNowMutedEndNote is the end note of a Run now whose activity a Lock or a switch
// away cleared (C-15): its result and failure were cleared with the rest, so the note
// says only that it ended and why nothing more is shown — never "Backup finished.",
// which a cancelled or failed run would wrongly claim (gate-2 adversarial pass 1 over
// AS4, as4web-1). Draft wording, settled by the friction review (§14 gate 3).
const runNowMutedEndNote = "The backup has ended. Its result is not shown here: the archive was locked, or you switched away from it, while the backup ran, so what it showed was cleared. Recent backups shows how it ended."

// runNowElsewhereRunning and runNowElsewhereEnded are what another archive's page says
// of a Run now (gate-2 adversarial pass 1 over AS4, as4web-2): its note, Cancel and
// "Stop waiting" belong to its own archive's page, never beside this archive's Run now.
const (
	runNowElsewhereRunning = "A Run now of another archive is running. Switch back to that archive to see its progress, stop its wait or cancel it."
	runNowElsewhereEnded   = "A Run now of another archive has ended. Switch back to that archive to see how it ended in Recent backups."
)

// resetSyncLocked clears a Run now's sync fields; c.mu is held.
func (c *captureState) resetSyncLocked() {
	if c.endWait != nil {
		c.endWait()
	}
	c.runNow, c.syncPhase, c.syncSince, c.syncEnded = false, "", time.Time{}, false
	c.syncLine, c.syncName, c.syncLeft, c.syncMinimized, c.syncNoStart = "", "", "", false, false
	c.endWait, c.waitStopped = nil, false
}

// forgetLocked clears an encrypted job's activity (C-15): its lines, result and sync
// fields, and, while it still runs, mutes it so its later lines are not kept; c.mu is
// held.
func (c *captureState) forgetLocked() {
	c.lines, c.result, c.errMsg = nil, "", ""
	runNow, endWait := c.runNow, c.endWait
	c.endWait = nil // resetSyncLocked must not end a running Run now's wait
	c.resetSyncLocked()
	if c.running { // the run goes on; only what it showed goes
		c.runNow, c.endWait = runNow, endWait
	}
	c.muted = c.running
	c.jobOut = ""
}

// forget clears the activity of an encrypted archive's Run now on out: a Lock of that
// archive (C-15). Other jobs on it — an unencrypted copy, which goes on after a Lock
// and names its folder when it ends — keep theirs.
func (c *captureState) forget(out string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runNow && c.encrypted && c.jobOut != "" && samePath(c.jobOut, out) {
		c.forgetLocked()
	}
}

// setPhase records a Run now's phase, only for the run whose slot id it carries and
// only before its sync has ended (C-71): a late call from an earlier, abandoned
// session never changes the next run's note.
func (c *captureState) setPhase(slot uint64, phase string, since time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slot != slot || !c.running || !c.runNow || c.syncEnded || c.muted {
		return
	}
	c.syncPhase, c.syncSince = phase, since
}

// endSync records how a Run now's sync ended (§4.6), for that run only.
func (c *captureState) endSync(slot uint64, o app.PresyncOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slot != slot || !c.runNow || c.muted {
		return
	}
	c.syncEnded, c.syncPhase = true, ""
	c.syncLine, c.syncName, c.syncLeft, c.syncMinimized = o.Line, o.Name, o.Left, o.Minimized
}

// noStart records that this platform's Run now does not start Thunderbird (C-49).
func (c *captureState) noStart(slot uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slot != slot || !c.runNow || c.muted {
		return
	}
	c.syncNoStart, c.syncEnded, c.syncLine = true, true, startNotAllowedLine
}

// runNowActivity adds a Run now's fields to /api/activity (§4.6): whether the job is a
// Run now (the page re-attaches its poll after a reload, C-22), its slot id (which
// "Stop waiting" names), the phase and the server-built note while the sync runs,
// Cancel's sync tooltip, whether "Stop waiting" is offered (only while it waits,
// OQ-6), and, once it ends, the sync's line and the whole end note; c.mu is held.
// cur is the current archive: on another archive's page (a switch while it runs) the
// Run now's note, log, result, Cancel and "Stop waiting" are left out, so nothing of
// it shows beside that archive's Run now, and a note says another archive's Run now
// runs or has ended (gate-2 adversarial pass 1 over AS4, as4web-2). A Run now whose
// activity was cleared (muted, C-15) ends with runNowMutedEndNote, never "Backup
// finished." (as4web-1).
func (c *captureState) runNowActivity(out map[string]any, goos string, now time.Time, cur string) {
	if !c.runNow {
		return
	}
	out["runNow"] = true
	here := c.slotOut != "" && cur != "" && samePath(c.slotOut, cur)
	out["runNowHere"] = here
	if !here {
		out["log"], out["result"], out["error"] = []string{}, "", ""
		out["cancellable"], out["skippable"] = false, false
		out["otherNote"] = runNowElsewhereEnded
		if c.running {
			out["otherNote"] = runNowElsewhereRunning
		}
		return
	}
	out["run"] = strconv.FormatUint(c.slot, 10)
	phase := ""
	if !c.syncEnded {
		phase = c.syncPhase
	}
	out["syncPhase"] = phase
	if c.running {
		if note := runNowSyncNote(goos, phase, c.syncSince, now, c.waitStopped); note != "" {
			out["syncNote"] = note
		}
		if (phase == presync.PhaseStarting || phase == presync.PhaseWaiting) && !c.waitStopped {
			out["cancelTitle"] = cancelSyncTitle(goos, phase)
		}
		out["stopWaitable"] = phase == presync.PhaseWaiting && !c.waitStopped && c.endWait != nil
		return
	}
	if c.muted { // its result and failure were cleared: no end note can say how it ended
		out["note"] = runNowMutedEndNote
		return
	}
	out["syncLine"] = c.syncLine
	out["note"] = runNowEndNote(c.syncLine, c.syncName, c.result, c.errMsg)
}

// runNowPlain is the plaintext Run now in the job slot (§3.1): attended through
// wizard.RunJobInProcess, on the slot's own context (Cancel: presync.ErrCancelAndQuit)
// with the Desktop's own context as the sync's Stop (Shut down). Its Phase, OnEnd and
// StartNotAllowed callbacks carry this run's slot id (C-71); its EndWait is ended by
// "Stop waiting and back up now" (OQ-6). A cancel is told by what the sync left.
func (d *dashboard) runNowPlain(j keyedJob, jobPath string, logger *log.Logger) (string, error) {
	goos := d.cfg.goos()
	rn, slot, endWait := d.runNowHooks(goos)
	defer endWait()
	res, err := wizard.RunJobInProcess(j.ctx, j.stop, jobPath, logger, rn)
	if words, ok := d.runNowCancelled(j, slot, goos, err); ok {
		return words, nil
	}
	return res, err
}

// runNowHooks marks the job in the slot as a Run now (C-15, C-22) and returns its
// callbacks, each bound to its slot id (C-71), with the end of its EndWait ("Stop
// waiting and back up now", OQ-6): both Run nows, plaintext and encrypted, use them.
func (d *dashboard) runNowHooks(goos string) (rn wizard.RunNow, slot uint64, endWait context.CancelFunc) {
	c := d.cap
	endCtx, endWait := context.WithCancel(context.Background())
	c.mu.Lock()
	slot = c.slot
	c.runNow, c.endWait = true, endWait
	c.mu.Unlock()
	rn = wizard.RunNow{
		GOOS:            goos,
		EndWait:         endCtx,
		Phase:           func(p string, since time.Time) { c.setPhase(slot, p, since) },
		OnEnd:           func(o app.PresyncOutcome) { c.endSync(slot, o) },
		StartNotAllowed: func() { c.noStart(slot) },
	}
	return rn, slot, endWait
}

// runNowCancelled is a Run now the person cancelled with the Cancel button, told by
// what its sync left (§3.1, C-23); ok is false for any other ending, a Shut down
// included.
func (d *dashboard) runNowCancelled(j keyedJob, slot uint64, goos string, err error) (string, bool) {
	if !errors.Is(err, context.Canceled) || j.ctx.Err() == nil || d.life.Context().Err() != nil {
		return "", false
	}
	c := d.cap
	c.mu.Lock()
	name, left, minimized := c.syncName, c.syncLeft, c.syncMinimized
	if c.slot != slot {
		name = ""
	}
	c.mu.Unlock()
	return runNowCancelWords(goos, name, left, minimized), true
}

// stopWaitAction is "Stop waiting and back up now" (§4.6, A16, owner ruling OQ-6):
// POST api/runnow-stopwait, through d.api (POST only, the body limit, no-store,
// GuardLocalPOST and the CSRF token), body {run} naming the running Run now's slot
// id. Under c.mu it ends that run's EndWait: the session ends its wait for mail at
// once, leaves Thunderbird open, and the backup reads what has arrived. Refused,
// changing nothing, when no Run now runs, the id is stale, or the sync is not
// waiting (still starting: Cancel is the way out; cancelling, or a wait already
// ended).
func (d *dashboard) stopWaitAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Run string `json:"run"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	c := d.cap
	goos := d.cfg.goos()
	c.mu.Lock()
	running := c.running && c.runNow && c.endWait != nil && !c.muted && in.Run != "" && in.Run == strconv.FormatUint(c.slot, 10)
	phase := ""
	if running && !c.syncEnded {
		phase = c.syncPhase
	}
	refuse := ""
	switch {
	case !running:
		refuse = "No backup is waiting for Thunderbird. Nothing was changed."
	case c.waitStopped || c.syncEnded || phase == presync.PhaseCancelling:
		refuse = "The wait has already ended. Nothing was changed."
	case phase == presync.PhaseStarting:
		refuse = "Thunderbird is still starting: Stop waiting works once it is waiting for mail, and until then Cancel is the way to stop. Nothing was changed."
	case phase != presync.PhaseWaiting:
		refuse = "No backup is waiting for Thunderbird. Nothing was changed."
	}
	if refuse != "" {
		c.mu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "error": refuse})
		return
	}
	c.waitStopped = true
	c.endWait()
	c.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "note": runNowSyncNote(goos, phase, time.Time{}, time.Time{}, true)})
}

// runNowJobSyncs reports whether the job Run now would run for out — the corroborated
// run-job, whatever its name (C-18/M2) — asks for the Thunderbird sync.
func runNowJobSyncs(out string) bool {
	p := schedule.BuildKindEvidence(out).JobPath
	if p == "" {
		return currentJobSyncsThunderbird(out)
	}
	j, err := job.Read(p)
	return err == nil && j.ThunderbirdSync != nil
}

// releaseLineMac is the release notes' line for the first release that carries
// slice AS4 with macOS's attended start switched on (§7, C-32, C-52, owner ruling
// OQ-7: ship the Mac first). It goes into packaging/release-notes.md in the commit
// that sets macOS's AttendedStartAllowed entry true, citing its lab record; MA-915
// fails while the release notes and the table disagree. Corrected 2026-10-31 (the
// wording pass before 1.5.2, MA-938): it says Run now syncs only when the archive's
// sync is on, and that a scheduled backup skips the sync while the computer is in
// use (it never waits; presync session S4).
const releaseLineMac = "On a Mac, Run now syncs Thunderbird first when the archive's Thunderbird sync is on, even while you are at the computer: it starts Thunderbird out of sight, waits for its mail (" + syncWaitWords + "; Stop waiting and back up now ends the wait), and leaves it open for you to quit. Scheduled backups still skip the sync while you are using the computer. On Windows, Run now does not start Thunderbird yet; scheduled backups still sync it."
