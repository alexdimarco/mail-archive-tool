package desktop

import (
	"net/http"
	"strings"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/wizard"
)

// addArchiveFn is the wizard's local add-an-archive flow (native dialogs), a seam a
// test replaces so it drives the handler without a GUI. It returns the new archive's
// output folder, or "" when the user backed out of a dialog.
var addArchiveFn = wizard.AddArchive

// addArchive runs the wizard's "add another archive" flow in process (the "Your
// archives" card button, MA-696): the same native-dialog flow the double-click
// wizard uses, so a running Desktop can create a NEW local archive without the
// Terminal. The new archive is folded into the known-archives index so it appears in
// the list, but it is NOT made current — it is an ADDITIONAL archive, not a switch.
// Loopback+CSRF guarded, and one at a time (the native dialogs are process-global).
// It never deletes or rewrites an existing archive (R36).
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

	out, err := addArchiveFn()
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

// registerArchive folds a newly created archive's folder into the known-archives
// index so the "Your archives" list shows it, WITHOUT making it the current archive
// (archindex.Reconcile is monotone — it only ever adds). A blank path or no index is
// a no-op. The Desktop stays the index's sole maintainer (SD-6).
func (cfg Config) registerArchive(path string, now time.Time) error {
	if strings.TrimSpace(cfg.IndexPath) == "" || strings.TrimSpace(path) == "" {
		return nil
	}
	before, err := archindex.ReadFile(cfg.IndexPath)
	if err != nil {
		return err
	}
	after := archindex.Reconcile(before, []string{path}, now)
	if len(after.Entries) == len(before.Entries) {
		return nil
	}
	return archindex.WriteFile(cfg.IndexPath, after)
}
