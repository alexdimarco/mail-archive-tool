package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// The key changes on the Encryption card (design-encrypted-archives-phase3 §9.1–§9.3,
// K2, K13; the P3-3 plan, 3e): a new password, a new recovery key with its recovery
// file, and remembering or forgetting the key. Each runs on the archive open here,
// through a write lease of its own; each asks first, in the page, the confirmation a
// change authorised by no secret typed for it owes (K13). Secrets arrive only in a
// guarded POST's body and are never written back, but the new recovery key, once.

// keyRequest is what the card's key changes send.
type keyRequest struct {
	NewPassword string `json:"newPassword"`
	Again       string `json:"again"`
	Confirm     bool   `json:"confirm"`
	Token       string `json:"token"`
	Typed       string `json:"typed"`
	On          bool   `json:"on"`
	ReRecord    bool   `json:"reRecord"`
}

// readKeyRequest decodes a key change's small body; false when it answered.
func readKeyRequest(w http.ResponseWriter, r *http.Request) (keyRequest, bool) {
	var in keyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSecretBody)).Decode(&in); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "That request was not understood — reload the page and try again."})
		return in, false
	}
	return in, true
}

// openHere is the current archive's open in this Desktop, or nil after answering that
// it is locked.
func (d *dashboard) openHere(w http.ResponseWriter) (string, *keyEntry) {
	out := d.cfg.effectiveOut()
	e := d.cfg.Keys.get(out)
	if e == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "This encrypted archive is locked: unlock it on its Encryption card first."})
	}
	return out, e
}

// askFirst answers the K13 confirmation for what when it was not given yet; true when
// it answered.
func askFirst(w http.ResponseWriter, e *keyEntry, confirmed bool, what string) bool {
	if confirmed {
		return false
	}
	// A question, not an error: it travels in its own field (P3-8, Tier C).
	writeJSON(w, map[string]any{"ok": false, "confirm": "keychange", "question": keyring.KeyChangeQuestion(e.u.How(), true, what)})
	return true
}

// changePasswordAction sets a new password (§9.1): only the key file changes; the
// messages, the remembered key and a copy made before keep theirs.
func (d *dashboard) changePasswordAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readKeyRequest(w, r)
	if !ok {
		return
	}
	defer func() { in.NewPassword, in.Again = "", "" }()
	out, e := d.openHere(w)
	if e == nil {
		return
	}
	if p := keyring.NewPasswordProblem(in.NewPassword); p != "" {
		writeJSON(w, map[string]any{"ok": false, "error": p})
		return
	}
	if in.NewPassword != in.Again {
		writeJSON(w, map[string]any{"ok": false, "error": "The two new passwords do not match."})
		return
	}
	if askFirst(w, e, in.Confirm, "its password can be changed") {
		return
	}
	lease, err := e.u.LeaseForWrite(keyring.OpenOptions{Store: d.cfg.Store})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer lease.Destroy()
	// A lock that lands while the new password's key is derived wins: the change is
	// not made on an archive the person locked (the P3-3 pass, KEY-03).
	_, key := keyOf(out)
	defer d.cfg.Keys.watchLock(key, lease.Lock)()
	if !d.cfg.Keys.ifOpen(out, e, func() {}) {
		writeJSON(w, map[string]any{"ok": false, "error": "This encrypted archive was locked: nothing was changed."})
		return
	}
	err = keyring.ChangePassword(lease, in.NewPassword)
	switch {
	case err == nil:
		writeJSON(w, map[string]any{"ok": true, "lines": []string{"The password was changed. " + keyring.PasswordHint}})
	case errors.Is(err, vault.ErrPartiallySaved):
		writeJSON(w, map[string]any{"ok": true, "lines": []string{"The new password is in effect; the second copy of the key file is rewritten the next time the archive is opened."}})
	default:
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
	}
}

