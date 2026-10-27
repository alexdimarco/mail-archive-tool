package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// What the encrypted-archive verbs judge before they ask for anything (P3-8
// friction review: T2-1, T2-2, B12, B15, B16): every refusal that needs no secret
// comes before the first prompt, said once, in plain words, with what to do.

// worded is a refusal worded for a person that still matches, by errors.Is, the
// error it stands for.
type worded struct {
	msg string
	err error
}

func (w *worded) Error() string { return w.msg }
func (w *worded) Unwrap() error { return w.err }

// refuseBusy refuses a keyed verb on an archive another MailArchive run is using,
// before anything is asked or derived (T2-1, as verify -full did; B15): what holds
// it and since when, once — never the lock's own diagnostics.
func refuseBusy(out string) error {
	lock := filepath.Join(out, lockfile.Name)
	if !lockfile.Held(lock) {
		return nil
	}
	since := ""
	if t, ok := lockStarted(lock); ok {
		since = " (started " + t.Local().Format("15:04 on 2006-01-02") + ")"
	}
	return &worded{
		msg: fmt.Sprintf("another backup or check is using the encrypted archive %s%s — it is being written or checked right now. Nothing was asked or changed: try again when it finishes.", out, since),
		err: lockfile.ErrHeld,
	}
}

// lockStarted reads when the lock's holder started, from its holder line.
func lockStarted(lock string) (time.Time, bool) {
	for _, field := range strings.Fields(lockfile.Holder(lock)) {
		if v, ok := strings.CutPrefix(field, "started="); ok {
			t, err := time.Parse(time.RFC3339, v)
			return t, err == nil
		}
	}
	return time.Time{}, false
}

// keylessRefusal is the refusal of a folder this computer recorded as an
// encrypted archive that no longer holds it whole: the case's own words — the
// folder named once, in the command to type — instead of "open it with its
// password", which cannot work there (B12). Any other refusal is kept as it is.
func keylessRefusal(kerr error, out string) error {
	if !errors.Is(kerr, app.ErrEncryptedNeedsKey) {
		return kerr
	}
	res, _ := encmark.At(abspath(out))
	if !res.Recorded || res.Case == encmark.None || res.ByRecordErr || res.Why == "" {
		return kerr
	}
	return &worded{msg: res.Why, err: kerr}
}

// refuseKeysGone refuses opening an encrypted archive neither of whose key files
// reads — before any password is asked, since none could open it (T2-2) — naming
// how to put them back (B12).
func refuseKeysGone(out string) error {
	if !encmark.KeysGone(out) || !app.UnlockableAt(out) {
		return nil
	}
	return &worded{msg: encmark.Wording(encmark.KeysMissing, out), err: vault.ErrVaultMissing}
}

// judgeKeyedFolder refuses a key change (change-password, new-recovery-key) on a
// folder it cannot work on, before any prompt (B16): a recorded folder no longer
// holding its archive in its case's words, a folder that is not there, a folder
// inside an archive, a folder that is no encrypted archive.
func judgeKeyedFolder(out string) error {
	if app.UnlockableAt(out) {
		return refuseKeysGone(out)
	}
	res, _ := encmark.At(out)
	switch {
	case res.ByRecordErr:
		return keyring.CheckRecordUsable(out)
	case res.Recorded && res.Case != encmark.None && res.Why != "":
		return &worded{msg: res.Why, err: app.ErrEncryptedNeedsKey}
	case res.Encrypted && res.Folder != "" && res.Folder != out:
		return fmt.Errorf("%s is inside the encrypted archive %s — give the archive's own folder: -out %s", out, res.Folder, encmark.CommandPath(res.Folder))
	}
	if _, err := os.Stat(out); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s does not exist. Is the drive that holds it plugged in? Nothing was asked or changed", out)
	}
	return fmt.Errorf("%s is not an encrypted archive, so it has no password or recovery key to change; nothing was asked or changed", out)
}

// keyVerbTerminal refuses a key verb without a person at the terminal, in the
// verb's own words (B17): what it needs the terminal for, and nothing about
// scheduled backups, which it is not.
func keyVerbTerminal(verb string, k *keyFlags) error {
	var why string
	switch verb {
	case "change-password":
		why = "the new password is typed there, twice"
		if k != nil && ((k.passwordFile != nil && *k.passwordFile != "") || (k.recoveryKeyFile != nil && *k.recoveryKeyFile != "")) {
			why += " — a file given with -password-file or -recovery-key-file gives only the current secret, never the new password"
		}
	case "new-recovery-key":
		why = "the new recovery key is shown there, and typed back to prove it was saved"
	case "restore-key-file":
		why = "a new password is typed there, and a new recovery key is shown and typed back"
	case "forget-key":
		why = "it needs no password, but it changes what this computer remembers, so you run it yourself"
	case "forget-archive":
		why = "you confirm there which folder this computer stops protecting"
	default:
		why = "it asks you questions there"
	}
	return fmt.Errorf("mailarchive %s needs you at a terminal: %s. Run it in a terminal window (or use the Encryption card in MailArchive Desktop)", verb, why)
}
