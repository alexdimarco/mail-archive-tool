package desktop

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
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
	writeJSON(w, map[string]any{"ok": true, "token": token, "lines": lines, "leaves": plan.Leaves, "askTwice": plan.AskTwice()})
}

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
	if err := d.runJob(func(logger *log.Logger) (string, error) {
		defer lease.Destroy()
		rep, err := keyring.DecryptCopy(d.life.Context(), lease, env, plan, archives, settings, logger, nil)
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Unencrypted copy made at %s: %d of %d messages copied.", rep.Dest, rep.Copied, rep.Messages)
		if rep.Skipped > 0 {
			msg += fmt.Sprintf(" %d were left out — each is listed, with why, in %s inside the copy.", rep.Skipped, app.CopyProblemsName)
		}
		if rep.Index == "none" {
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
