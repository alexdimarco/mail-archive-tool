package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// The P3-1b adversarial pass's fixes, through the command line (MA-793).

// archiveWithFile makes an encrypted archive whose recovery file is saved, drops
// its key files, and returns the folder, the recovery file and its recovery key.
func archiveWithFile(t *testing.T) (out, file, rk string) {
	t.Helper()
	out = filepath.Join(t.TempDir(), "archive")
	file = filepath.Join(t.TempDir(), "recovery-file.json")
	k := noKeyFlags()
	k.recoveryFileOut = &file
	a := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "", keyAnswer}}
	u, err := createEncrypted(a, out, k, false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	return out, file, a.shown[0]
}

const restoredCLIPassword = "the restored cli passphrase"

// covers: MA-793, MA-792, R37
// A restore that wrote the key file, and then could not record the folder, keeps the
// saved copies of the new recovery key — the one in force — and says the key file is
// back, never "NOT put back" (the P3-1b pass, CON-02, FID-02, OUT-01).
func TestTheCLIKeepsTheCopiesOfARecoveryKeyInForce(t *testing.T) {
	flowRecord(t)
	out, file, rk := archiveWithFile(t)
	dir, err := encrecord.Dir()
	if err != nil {
		t.Fatal(err)
	}
	keyOut := filepath.Join(t.TempDir(), "new-key.txt")
	fileOut := filepath.Join(t.TempDir(), "new-file.json")
	k := noKeyFlags()
	k.recoveryKeyOut, k.recoveryFileOut = &keyOut, &fileOut
	var held *lockfile.Lock
	a := &scripted{isAttended: true, answers: []string{rk, restoredCLIPassword, restoredCLIPassword, keyAnswer}, onShow: func() {
		held, _ = lockfile.AcquireAs(filepath.Join(dir, encrecord.LockName), "test")
	}}
	err = restoreKeyFile(a, out, file, k)
	if held != nil {
		held.Release()
	}
	tr := a.transcript()
	if err == nil || strings.Contains(tr, "NOT put back") || !strings.Contains(tr, "the ones that count now") {
		t.Fatalf("a restore in force must say so and still report the record: %v\n%s", err, tr)
	}
	assure.Reached(t, exists(keyOut) && exists(fileOut), "both copies of the recovery key in force are kept")
	u, err := keyring.Open(out, keyring.Password(restoredCLIPassword), keyring.OpenOptions{})
	assure.Reached(t, u, "the new password opens")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
}

// covers: MA-793, MA-792, R37
// A restore that did not take effect voids both saved copies — the recovery key's
// and the recovery file — and says so (MA-792; the P3-1b pass, FID-06).
func TestTheCLIVoidsBothCopiesOfARecoveryKeyNotInEffect(t *testing.T) {
	flowRecord(t)
	out, file, rk := archiveWithFile(t)
	keyOut := filepath.Join(t.TempDir(), "new-key.txt")
	fileOut := filepath.Join(t.TempDir(), "new-file.json")
	k := noKeyFlags()
	k.recoveryKeyOut, k.recoveryFileOut = &keyOut, &fileOut
	a := &scripted{isAttended: true, answers: []string{rk, restoredCLIPassword, restoredCLIPassword, keyAnswer}, onShow: func() {
		os.WriteFile(filepath.Join(out, vault.BackupName), []byte("{}"), 0o600)
	}}
	err := restoreKeyFile(a, out, file, k)
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("changed"),
		assure.NoSideEffect(func() bool { return !exists(keyOut) && !exists(fileOut) && !exists(filepath.Join(out, vault.FileName)) }))
	assure.Reached(t, strings.Contains(a.transcript(), "NOT put back"), "told the key just shown is not in effect")

	// The second copy failing to save voids the first.
	missing := filepath.Join(t.TempDir(), "no-such-folder", "file.json")
	k.recoveryFileOut = &missing
	_, err = saveKeyCopies(a, k, out, vault.ID{1}, rk, func() ([]byte, error) { return []byte("{}"), nil })
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(missing), assure.NoSideEffect(func() bool { return !exists(keyOut) }))
}

