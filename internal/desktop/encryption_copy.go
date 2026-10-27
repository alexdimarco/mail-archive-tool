package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/thunderbird"
)

// "Make an unencrypted copy" on the Encryption card (design-encrypted-archives-phase3
// §10, K5, X14; P3-4 step 7): the folder picked with the system's dialog, the plan
// shown first and kept under a token, the copy started only when it is confirmed —
// twice for a folder that may leave this computer, checked here, not only in the page
// — on a read lease taken as it is clicked, so a Lock after it does not stop it; Shut
// down stops it, leaving the folder marked unfinished.

// copyPlanAction picks the folder to make the copy in and shows what the copy will do.
func (d *dashboard) copyPlanAction(w http.ResponseWriter, r *http.Request) {
	out, e := d.openHere(w)
	if e == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.life.Context(), pickerBudget)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()
	parent, err := d.pick(ctx, "Choose a folder to make the unencrypted copy in")
	if err != nil || strings.TrimSpace(parent) == "" {
		writeJSON(w, map[string]any{"ok": true, "canceled": true})
		return
	}
	plan, err := d.copyEnv().PlanDecryptCopy(out, strings.TrimSpace(parent), d.copyArchives(), d.cfg.settingsDirs(), d.now())
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	abs, key := keyOf(out)
	token, ok := d.keep(w, &pendingItem{key: key, out: abs, copyPlan: plan})
	if !ok {
		return
	}
	lines := []string{
		"An UNENCRYPTED copy of this archive will be made — readable by anyone who can open its folder.",
		"New folder: " + plan.Dest,
		"Size: up to " + thunderbird.HumanBytes(plan.NeedBytes),
	}
	if plan.FreeKnown {
		lines[2] += fmt.Sprintf(" (%s free)", thunderbird.HumanBytes(int64(plan.FreeBytes)))
	}
	lines = append(lines, plan.Warnings...)
	lines = append(lines, copyRunsOnText)
	writeJSON(w, map[string]any{"ok": true, "token": token, "lines": lines, "leaves": plan.Leaves, "askTwice": plan.AskTwice()})
}

// copyRunsOnText says, in the plan, what Lock and Shut down do to a copy once it runs.
const copyRunsOnText = "Once started, the copy runs to its end even if you lock the archive. Shut down stops it and leaves its folder marked unfinished; MailArchive does not remember a stopped copy after it closes, so delete that folder yourself."

// copyArchives are the other archive folders the copy is made outside of.
func (d *dashboard) copyArchives() []string {
	return append(d.archives(), d.ws.otherPaths()...)
}

// copyStartRequest confirms a planned copy.
type copyStartRequest struct {
	Token        string `json:"token"`
	Confirm      bool   `json:"confirm"`
	ConfirmAgain bool   `json:"confirmAgain"`
}