// newRecoveryAction makes a new recovery key (§9.2, K2): shown once, then typed back
// before it takes effect; the earlier one works until then.
func (d *dashboard) newRecoveryAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readKeyRequest(w, r)
	if !ok {
		return
	}
	out, e := d.openHere(w)
	if e == nil || askFirst(w, e, in.Confirm, "a new recovery key can be made") {
		return
	}
	lease, err := e.u.LeaseForWrite(keyring.OpenOptions{Store: d.cfg.Store})
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	rec, err := keyring.NewRecoveryKey(lease)
	if err != nil {
		lease.Destroy()
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Kept only while the archive is still open here: one locked while the key was
	// made — by the person, the idle lock, a switch or Shut down — never keeps a
	// prepared change (the P3-3 pass, KEY-03).
	abs, key := keyOf(out)
	it := &pendingItem{key: key, out: abs, rec: rec, lease: lease}
	var token string
	if !d.cfg.Keys.ifOpen(out, e, func() { token = d.pending.put(it) }) {
		it.discard()
		writeJSON(w, map[string]any{"ok": false, "error": "This encrypted archive was locked while the new recovery key was made: nothing was changed."})
		return
	}
	if token == "" {
		writeJSON(w, map[string]any{"ok": false, "error": errKeysClosed.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "token": token, "recoveryKey": rec.RecoveryKey(), "warning": keyring.RecoveryWarning})
}

// keep puts it in the store and answers with its token, or answers that the Desktop
// is shutting down; false when it answered.
func (d *dashboard) keep(w http.ResponseWriter, it *pendingItem) (string, bool) {
	token := d.pending.put(it)
	if token == "" {
		writeJSON(w, map[string]any{"ok": false, "error": errKeysClosed.Error()})
		return "", false
	}
	return token, true
}

// notWaiting answers that a prepared key change is gone.
func notWaiting(w http.ResponseWriter) {
	writeJSON(w, map[string]any{"ok": false, "error": "That new recovery key is no longer waiting — make a new one."})
}

// recoveryFileName is the recovery file's name in the folder the person picks.
func recoveryFileName(out string) string {
	return "MailArchive recovery file — " + filepath.Base(out) + ".txt"
}

// saveRecoveryFileAction saves the new recovery key's recovery file in a folder the
// person picks with the system's dialog (§9.4): a new file, owner-only, outside every
// archive — never sent to the page. A file saved earlier for the same key is voided.
func (d *dashboard) saveRecoveryFileAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readKeyRequest(w, r)
	if !ok {
		return
	}
	it := d.pending.get(in.Token, isKeyChange)
	if it == nil {
		notWaiting(w)
		return
	}
	ctx, cancel := context.WithTimeout(d.life.Context(), pickerBudget)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()
	dir, err := d.pick(ctx, "Choose a folder for the recovery file (outside the archive)")
	if err != nil || strings.TrimSpace(dir) == "" {
		writeJSON(w, map[string]any{"ok": true, "canceled": true})
		return
	}
	// Written before it is attached, then attached only while the key still waits: a
	// key discarded while the dialog was open — a Lock, a switch, Cancel, its ten
	// minutes — never keeps a recovery file (KEY-07), and no disk work is done under
	// the store's lock (the P3-3 follow-up pass, LIFE-03). The file is named for, and
	// kept outside, the archive the key was made for (HTTP-04).
	var data []byte
	var derr error
	if !d.pending.withItem(in.Token, it, func() { data, derr = it.recoveryFile() }) {
		notWaiting(w)
		return
	}
	if derr != nil {
		writeJSON(w, map[string]any{"ok": false, "error": derr.Error()})
		return
	}
	path, err := keyring.WriteNewFile("The file chosen", "the recovery file", filepath.Join(dir, recoveryFileName(it.out)), it.out, data)
	clear(data)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var earlier string
	if !d.pending.withItem(in.Token, it, func() { earlier, it.file = it.file, path }) {
		_ = keyring.VoidFile(path)
		notWaiting(w)
		return
	}
	if earlier != "" {
		_ = keyring.VoidFile(earlier)
	}
	writeJSON(w, map[string]any{"ok": true, "path": path, "note": keyring.RecoveryFileNote})
}

