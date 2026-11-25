package desktop

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/wizard"
)

// addArchivesFn is the wizard's local add-an-archive flow (native dialogs), a seam a
// test replaces so it drives the handler without a GUI. It returns every archive folder
// the flow made or continued with its outcome, none when the person backed out
// (docs/design-per-mailbox-archives.md §10, MA-1499).
var addArchivesFn = wizard.AddArchives

// addArchive runs the wizard's "add another archive" flow in process (the "Your
// archives" card button, MA-696): the same native-dialog flow the double-click
// wizard uses, so a running Desktop can create NEW local archives without the
// Terminal. Each new archive is folded into the known-archives index so it appears in
// the list, but none is made current — each is an ADDITIONAL archive, not a switch.
// One archive keeps today's words (MA-696, MA-954); One archive for each mailbox has its
// own (addedSet, MA-1499). Loopback+CSRF guarded, and one at a time (the native dialogs
// are process-global). It never deletes or rewrites an existing archive (R36).
func (d *dashboard) addArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, csrfRefused, http.StatusForbidden)
		return
	}
	if !d.addBusy.CompareAndSwap(false, true) {
		writeJSON(w, map[string]any{"ok": false, "error": "An add is already in progress — finish the steps on your computer, then try again."})
		return
	}
	defer d.addBusy.Store(false)

	made, err := addArchivesFn()
	if len(made) > 0 && made[0].Parent != "" {
		d.addedSet(w, made, err)
		return
	}
	var one wizard.Made
	if len(made) > 0 {
		one = made[0]
	}
	out := one.Folder
	if one.Outcome == wizard.MadeStopped && strings.TrimSpace(out) != "" {
		// The copy was stopped part way (MA-954): list the mail copied so far, but never
		// as a finished archive, and say how to finish it. A folder the copy never
		// reached holds no archive and is not added.
		if validArchiveFolder(out) == "" {
			writeJSON(w, map[string]any{"ok": false, "canceled": true, "message": "The wizard was stopped before it copied any mail. No archive was added."})
			return
		}
		msg := "The wizard was stopped before it finished copying your mail. The archive keeps the mail copied so far and is listed under Your archives once you reload this page. To finish it, click Make a new archive… again and choose the same mail and the same folder."
		if rerr := d.cfg.registerArchive(out, d.now()); rerr != nil {
			msg = "The wizard was stopped before it finished copying your mail. The archive keeps the mail copied so far in " + out + ", but it could not be added to the list. To finish it, click Make a new archive… again and choose the same mail and the same folder."
		}
		writeJSON(w, map[string]any{"ok": false, "stopped": true, "path": out, "message": msg})
		return
	}
	if err == nil && one.Outcome == wizard.MadeFailed {
		err = errors.New(one.Reason) // the flow's answers always carry the error; said as one
	}
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "Could not add an archive: " + err.Error()})
		return
	}
	if strings.TrimSpace(out) == "" {
		writeJSON(w, map[string]any{"ok": false, "canceled": true, "message": "No archive was added."})
		return
	}
	if rerr := d.cfg.registerArchive(out, d.now()); rerr != nil {
		writeJSON(w, map[string]any{"ok": true, "path": out, "message": "Archive created. It could not be added to the list automatically — it is safe; reload to see it."})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": out, "message": "Archive added — it is now in your list."})
}

// The notes of a set's Make a new archive… (§10, revision 2's C8 and C11).
const (
	// setAddedLead is the lead when every archive finished (two or more), and, with the count of
	// those that finished, when some did not.
	setAddedLead = "%d archives added — they are now in Your archives."
	// setAddedOneLead is the lead when exactly one archive finished and another did not (the
	// builder's singular form).
	setAddedOneLead = "1 archive added — it is now in Your archives."
	// setAddedNoneLead is the lead when none finished (the builder's zero form).
	setAddedNoneLead = "No archive was finished."
	// setNotListed replaces the lead when the list of archives could not be written (the builder's
	// form of today's "Archive created. It could not be added to the list automatically …").
	setNotListed = "The archives were made in %s, but they could not be added to the list automatically — they are safe; reload to see them."
	// setDidNotFinish names an archive that stopped part way or failed, with its reason.
	setDidNotFinish = "%s did not finish: %s."
	// setWasNotStarted names an archive that was not started (the builder's words).
	setWasNotStarted = "%s was not started."
	// setFinishIt and setFinishThem say how to finish, naming the set's folder (C8).
	setFinishIt   = "To finish it, click Make a new archive… again, choose One archive for each mailbox and choose the same folder, %s."
	setFinishThem = "To finish them, click Make a new archive… again, choose One archive for each mailbox and choose the same folder, %s."
	// setThenError follows the notes when the wizard ended with an error after the archives
	// (the builder's words).
	setThenError = "The wizard then ended with an error: %s."
)

