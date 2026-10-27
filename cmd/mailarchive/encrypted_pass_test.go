package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/tty"
	"mail-archive-tool/internal/vault"
)

// refused maps an error to assure.Refused's (code, message).
func refused(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

// covers: MA-790, MA-786, R37
// -recovery-key-out is written only once the key was entered again, just before
// the commit: a create abandoned before then leaves no file; a new recovery key
// whose save then fails has its file removed and is told NOT in effect, the
// earlier key still working (the P3-1a pass, SEC-01, CON-03); and the key never
// entered again, to the limit, commits nothing in the product's own words (FID-04).
func TestARecoveryKeyFileIsKeptOnlyForAKeyInEffect(t *testing.T) {
	flowRecord(t)
	keyOut := filepath.Join(t.TempDir(), "recovery.txt")
	k := noKeyFlags()
	k.recoveryKeyOut = &keyOut
	wrong := []string{flowPassword, flowPassword, "n", ""}
	for i := 0; i < 15; i++ {
		wrong = append(wrong, "AAAA-AAAA-AAAA-AAAA-AAAA-AAAA-AAA")
	}
	out := filepath.Join(t.TempDir(), "archive")
	a := &scripted{isAttended: true, answers: wrong}
	_, err := createEncrypted(a, out, k, false, nil)
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("not entered again", "nothing was saved"),
		assure.NoSideEffect(func() bool { return !exists(keyOut) && !exists(filepath.Join(out, vault.FileName)) }))

	// A new recovery key whose save fails: the archive's lock is taken while the key
	// is on screen.
	out = makeArchive(t)
	var held *lockfile.Lock
	a = &scripted{isAttended: true, answers: []string{flowPassword, "", keyAnswer}, onShow: func() {
		held, _ = lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	}}
	k = noKeyFlags()
	k.recoveryKeyOut = &keyOut
	err = newRecoveryKey(a, out, k)
	if held != nil {
		held.Release()
	}
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("in use"),
		assure.NoSideEffect(func() bool { return !exists(keyOut) }))
	tr := a.transcript()
	if !strings.Contains(tr, "NOT in effect") || !strings.Contains(tr, "earlier recovery key still opens") {
		t.Fatalf("the person was not told the new key is not in effect:\n%s", tr)
	}
	if _, err := keyring.Open(out, keyring.RecoveryKey(a.shown[0]), keyring.OpenOptions{}); !errors.Is(err, vault.ErrWrongSecret) {
		t.Fatalf("the key shown opens the archive though its save failed: %v", err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// covers: MA-790, MA-786
// A new password that reads as a recovery key is refused at the prompt (the P3-1a
// pass, SEC-03); a folder that holds files is refused before any question (REG-03).
func TestTheCLIRefusesWhatWouldGoWrongBeforeAsking(t *testing.T) {
	flowRecord(t)
	out := filepath.Join(t.TempDir(), "archive")
	const shaped = "mango umbrella umbrella hammer"
	a := &scripted{isAttended: true, answers: []string{shaped, flowPassword, flowPassword, "n", "", keyAnswer}}
	u, err := createEncrypted(a, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	assure.Reached(t, strings.Contains(a.transcript(), "reads as a recovery key"), "the key-shaped password refused")

	full := filepath.Join(t.TempDir(), "full")
	os.MkdirAll(full, 0o700)
	os.WriteFile(filepath.Join(full, "index.html"), []byte("x"), 0o600)
	a = &scripted{isAttended: true}
	_, err = createEncrypted(a, full, noKeyFlags(), false, nil)
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(full, "new or empty"),
		assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))
}

// covers: MA-790, MA-787, R37
// An archive whose password reads as a recovery key — set before such passwords
// were refused — still opens when that password is typed: what fails as a recovery
// key is tried as a password (the P3-1a pass, SEC-03). A recovery key typed opens
// as one.
func TestATypedSecretIsTriedAsWhatItCouldBe(t *testing.T) {
	flowRecord(t)
	const shaped = "mango umbrella umbrella hammer"
	out := filepath.Join(t.TempDir(), "old")
	os.MkdirAll(out, 0o700)
	p, err := vault.PrepareCreate(out, shaped)
	if err != nil {
		t.Fatal(err)
	}
	rk := p.RecoveryKey().String()
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	for _, typed := range []string{shaped, rk} {
		a := &scripted{isAttended: true, answers: []string{typed, "n"}}
		u, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
		if err != nil {
			t.Fatalf("typed %q: %v\n%s", typed, err, a.transcript())
		}
		u.Destroy()
	}
	assure.Reached(t, recordedAt(t, out), "the folder recorded by the write opens")
}

// covers: MA-790, MA-787, R37
// Finishing an unrecorded archive (one handed over, or whose setting up was cut
// short) offers Remember again, on by default (§5.1 step 7; the P3-1a pass,
// CON-07); and Ctrl-C at a question asked inside an open ends as an interruption,
// exit 130, not a refusal (CON-08).
func TestFinishingSetUpOffersRememberAndQuestionsCanBeInterrupted(t *testing.T) {
	store := flowRecord(t)
	out := makeArchive(t)
	id := recordedAt(t, out)
	change := func(fn func(*encrecord.Record)) {
		if _, err := encrecord.Update("test", func(r *encrecord.Record) error { fn(r); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	change(func(r *encrecord.Record) { delete(r.Archives, id) })
	a := &scripted{isAttended: true, answers: []string{flowPassword, ""}}
	u, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	assure.Reached(t, keyring.HasRemembered(out, store), "the key remembered after Enter at the question")

	change(func(r *encrecord.Record) { r.ForgetID(id) })
	a = &scripted{isAttended: true, answers: []string{flowPassword}, interrupt: "Record it again"}
	_, err = unlockForWrite(a, out, "a backup into it", noKeyFlags())
	var ie *tty.ErrInterrupted
	if !errors.As(err, &ie) {
		t.Fatalf("Ctrl-C at the record-again question: %v", err)
	}
	if ee, ok := interrupted(err, &strings.Builder{}).(*exitError); !ok || ee.code != 130 {
		t.Fatalf("it exits %v, want 130", interrupted(err, &strings.Builder{}))
	}
}

// covers: MA-790, MA-786, MA-787
// The refusals and words §5.2, §9.1 and K2 promise, through the command line and
// its flows (the P3-1a pass, FID-04, FID-10): -encrypt with -manifest, -unattended
// or -list; -encrypt on an existing encrypted archive; `encrypt`; the key verbs'
// flags; a password change saved in one copy only; a recovery key typed at the
// unlock prompt; K13's yes; -recovery-key-out inside an encrypted archive; and a
// whole-archive forget leaving a tombstone.
func TestTheCLIKeepsItsWord(t *testing.T) {
	for args, want := range map[string]string{
		"-encrypt -manifest X":               "-manifest",
		"-encrypt -unattended":               "unattended",
		"-encrypt -list":                     "-list",
		"encrypt -out X":                     "-encrypt",
		"forget-key -out X -password-file f": "not defined",
	} {
		argv := strings.Fields(strings.ReplaceAll(args, "X", t.TempDir()))
		if !strings.HasPrefix(args, "encrypt") && !strings.HasPrefix(args, "forget") {
			argv = append(argv, "-out", t.TempDir(), "-input", "../../testdata/support.pst")
		}
		code, stderr := runCLI(argv...)
		assure.Refused(t, code, stderr, assure.Code(1), assure.Names(want))
	}
	// change-password takes -remember (it is parsed; without a terminal it is then
	// refused for that).
	code, stderr := runCLI("change-password", "-out", t.TempDir(), "-remember")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("terminal"), assure.Forbid("not defined"))

	store := flowRecord(t)
	out := makeArchive(t)
	code, stderr = runCLI("-encrypt", "-out", out, "-input", "../../testdata/support.pst")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("already an encrypted archive", "without -encrypt"))

	// A recovery key typed at the unlock prompt opens as one.
	a := &scripted{isAttended: true, answers: []string{flowPassword, "", keyAnswer}}
	if err := newRecoveryKey(a, out, noKeyFlags()); err != nil {
		t.Fatal(err)
	}
	rk := a.shown[0]
	a = &scripted{isAttended: true, answers: []string{rk}}
	u, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
	if err != nil || u.How() != keyring.ByRecoveryKey {
		t.Fatalf("a typed recovery key: %v", err)
	}
	u.Destroy()

	// A password change saved in one copy only says so.
	mirror := filepath.Join(out, vault.BackupName)
	os.Remove(mirror)
	os.Mkdir(mirror, 0o700)
	a = &scripted{isAttended: true, answers: []string{flowPassword, "a third cli passphrase", "a third cli passphrase"}}
	if err := changePassword(a, out, noKeyFlags()); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	os.Remove(mirror)
	if !strings.Contains(a.transcript(), "the second copy of the key file is rewritten at the next opening") {
		t.Fatalf("a change saved in one copy only:\n%s", a.transcript())
	}

	// K13: by the remembered key, yes goes ahead.
	yes := true
	k := noKeyFlags()
	k.remember = &yes
	a = &scripted{isAttended: true, answers: []string{"a third cli passphrase"}}
	if u, err = unlockForWrite(a, out, "", k); err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	a = &scripted{isAttended: true, answers: []string{"y", "a fourth cli passphrase", "a fourth cli passphrase"}}
	if err := changePassword(a, out, noKeyFlags()); err != nil {
		t.Fatalf("K13 yes: %v\n%s", err, a.transcript())
	}

	// -recovery-key-out inside an encrypted archive is refused before anything.
	inside := filepath.Join(out, "rk.txt")
	k = noKeyFlags()
	k.recoveryKeyOut = &inside
	a = &scripted{isAttended: true, answers: []string{}}
	err = newRecoveryKey(a, out, k)
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("inside"), assure.NoSideEffect(func() bool { return !exists(inside) }))

	// A whole-archive forget leaves a tombstone.
	id := recordedAt(t, out)
	a = &scripted{isAttended: true, answers: []string{"y", "y", "y"}}
	if err := forgetArchive(a, out); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	rec, _ := encrecord.Load()
	if e := rec.Archives[id]; e == nil || !e.Forgotten || len(e.Paths) != 0 {
		t.Fatalf("a whole forget left %+v", e)
	}
	if keyring.HasRemembered(out, store) {
		t.Fatal("the remembered key was kept")
	}
}