// confirmRecoveryAction puts the new recovery key in effect once it was typed back
// (K2), and says what it does not withdraw (§9.2, §9.4).
func (d *dashboard) confirmRecoveryAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readKeyRequest(w, r)
	if !ok {
		return
	}
	defer func() { in.Typed = "" }()
	it := d.pending.get(in.Token, isKeyChange)
	if it == nil {
		notWaiting(w)
		return
	}
	var shown string
	d.pending.withItem(in.Token, it, func() { shown = it.recoveryKey() })
	if shown == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "Type your new password first; its recovery key is shown next."})
		return
	}
	if err := keyring.CheckRecoveryKey(shown, in.Typed); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "That is not the recovery key shown (" + err.Error() + "). Type it again from where you saved it."})
		return
	}
	if it = d.pending.take(in.Token, isKeyChange); it == nil {
		notWaiting(w)
		return
	}
	if it.res != nil {
		d.commitRestore(w, it)
		return
	}
	if it.make != nil {
		d.commitCreate(w, it)
		return
	}
	// The archive must still be open here, as the one the key was made for: a lock
	// discards the item, so this only closes the moment between (the P3-3 pass, KEY-03).
	if e := d.cfg.Keys.get(it.out); e == nil || e.u.ArchiveID() != it.lease.ArchiveID() {
		it.discard()
		writeJSON(w, map[string]any{"ok": false, "error": "This encrypted archive was locked: the new recovery key was not saved, and the earlier one still works."})
		return
	}
	err := it.rec.Commit()
	it.lease.Destroy()
	if err != nil && !errors.Is(err, vault.ErrPartiallySaved) {
		it.rec.Discard() // the reply says to make a new one: this one's key goes now (KEY-04)
		if it.file != "" {
			_ = keyring.VoidFile(it.file)
		}
		writeJSON(w, map[string]any{"ok": false, "error": "The new recovery key was not saved, and the earlier one still works: " + err.Error()})
		return
	}
	lines := []string{"The new recovery key is in effect."}
	if it.file != "" {
		lines = append(lines, keyring.EarlierKeyWarning, keyring.RecoveryFileNote)
	} else {
		lines = append(lines, keyring.NoRecoveryFileWarning("New recovery key on this card, saving its recovery file"))
	}
	writeJSON(w, map[string]any{"ok": true, "lines": lines})
}

// cancelRecoveryAction forgets a new recovery key not yet in effect; the earlier one
// stays, and a recovery file saved for the new one is voided.
func (d *dashboard) cancelRecoveryAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readKeyRequest(w, r)
	if !ok {
		return
	}
	d.pending.discard(in.Token)
	writeJSON(w, map[string]any{"ok": true})
}

// rememberAction remembers the key on this computer, or forgets it (§9.3, O3): an
// archive opened by its remembered key is locked when the key is forgotten, so it
// asks for the password next time, as the person was told.
func (d *dashboard) rememberAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readKeyRequest(w, r)
	if !ok {
		return
	}
	out, e := d.openHere(w)
	if e == nil {
		return
	}
	if !in.On {
		if err := keyring.ForgetKeyOf(e.u, d.cfg.Store); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if e.u.How() == keyring.ByRememberedKey {
			d.cfg.Keys.lock(out)
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	askedReRecord := false
	lease, err := e.u.LeaseForWrite(keyring.OpenOptions{Store: d.cfg.Store, ConfirmReRecord: func(string) (bool, error) {
		askedReRecord = !in.ReRecord
		return in.ReRecord, nil
	}})
	if err != nil {
		if askedReRecord && errors.Is(err, keyring.ErrReRecordRefused) {
			writeJSON(w, map[string]any{"ok": false, "confirm": "rerecord",
				"error": "You once told MailArchive to forget this encrypted archive on this computer. Remembering its key records it here again. Go ahead?"})
			return
		}
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer lease.Destroy()
	if err := keyring.Remember(lease, d.cfg.Store); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