// addedSet answers Make a new archive… after One archive for each mailbox (§10, revision 2's C8
// and C11; MA-1499). Every folder of the set that holds an archive (a finished one, or one
// stopped part way with the mail copied so far) is registered in one write of the list of archives
// (registerArchives: one archindex.UpdateFile with Reconcile), and none is made current (MA-696).
// Its note: all finished, "N archives added — they are now in Your archives." (one archive, today's
// "Archive added — it is now in your list."); otherwise the count of those that finished, then each
// one that did not, by name with its reason (or "was not started"), and how to finish it, naming the
// set's folder. The page reloads only when every archive finished and nothing went wrong; otherwise
// the note stays on the page for the person to read. Names, reasons and the folder reach the page
// through the reply's JSON and the page's textContent (X15).
func (d *dashboard) addedSet(w http.ResponseWriter, made []wizard.Made, flowErr error) {
	parent := util.StripControl(made[0].Parent)
	var (
		folders  []string
		finished int
		notDone  []string
	)
	for _, m := range made {
		if validArchiveFolder(m.Folder) != "" {
			folders = append(folders, m.Folder)
		}
		name := util.StripControl(m.Name)
		switch m.Outcome {
		case wizard.MadeFinished:
			finished++
		case wizard.MadeNotStarted:
			notDone = append(notDone, fmt.Sprintf(setWasNotStarted, name))
		default:
			notDone = append(notDone, fmt.Sprintf(setDidNotFinish, name, util.StripControl(m.Reason)))
		}
	}
	rerr := d.cfg.registerArchives(folders, d.now())

	var lead string
	switch {
	case rerr != nil && len(made) == 1:
		lead = "Archive created. It could not be added to the list automatically — it is safe; reload to see it."
	case rerr != nil:
		lead = fmt.Sprintf(setNotListed, parent)
	case len(made) == 1 && finished == 1:
		lead = "Archive added — it is now in your list."
	case finished == 0:
		lead = setAddedNoneLead
	case finished == 1:
		lead = setAddedOneLead
	default:
		lead = fmt.Sprintf(setAddedLead, finished)
	}
	parts := append([]string{lead}, notDone...)
	switch len(notDone) {
	case 0:
	case 1:
		parts = append(parts, fmt.Sprintf(setFinishIt, parent))
	default:
		parts = append(parts, fmt.Sprintf(setFinishThem, parent))
	}
	if flowErr != nil {
		parts = append(parts, fmt.Sprintf(setThenError, strings.TrimSuffix(util.StripControl(flowErr.Error()), ".")))
	}
	msg := strings.Join(parts, " ")
	ok := len(notDone) == 0 && flowErr == nil && rerr == nil
	reply := map[string]any{"ok": ok, "paths": folders, "message": msg}
	if len(notDone) > 0 {
		reply["stopped"] = true
	}
	writeJSON(w, reply)
}

// registerArchive folds a newly created archive's folder into the known-archives
// index so the "Your archives" list shows it, WITHOUT making it the current archive
// (archindex.Reconcile is monotone — it only ever adds). A blank path or no index is
// a no-op. The Desktop stays the index's sole maintainer (SD-6).
func (cfg Config) registerArchive(path string, now time.Time) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return cfg.registerArchives([]string{path}, now)
}

// registerArchives folds the folders of newly made archives into the known-archives index in one
// write, none made current (MA-696, MA-1499): the list is read and written under its lock
// (archindex.UpdateFile, CON-07), and archindex.Reconcile adds each that is an archive and is not
// listed yet. No folders, or no index, is a no-op.
func (cfg Config) registerArchives(paths []string, now time.Time) error {
	if strings.TrimSpace(cfg.IndexPath) == "" || len(paths) == 0 {
		return nil
	}
	return archindex.UpdateFile(cfg.IndexPath, func(before archindex.Index) (archindex.Index, bool, error) {
		n := len(before.Entries)
		after := archindex.Reconcile(before, paths, now)
		return after, len(after.Entries) != n, nil
	})
}
