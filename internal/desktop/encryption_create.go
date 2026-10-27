package desktop

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/wizard"
)

// Make an encrypted archive from MailArchive Desktop (design-encrypted-archives-
// phase3 §5.1, §5.2, D1, K2; the P3-3 plan, 3f): the wizard's native dialogs choose
// what to back up and the folder — judged empty and outside every archive and the
// settings as soon as it is chosen — and never ask a password; the password is typed
// in the page, the recovery key shown there once and typed back (K2) before anything
// is written; then the key file is written, the folder recorded and listed (not made
// current, MA-696), the run job Run now repeats written with no schedule (O5), and the
// first backup runs here, attended, its key destroyed when it ends.

// addEncryptedFn is the wizard's native-dialog flow, a seam a test replaces.
var addEncryptedFn = wizard.AddEncryptedArchive

// credentialPaths are the Desktop's own settings and secret files: none may be inside
// the new archive, nor the archive inside one (P2a-C14).
func (cfg Config) credentialPaths() []string {
	var ps []string
	for _, p := range []string{cfg.ConfigPath, cfg.SettingsPath, cfg.IndexPath, cfg.TokenCachePath, cfg.StateDir} {
		if strings.TrimSpace(p) != "" {
			ps = append(ps, p)
		}
	}
	return ps
}

// createStartAction runs the wizard's questions for a new encrypted archive.
func (d *dashboard) createStartAction(w http.ResponseWriter, r *http.Request) {
	if !d.addBusy.CompareAndSwap(false, true) {
		writeJSON(w, map[string]any{"ok": false, "error": "An add is already in progress — finish the steps on your computer, then try again."})
		return
	}
	defer d.addBusy.Store(false)
	creds := d.cfg.credentialPaths()
	add, err := addEncryptedFn(func(out string) error {
		_, err := keyring.CheckCreateFolder(out, creds)
		return err
	})
	switch {
	case err != nil:
		writeJSON(w, map[string]any{"ok": false, "error": "Could not make an encrypted archive: " + err.Error()})
		return
	case add == nil:
		writeJSON(w, map[string]any{"ok": true, "canceled": true})
		return
	case add.Outlook():
		writeJSON(w, map[string]any{"ok": false, "error": "An encrypted archive made in MailArchive Desktop backs up mail files, not through the Outlook app, for now: choose the mail files, or make it with `mailarchive -encrypt` at a terminal."})
		return
	}
	abs, key := keyOf(add.Out())
	token, ok := d.keep(w, &pendingItem{key: key, out: abs, add: add})
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"ok": true, "token": token, "path": add.Out(), "hint": keyring.PasswordHint})
}

// createKeyRequest is the new archive's password, typed twice, and whether to
// remember its key (on by default, D1).
type createKeyRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
	Again       string `json:"again"`
	Remember    bool   `json:"remember"`
}

// createKeyAction makes the new archive's key from its password and shows its
// recovery key once; nothing is written until it is typed back.
func (d *dashboard) createKeyAction(w http.ResponseWriter, r *http.Request) {
	var in createKeyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSecretBody)).Decode(&in); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "That request was not understood — reload the page and try again."})
		return
	}
	defer func() { in.NewPassword, in.Again = "", "" }()
	it := d.pending.get(in.Token, func(it *pendingItem) bool { return it.add != nil })
	if it == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "That new archive is no longer waiting — click Add again."})
		return
	}
	if p := keyring.NewPasswordProblem(in.NewPassword); p != "" {
		writeJSON(w, map[string]any{"ok": false, "error": p})
		return
	}
	if in.NewPassword != in.Again {
		writeJSON(w, map[string]any{"ok": false, "error": "The two passwords do not match."})
		return
	}
	p, err := keyring.Create(it.add.Out(), in.NewPassword, keyring.CreateOptions{Remember: in.Remember && d.cfg.Store != nil, Store: d.cfg.Store, CredentialPaths: d.cfg.credentialPaths()})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Kept only while the item still waits and has no key yet: one discarded
	// meanwhile (Cancel, its ten minutes, Shut down) never holds the new key, and a
	// key already made is never replaced — the recovery key typed back is always the
	// one committed, and no recovery file is left for a key that was not (the P3-3
	// follow-up pass, MAKE-03).
	made := false
	if !d.pending.withItem(in.Token, it, func() {
		if made = it.make != nil; !made {
			it.make = p
		}
	}) || made {
		p.Discard()
		msg := "That new archive is no longer waiting — click Add again."
		if made {
			msg = "This new archive's key is already made — Cancel, then start again to choose another password."
		}
		writeJSON(w, map[string]any{"ok": false, "error": msg})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "token": in.Token, "recoveryKey": p.RecoveryKey(), "warning": keyring.RecoveryWarning})
}

// commitCreate writes the new archive's key file once its recovery key was typed
// back, records and lists it, and starts its first backup.
func (d *dashboard) commitCreate(w http.ResponseWriter, it *pendingItem) {
	out := it.add.Out()
	u, err := it.make.Commit()
	if err != nil && !errors.Is(err, keyring.ErrCreatedNotRecorded) {
		it.make.Discard() // nothing was made: its key goes now, never to be committed later
		if it.file != "" {
			_ = keyring.VoidFile(it.file)
		}
		writeJSON(w, map[string]any{"ok": false, "error": "Nothing was made: " + err.Error()})
		return
	}
	_ = d.cfg.registerArchive(out, d.now())
	lines := []string{"The encrypted archive was made in " + out + "."}
	if it.file != "" {
		lines = append(lines, keyring.RecoveryFileNote)
	}
	// The run job Run now repeats is written at once, so an archive whose first backup
	// cannot start is still finished by Run now (the P3-3 follow-up pass, MAKE-01).
	jobPath, jerr := it.add.WriteRunJob()
	if jerr != nil {
		lines = append(lines, "Its backup settings could not be saved ("+jerr.Error()+"): back it up with mailarchive -out at a terminal to finish setting it up.")
	}
	if err != nil { // made, but not recorded here: it says how to finish
		writeJSON(w, map[string]any{"ok": true, "lines": append(lines, err.Error())})
		return
	}
	lines = append(lines, u.Warnings()...)
	if jerr != nil {
		u.Destroy()
		writeJSON(w, map[string]any{"ok": true, "lines": lines})
		return
	}
	// The job is the new archive's own: its failure is worded for it, not for the
	// archive open here (MAKE-02).
	if rerr := d.runJobOn(out, func(logger *log.Logger) (string, error) {
		defer u.Destroy() // the first backup's key goes when it ends
		return wizard.RunJobInProcessKeyed(d.life.Context(), jobPath, logger, func(o *app.Options) (func(), error) {
			return keyring.Apply(o, u)
		})
	}); rerr != nil {
		u.Destroy()
		lines = append(lines, "Its first backup could not start now ("+rerr.Error()+"): switch to it, unlock it, and click Run now to finish setting it up.")
		writeJSON(w, map[string]any{"ok": true, "lines": lines})
		return
	}
	lines = append(lines, "Its first backup is running now — it shows here as it goes. The archive is in Your archives; switch to it to read it.")
	writeJSON(w, map[string]any{"ok": true, "lines": lines, "running": true})
}
