package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The Desktop's actions on this computer's record of encrypted archives that need
// no key (design-encrypted-archives-phase3 §4.3; the P3-3 plan, 3b): repair a
// damaged record, and acknowledge that the record lost an archive. Each is a
// guarded POST (d.api), offered on the Status & health card only when its warning
// is there.

// maxSecretBody bounds an unlock's request: a password or recovery key is small.
const maxSecretBody = 16 << 10

// unlockRequest is what the Encryption card sends to unlock the current archive.
// Secret is never written back in a reply, a log or a setting (§5.2).
type unlockRequest struct {
	Secret   string `json:"secret"`
	Remember bool   `json:"remember"`
	// Cost is the derivation cost the person agreed to at a folder this computer has
	// not recorded (K14), sent back as the reply asking it gave it.
	Cost *vault.Params `json:"cost,omitempty"`
	// ReRecord: the person agreed to record again an archive they once told
	// MailArchive to forget (§4.2) — asked only when Remember records it.
	ReRecord bool `json:"reRecord"`
	// Remembered: open it with the key this computer remembers, the person's own Lock
	// notwithstanding (O3; the P3-3 pass, FID-03). No secret is sent.
	Remembered bool `json:"remembered"`
}

// unlockAction unlocks the current encrypted archive with what the person typed, on
// the Encryption card (§7.1: never in the reader). A read unlock records nothing;
// with Remember it records the folder and remembers the key (§3.3). A key file asking
// more than MailArchive's own cost at an unrecorded folder, and an archive the person
// said to forget, are asked about in the page and sent again with the answer.
func (d *dashboard) unlockAction(w http.ResponseWriter, r *http.Request) {
	var in unlockRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSecretBody)).Decode(&in); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "That request was not understood — reload the page and try again."})
		return
	}
	out := d.cfg.effectiveOut()
	if d.cfg.Keys == nil || out == "" || !app.ArchiveEncrypted(out) || !app.UnlockableAt(out) {
		writeJSON(w, map[string]any{"ok": false, "error": "The current archive is not an encrypted archive MailArchive Desktop can open here."})
		return
	}
	if in.Remembered {
		in.Secret = ""
		if _, err := d.cfg.Keys.unlockRemembered(out); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "The key this computer remembers did not open this archive: " + err.Error() + " — unlock it with its password or recovery key."})
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	// Remember records the folder: one the person told MailArchive to forget is asked
	// about before any key is derived, not after (§4.2; the P3-3 pass, FID-05).
	if in.Remember && !in.ReRecord {
		if gone, _ := keyring.Forgotten(out); gone {
			in.Secret = ""
			writeJSON(w, map[string]any{"ok": false, "confirm": "rerecord",
				"error": "You once told MailArchive to forget this encrypted archive on this computer. Remembering its key records it here again. Go ahead?"})
			return
		}
	}
	var askedCost *vault.Params
	askedReRecord := false
	o := keyring.OpenOptions{Store: d.cfg.Store, Remember: in.Remember,
		ConfirmCost: func(p vault.Params) (bool, error) {
			if c := in.Cost; c != nil && p.MemoryKiB <= c.MemoryKiB && p.Time <= c.Time && p.Threads <= c.Threads {
				return true, nil
			}
			askedCost = &p
			return false, nil
		},
		ConfirmReRecord: func(string) (bool, error) {
			askedReRecord = !in.ReRecord
			return in.ReRecord, nil
		}}
	_, err := d.cfg.Keys.unlock(out, in.Secret, o)
	in.Secret = ""
	switch {
	case err == nil:
		writeJSON(w, map[string]any{"ok": true})
	case askedCost != nil && errors.Is(err, keyring.ErrCostRefused):
		writeJSON(w, map[string]any{"ok": false, "confirm": "cost", "cost": askedCost,
			"error": fmt.Sprintf("Opening this archive would use %s (MailArchive's own key files use %s), and this computer has not recorded it. Open it anyway?", askedCost, vault.CostFloor())})
	case askedReRecord && errors.Is(err, keyring.ErrReRecordRefused):
		writeJSON(w, map[string]any{"ok": false, "confirm": "rerecord",
			"error": "You once told MailArchive to forget this encrypted archive on this computer. Remembering its key records it here again. Go ahead?"})
	case errors.Is(err, vault.ErrWrongSecret):
		writeJSON(w, map[string]any{"ok": false, "error": wrongSecretText(out)})
	default:
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
	}
}