// covers: MA-793, MA-792, R37
// The recovery file and the recovery key's typing are checked before the new
// password is asked: a file that is not one is refused asking nothing; a mistyped
// key is asked again; a key that does not open the file is asked again after the
// password, which is kept (the P3-1b pass, CRIT-06). -recovery-key-file is taken too.
func TestTheCLIChecksTheRecoveryFileAndKeyBeforeThePassword(t *testing.T) {
	flowRecord(t)
	out, file, rk := archiveWithFile(t)
	notAFile := filepath.Join(t.TempDir(), "notes.json")
	os.WriteFile(notAFile, []byte(`{"format": 1, "kind": "something else"}`), 0o600)
	a := &scripted{isAttended: true}
	err := restoreKeyFile(a, out, notAFile, noKeyFlags())
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(notAFile, "recovery file"),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))

	_, _, otherKey := archiveWithFile(t)
	fileOut := filepath.Join(t.TempDir(), "new-file.json")
	k := noKeyFlags()
	k.recoveryFileOut = &fileOut
	a = &scripted{isAttended: true, answers: []string{"not a recovery key", otherKey, restoredCLIPassword, restoredCLIPassword, rk, keyAnswer}}
	if err := restoreKeyFile(a, out, file, k); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	tr := a.transcript()
	if !strings.Contains(tr, "That is not a recovery key") || !strings.Contains(tr, "does not open this recovery file") ||
		strings.Count(tr, "Choose a password") != 1 {
		t.Fatalf("a mistyped key and a wrong key are each asked again, the password once:\n%s", tr)
	}

	// -recovery-key-file: the key read from a file.
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	keyFile := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(keyFile, []byte(a.shown[0]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k = noKeyFlags()
	k.recoveryKeyFile = &keyFile
	a = &scripted{isAttended: true, answers: []string{"", "a third cli passphrase", "a third cli passphrase", keyAnswer}}
	if err := restoreKeyFile(a, out, fileOut, k); err != nil {
		t.Fatalf("-recovery-key-file: %v\n%s", err, a.transcript())
	}
	assure.Reached(t, !strings.Contains(a.transcript(), "The recovery key saved with"), "the key file is not asked for at the terminal")
}

// covers: MA-793, MA-792, R37
// Without -recovery-file-out the new recovery file is offered at the terminal: a
// place inside the archive is refused and asked again; a new file outside it is
// saved (§5.1 step 5, §9.2; the P3-1b pass, CRIT-01). With the flag, a place inside
// the archive or over a file is refused before anything is asked, on each verb that
// saves one (FID-06).
func TestTheCLIOffersTheNewRecoveryFile(t *testing.T) {
	flowRecord(t)
	out, file, rk := archiveWithFile(t)
	inside := filepath.Join(out, "inside.json")
	saveAs := filepath.Join(t.TempDir(), "offered.json")
	a := &scripted{isAttended: true, answers: []string{rk, inside, saveAs, restoredCLIPassword, restoredCLIPassword, keyAnswer}}
	if err := restoreKeyFile(a, out, file, noKeyFlags()); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	tr := a.transcript()
	if !strings.Contains(tr, "inside the archive folder") || !exists(saveAs) || exists(inside) || !strings.Contains(tr, "opens nothing on its own") {
		t.Fatalf("the offered recovery file:\n%s", tr)
	}
	if _, err := vault.ParseRecoveryFile(must(os.ReadFile(saveAs))); err != nil {
		t.Fatalf("the offered recovery file does not read as one: %v", err)
	}

	existing := filepath.Join(t.TempDir(), "taken.json")
	os.WriteFile(existing, []byte("x"), 0o600)
	for _, bad := range []string{inside, existing} {
		k := noKeyFlags()
		k.recoveryFileOut = &bad
		for verb, run := range map[string]func(a *scripted) error{
			"-encrypt": func(a *scripted) error {
				_, err := createEncrypted(a, filepath.Join(t.TempDir(), "new"), k, false, nil)
				return err
			},
			"new-recovery-key": func(a *scripted) error { return newRecoveryKey(a, out, k) },
			"restore-key-file": func(a *scripted) error { return restoreKeyFile(a, out, file, k) },
		} {
			a := &scripted{isAttended: true}
			err := run(a)
			code, msg := refused(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names("-recovery-file-out"),
				assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))
			_ = verb
		}
	}
}

// covers: MA-793
// A plain export given only -recovery-file-out is refused naming that flag (R12; the
// P3-1b pass, REG-04); without -out, the subcommands listed include restore-key-file.
func TestTheCLINamesTheKeyFlagGiven(t *testing.T) {
	flowRecord(t)
	code, stderr := runCLI("-out", t.TempDir(), "-input", "../../testdata/support.pst", "-recovery-file-out", filepath.Join(t.TempDir(), "f.json"))
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-recovery-file-out", "encrypted archive"))
	code, stderr = runCLI()
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("restore-key-file", "encrypt)"))
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