// copyStartAction makes the copy the plan under its token describes.
func (d *dashboard) copyStartAction(w http.ResponseWriter, r *http.Request) {
	var in copyStartRequest
	if !d.readBody(w, r, &in) {
		return
	}
	it := d.pending.get(in.Token, isCopyPlan)
	if it == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "That copy is no longer planned — click Make an unencrypted copy again."})
		return
	}
	if !in.Confirm || (it.copyPlan.AskTwice() && !in.ConfirmAgain) {
		writeJSON(w, map[string]any{"ok": false, "error": "The copy was not confirmed; nothing was made."})
		return
	}
	if it = d.pending.take(in.Token, isCopyPlan); it == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "That copy is no longer planned — click Make an unencrypted copy again."})
		return
	}
	e := d.cfg.Keys.get(it.out)
	if e == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "This encrypted archive is locked: unlock it, then make the copy again."})
		return
	}
	lease, err := e.u.Lease() // a read lease: the copy only reads the archive
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	plan, env, archives, settings := it.copyPlan, d.copyEnv(), d.copyArchives(), d.cfg.settingsDirs()
	_, key := keyOf(it.out)
	if err := d.runJob(func(logger *log.Logger) (string, error) {
		defer lease.Destroy()
		d.copying.Store(key)
		defer d.copying.Store("")
		rep, err := decryptCopy(d.life.Context(), lease, env, plan, archives, settings, logger, copyProgress(logger, time.Now))
		if err != nil {
			return "", spokenErr(copyFailureText(plan, rep, err))
		}
		msg := fmt.Sprintf("Unencrypted copy made at %s: %d of %d messages copied.", rep.Dest, rep.Copied, rep.Messages)
		if rep.Skipped > 0 {
			msg += fmt.Sprintf(" %d were left out — each is listed, with why, in %s inside the copy.", rep.Skipped, app.CopyProblemsName)
		}
		if rep.HistoryLost > 0 || rep.HistoryGap {
			msg += " Part of the archive's go-back timeline could not be read, so the copy's go-back view is partial too."
		}
		switch rep.Index {
		case "rebuilt":
			msg += " The copy's search index was rebuilt from the copied files."
		case "none":
			msg += " The copy has no search index: Rebuild search index on it makes one."
		}
		return msg, nil
	}); err != nil {
		lease.Destroy()
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// decryptCopy makes the copy; tests hold it open (fault injection) to drive Lock and
// Shut down while it runs.
var decryptCopy = keyring.DecryptCopy

// copyProgressEvery is how often, at most, the copy's progress shows in Activity.
const copyProgressEvery = 3 * time.Second

// copyProgress writes the copy's progress to Activity as counts only — never a path
// or a subject — at most every copyProgressEvery, and the first message at once.
func copyProgress(logger *log.Logger, now func() time.Time) func(app.DecryptReport) {
	var last time.Time
	return func(r app.DecryptReport) {
		if t := now(); last.IsZero() || t.Sub(last) >= copyProgressEvery {
			last = t
			logger.Printf("Unencrypted copy: %d of %d messages so far", r.Copied+r.Skipped, r.Messages)
		}
	}
}

// copyRunning says an unencrypted copy of the archive at out is being made.
func (d *dashboard) copyRunning(out string) bool {
	_, key := keyOf(out)
	s, _ := d.copying.Load().(string)
	return s != "" && s == key
}

// copyGoesOnText is the Lock reply while a copy of the archive is being made.
const copyGoesOnText = "Locked. The unencrypted copy already started goes on until it finishes — Shut down stops it, leaving its folder marked unfinished."

// spokenErr is a task's failure already worded for the person: the task's result
// shows it as it is.
type spokenErr string

func (e spokenErr) Error() string { return string(e) }

// copyFailureText words a copy that did not finish for the person: what happened, and
// where the folder of readable mail it left is. It never shows the engine's wrapped
// cause, which can name a message's file.
func copyFailureText(plan *app.DecryptPlan, rep app.DecryptReport, err error) string {
	var r *app.CopyRefusal
	switch {
	case errors.As(err, &r):
		return r.Message
	case errors.Is(err, app.ErrDecryptStale), errors.Is(err, lockfile.ErrHeld):
		return err.Error()
	}
	// How far the copy got, not a look at the path: a folder moved away mid-copy still
	// holds readable mail (the PG pass, FIX-01).
	if !rep.Unfinished {
		if errors.Is(err, app.ErrCopyLocked) {
			return "The archive was locked before the copy began; nothing was made. Unlock it, then make the copy again."
		}
		return "The unencrypted copy could not be started; nothing was made. Check that the folder is still there and the archive is unlocked, then make the copy again."
	}
	what := "The unencrypted copy did not finish."
	if errors.Is(err, app.ErrCopyStopped) {
		what = "The unencrypted copy was stopped."
	}
	if _, lerr := os.Lstat(plan.Dest); lerr != nil {
		return what + " Its folder, " + plan.Name + ", was moved or replaced while the copy was made: wherever it is now, it holds part of the copy, READABLE by anyone who can open it — find it and delete it, then make the copy again."
	}
	return what + " The folder " + plan.Dest + " holds part of it, READABLE by anyone who can open it — delete it, then make the copy again."
}