// wrongSecretText is the unlock's answer to a password or recovery key that does not
// open the archive at out, in the command line's words (P3-8 friction review, B14's
// second voice): that nothing changed, which secret to try, and — when neither opens
// it — how to put back key files that may have been replaced.
func wrongSecretText(out string) string {
	return "That does not open this archive, so nothing was changed. Try again: the password, or the recovery key if you have forgotten it. " +
		"If neither the password nor the recovery key opens it, its key files may have been replaced: put them back with mailarchive restore-key-file -out " +
		encmark.CommandPath(out) + " -from FILE (FILE is the recovery file you saved)."
}

// lockAction locks the current encrypted archive because the person asked: its
// reader shows the locked page at once, and a remembered key does not re-open it by
// itself until they unlock it again (O3).
func (d *dashboard) lockAction(w http.ResponseWriter, r *http.Request) {
	out := d.cfg.effectiveOut()
	if d.cfg.Keys == nil || out == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "There is no encrypted archive open here to lock."})
		return
	}
	d.cfg.Keys.lock(out)
	if d.copyRunning(out) {
		writeJSON(w, map[string]any{"ok": true, "note": copyGoesOnText})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// OpenArchiveLabel is the "Open an archive folder…" button's words, kept in one place
// so the note an encrypted archive carries (P3-5) can quote them exactly.
const OpenArchiveLabel = app.OpenArchiveWords

// openArchiveAction is "Open an archive folder…" (§7.1): the folder picker, then a
// judgement of the folder chosen — an archive at its own folder, plaintext or
// encrypted; never a folder inside an encrypted archive, nor a recorded one that no
// longer holds its archive. It answers the path; the page then switches to it
// through the settings save, which asks about the weekly backup as any switch does.
// An encrypted archive opens on its Encryption card. Nothing is read or written here.
func (d *dashboard) openArchiveAction(w http.ResponseWriter, r *http.Request) {
	if d.exportLive() {
		writeJSON(w, map[string]any{"ok": false, "error": "The folder picker is off while an export runs — type the archive's folder in Settings → Archive location instead."})
		return
	}
	ws := d.ws
	ws.mu.Lock()
	if ws.pickerBusy {
		ws.mu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "error": "A folder picker is already open — it may be behind this window."})
		return
	}
	ws.pickerBusy = true
	ws.mu.Unlock()
	defer func() {
		ws.mu.Lock()
		ws.pickerBusy = false
		ws.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(d.life.Context(), pickerBudget)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()
	path, err := d.pick(ctx, "Choose the archive's folder")
	if err != nil || strings.TrimSpace(path) == "" {
		writeJSON(w, map[string]any{"ok": true, "canceled": true})
		return
	}
	if why := archiveFolderProblem(path); why != "" {
		writeJSON(w, map[string]any{"ok": false, "error": why})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": path})
}

// archiveFolderProblem says why path cannot be opened as an archive, or "".
func archiveFolderProblem(path string) string {
	abs, _ := keyOf(path)
	if dir, _, _ := encmark.AtOrAbove(abs); dir != "" && cleanPath(dir) != cleanPath(abs) {
		return fmt.Sprintf("%s is inside the encrypted archive %s — choose the archive's own folder.", abs, dir)
	}
	if p := recordedProblem(abs); p != "" {
		return p
	}
	if state.UnfinishedCopy(abs) {
		return fmt.Sprintf("%s holds an unencrypted copy that did not finish: it is not complete — delete it, then make the copy again.", abs)
	}
	if !archindex.IsArchive(abs) {
		// The same words as the No-archive card; a manifest that is there but cannot be
		// read is told so, with where its repair shows (the P3-3 pass, REG-04).
		if _, err := os.Lstat(filepath.Join(abs, ".mailarchive-manifest.json")); err == nil {
			return fmt.Sprintf("%s holds an archive whose record (.mailarchive-manifest.json) cannot be read — type the folder in Settings → Archive location, and Status & health shows how to repair it.", abs)
		}
		return fmt.Sprintf("%s isn't a MailArchive archive — choose the folder that holds the .mailarchive-manifest.json file.", abs)
	}
	return ""
}

// claimsID reports whether a key file in out still names the archive id opened here.
func claimsID(out, id string) bool {
	for _, c := range vault.Claims(out).IDs {
		if c.String() == id {
			return true
		}
	}
	return false
}

// Status actions the health reply offers, by the warning they answer.
const (
	actionRepairRecord = "repair-record"
	actionDismissLost  = "dismiss-lost"
)

// recordActions are the record actions the Status & health card offers for out.
func recordActions(out string) []string {
	var actions []string
	var ue *encrecord.UnusableError
	if _, err := encrecord.Load(); errors.As(err, &ue) && ue.Case == encrecord.CaseDamaged {
		actions = append(actions, actionRepairRecord)
	}
	if out != "" && encmark.LostHere(out) {
		actions = append(actions, actionDismissLost)
	}
	return actions
}

// repairRecordAction rebuilds this computer's damaged record from what still reads,
// as `mailarchive repair-record` does: the damaged copies are kept beside it, and
// every rebuilt folder stays protected until its archive is opened with its
// password. A record that is not damaged is refused, nothing moved.
func (d *dashboard) repairRecordAction(w http.ResponseWriter, r *http.Request) {
	rep, err := encrecord.Repair()
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	lines := []string{fmt.Sprintf("Rebuilt this computer's record of encrypted archives: %d folder(s), %d forgotten archive(s).", rep.Paths, rep.Tombstones)}
	for _, p := range rep.MovedAside {
		lines = append(lines, "The damaged copy "+p+" was kept aside.")
	}
	for _, n := range rep.NotRebuilt {
		lines = append(lines, "Not rebuilt: "+n)
	}
	if rep.Paths > 0 {
		lines = append(lines, "Open each archive once with its password and run a backup into it to confirm it.")
	}
	writeJSON(w, map[string]any{"ok": true, "lines": lines})
}

// dismissLostAction acknowledges that this computer's record no longer lists the
// current archive (§4.3): only the archive open here leaves the lost list, so the
// warning stops for it and no other.
func (d *dashboard) dismissLostAction(w http.ResponseWriter, r *http.Request) {
	out := d.cfg.effectiveOut()
	if len(encmark.LostIDs(out)) == 0 {
		writeJSON(w, map[string]any{"ok": false, "error": "there is no warning about this archive's record to dismiss"})
		return
	}
	// The folder's key files can name any archive — a planted copy, or both copies
	// replaced — so only the id of the archive opened here with its key is cleared:
	// the one id nobody can plant (the P3-3 follow-up pass, DRIVE-01).
	e := d.cfg.Keys.get(out)
	if e == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "unlock this archive first: opening it with its key shows which archive the warning is about, and only that one's warning is dismissed"})
		return
	}
	lost, _ := encrecord.Lost()
	if !slices.Contains(lost, e.id) {
		writeJSON(w, map[string]any{"ok": false, "error": "the archive open here is not the one the warning is about: its key files may have been replaced — nothing was dismissed"})
		return
	}
	if err := encrecord.ClearLost(e.id); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "the warning could not be dismissed: " + err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
