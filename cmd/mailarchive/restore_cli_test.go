package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// covers: MA-792, MA-786, R37
// The recovery file saved at -encrypt with -recovery-file-out puts back the
// archive's lost key files through restore-key-file -from, with its recovery key:
// a new password, a new recovery key shown, taken away and entered again, saved
// again with -recovery-file-out, and the words for what it does and does not
// withdraw. The old password no longer opens it.
func TestTheCLIPutsBackALostKeyFileFromItsRecoveryFile(t *testing.T) {
	flowRecord(t)
	out := filepath.Join(t.TempDir(), "archive")
	file := filepath.Join(t.TempDir(), "recovery-file.json")
	k := noKeyFlags()
	k.recoveryFileOut = &file
	a := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", keyAnswer}}
	u, err := createEncrypted(a, out, k, false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	rk := a.shown[0]
	if !exists(file) || !strings.Contains(a.transcript(), "opens nothing on its own") {
		t.Fatalf("the recovery file was not saved and explained:\n%s", a.transcript())
	}
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}

	newFile := filepath.Join(t.TempDir(), "new-recovery-file.json")
	k = noKeyFlags()
	k.recoveryFileOut = &newFile
	const newPassword = "the restored cli passphrase"
	a = &scripted{isAttended: true, answers: []string{rk, newPassword, newPassword, keyAnswer}}
	if err := restoreKeyFile(a, out, file, k); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	tr := a.transcript()
	for _, want := range []string{"is back", "no longer opens it", "destroy recovery files saved with your earlier recovery key"} {
		if !strings.Contains(tr, want) {
			t.Fatalf("the restore never said %q:\n%s", want, tr)
		}
	}
	assure.Reached(t, exists(newFile), "the new recovery file saved")
	if u, err := keyring.Open(out, keyring.Password(newPassword), keyring.OpenOptions{}); err != nil {
		t.Fatalf("the new password: %v", err)
	} else {
		u.Destroy()
	}
	if _, err := keyring.Open(out, keyring.Password(flowPassword), keyring.OpenOptions{}); !errors.Is(err, vault.ErrWrongSecret) {
		t.Fatalf("the old password still opens: %v", err)
	}
}

// covers: MA-792, R37
// restore-key-file -remembered asks the K13 question with the R-E2 disclosure;
// "no" changes nothing, "yes" rebuilds the key files. Without a terminal it is
// refused before anything is read; and -from and -remembered are exactly one.
func TestTheCLIRebuildsKeyFilesFromTheRememberedKey(t *testing.T) {
	flowRecord(t)
	out := filepath.Join(t.TempDir(), "archive")
	a := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "", keyAnswer}}
	u, err := createEncrypted(a, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	a = &scripted{isAttended: true, answers: []string{"n"}}
	err = restoreKeyFile(a, out, "", noKeyFlags())
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("nothing was changed"),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
	if !strings.Contains(a.transcript(), "not this computer") {
		t.Fatalf("the K13 question lacks the R-E2 disclosure:\n%s", a.transcript())
	}
	a = &scripted{isAttended: true, answers: []string{"y", "a rebuilt cli passphrase", "a rebuilt cli passphrase", keyAnswer}}
	if err := restoreKeyFile(a, out, "", noKeyFlags()); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}

	a = &scripted{isAttended: false}
	err = restoreKeyFile(a, out, "", noKeyFlags())
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("terminal"), assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))
	for _, args := range [][]string{
		{"restore-key-file", "-out", out},
		{"restore-key-file", "-out", out, "-from", "f", "-remembered"},
	} {
		code, stderr := runCLI(args...)
		assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-from", "-remembered"))
	}
	if _, err := parseJob([]string{"restore-key-file", "-out", out}); err == nil {
		t.Fatal("restore-key-file was scheduled")
	}
}
