package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// The P3-8 friction review's command-line and key-wording findings (group G1):
// B9, B12-B17, B24, B27, T2-1, T2-2, T2-8 and the Tier C lows in the same area.

// stderrOf runs f with os.Stderr captured.
func stderrOf(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	got := make(chan string)
	go func() { b, _ := io.ReadAll(r); got <- string(b) }()
	ferr := f()
	w.Close()
	os.Stderr = orig
	return <-got, ferr
}

// covers: MA-850, R37
// -encrypt asks whether to keep copies of MailArchive in the archive, on by
// default, and says what it chose; -program-copies decides it without the
// question (design §5.1 step 4; B9).
func TestEncryptAsksAboutProgramCopiesAndSaysTheChoice(t *testing.T) {
	flowRecord(t)
	// Enter keeps the copies (the default).
	a := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "", keyAnswer}}
	k := noKeyFlags()
	u, err := createEncrypted(a, filepath.Join(t.TempDir(), "on"), k, false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	tr := a.transcript()
	assure.Reached(t, strings.Contains(tr, app.ProgramCopiesLabel(app.ReleaseBuild())) && strings.Contains(tr, "[Y/n]") &&
		strings.Contains(tr, "Copies of MailArchive will be kept") && k.programChoice(true) == app.ProgramCopiesOn,
		"the question asked, on by default, and the choice said: "+tr)

	// n leaves them out, and says how to change it later.
	a = &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "n", keyAnswer}}
	k = noKeyFlags()
	if u, err = createEncrypted(a, filepath.Join(t.TempDir(), "off"), k, false, nil); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	assure.Reached(t, strings.Contains(a.transcript(), "No copies of MailArchive will be kept") && strings.Contains(a.transcript(), "-program-copies on") &&
		k.programChoice(true) == app.ProgramCopiesOff, "no copies, said: "+a.transcript())

	// -program-copies off is the answer: no question, the choice still said.
	off := "off"
	a = &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", keyAnswer}}
	k = noKeyFlags()
	k.programCopies = &off
	if u, err = createEncrypted(a, filepath.Join(t.TempDir(), "flag"), k, false, nil); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	for _, p := range a.prompts {
		if strings.Contains(p, "Keep a copy of MailArchive") {
			t.Fatalf("-program-copies was given, yet the question was asked:\n%s", a.transcript())
		}
	}
	assure.Reached(t, strings.Contains(a.transcript(), "No copies of MailArchive will be kept") && k.programChoice(true) == app.ProgramCopiesOff, "the flag's choice said")
}

// covers: MA-850, R37
// A recorded archive whose folder is gone — a drive unplugged — is refused by a
// keyed verb asking first whether the drive is plugged in, naming the folder only
// in the command to type, and never "open it with its password", which cannot
// work there; one whose key files are missing names restore-key-file (B12).
// Positive twin: the same archive, whole, is searched.
func TestARecordedFolderRefusalCanBeFollowed(t *testing.T) {
	out, pw := backedUpArchive(t)
	text, err := stdout(t, func() error { return runSearch([]string{"-out", out, "-password-file", pw, "Hello"}) })
	if err != nil || !strings.Contains(text, "1 match") {
		t.Fatalf("positive twin: the whole archive is searched: %v\n%s", err, text)
	}

	unplugged := out + "-unplugged"
	if err := os.Rename(out, unplugged); err != nil {
		t.Fatal(err)
	}
	_, err = stdout(t, func() error { return runSearch([]string{"-out", out, "-password-file", pw, "Hello"}) })
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("Is the drive plugged in?", "mailarchive forget-archive -out", out),
		assure.Forbid("with its password or recovery key", "vault:"),
		assure.NoSideEffect(func() bool { return !exists(out) && recordedAt(t, out) != "" }))
	assure.Reached(t, strings.HasPrefix(msg, "Is the drive plugged in?") && strings.Count(msg, out) == 1, "the drive asked first, the folder named once: "+msg)

	// Back, without its key files: put them back, with the exact command.
	if err := os.Rename(unplugged, out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	_, err = stdout(t, func() error { return runSearch([]string{"-out", out, "-password-file", pw, "Hello"}) })
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("key files are missing", "mailarchive restore-key-file -out", "-from FILE", "-remembered"),
		assure.Forbid("with its password or recovery key", "vault:"),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
	assure.Reached(t, strings.Count(msg, out) == 1, "the folder named once: "+msg)
}

// covers: MA-850, R37
// An encrypted archive this computer never recorded, both of whose key files are
// gone, is refused before any password is asked — none could open it — naming
// restore-key-file (T2-2, B12). Positive twin: with its key files, it asks and opens.
func TestKeyFilesGoneRefuseBeforeAnyPassword(t *testing.T) {
	out, _ := backedUpArchive(t)
	flowRecord(t) // another computer: the archive is not recorded here
	a := &scripted{isAttended: true, answers: []string{flowPassword, "n"}}
	u, err := unlockForRead(a, out, "searching it", noKeyFlags())
	assure.Reached(t, u, "positive twin: with its key files it opens")
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	for verb, run := range map[string]func(a *scripted) error{
		"read": func(a *scripted) error {
			_, err := unlockForRead(a, out, "searching it", noKeyFlags())
			return err
		},
		"backup": func(a *scripted) error {
			_, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
			return err
		},
	} {
		a := &scripted{isAttended: true, answers: []string{flowPassword}}
		code, msg := refused(run(a))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("key files are missing", "mailarchive restore-key-file -out"),
			assure.Forbid("vault"), assure.NoSideEffect(func() bool { return len(a.prompts) == 0 }))
		_ = verb
	}
}

// covers: MA-850, R37
// A damaged record of encrypted archives refuses a keyed write before any password
// is asked, naming `mailarchive repair-record`, and changes nothing (B13, T2-2).
// Positive twin: with the record whole, the same open asks and opens.
func TestADamagedRecordNamesRepairRecordBeforeAnyPassword(t *testing.T) {
	flowRecord(t)
	out := makeArchive(t)
	a := &scripted{isAttended: true, answers: []string{flowPassword}}
	u, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
	assure.Reached(t, u, "positive twin: the archive opens with its record whole")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()

	dir, err := encrecord.Dir()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, encrecord.FileName), []byte("not a record\n"), 0o600)
	os.WriteFile(filepath.Join(dir, encrecord.MirrorName), []byte("not one either\n"), 0o600)
	for name, run := range map[string]func(a *scripted) error{
		"backup": func(a *scripted) error {
			_, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
			return err
		},
		"change-password": func(a *scripted) error { return changePassword(a, out, noKeyFlags()) },
	} {
		a := &scripted{isAttended: true, answers: []string{flowPassword}}
		code, msg := refused(run(a))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("damaged", "mailarchive repair-record", "nothing was changed"),
			assure.Forbid("vault:"),
			assure.NoSideEffect(func() bool {
				b, _ := os.ReadFile(filepath.Join(dir, encrecord.FileName))
				return len(a.prompts) == 0 && string(b) == "not a record\n"
			}))
		_ = name
	}
}

// covers: MA-850, R37
// A secret that does not open the archive is refused without the vault's words,
// with the next step — the recovery key, or putting back key files — naming the
// -password-file it read; three typed tries say so; the recovery key typed back
// with a mistake is told so in plain words (B14). Positive twin: the right file
// opens.
func TestAWrongSecretSaysWhatToDoNext(t *testing.T) {
	out, pw := backedUpArchive(t)
	k := noKeyFlags()
	k.passwordFile = &pw
	u, err := unlockForRead(&scripted{}, out, "searching it", k)
	assure.Reached(t, u, "positive twin: the password file opens")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()

	wrong := filepath.Join(t.TempDir(), "wrong-pw")
	os.WriteFile(wrong, []byte("not the password at all\n"), 0o600)
	k = noKeyFlags()
	k.passwordFile = &wrong
	_, err = unlockForRead(&scripted{}, out, "searching it", k)
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(wrong, "-password-file", "recovery key", "restore-key-file", "nothing was changed"),
		assure.Forbid("vault:"), assure.NoSideEffect(func() bool { return exists(filepath.Join(out, vault.FileName)) }))

	a := &scripted{isAttended: true, answers: []string{"wrong one here", "wrong two here", "wrong three here"}}
	_, err = unlockForWrite(a, out, "a backup into it", noKeyFlags())
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("Three tries", "recovery key", "restore-key-file"),
		assure.Forbid("vault:"), assure.NoSideEffect(func() bool { return len(a.prompts) == 3 }))

	// The recovery key typed back with one character changed.
	first := true
	c := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "", "placeholder", keyAnswer}}
	c.onShow = func() {
		if first {
			first = false
			key := []byte(c.shown[0])
			if key[0] == 'A' {
				key[0] = 'B'
			} else {
				key[0] = 'A'
			}
			c.answers[0] = string(key)
		}
	}
	u, err = createEncrypted(c, filepath.Join(t.TempDir(), "typo"), noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, c.transcript())
	}
	u.Destroy()
	tr := c.transcript()
	assure.Reached(t, strings.Contains(tr, "typing mistake") && !strings.Contains(tr, "vault:") && !strings.Contains(tr, "(vault"), "the typo told plainly: "+tr)
}

// covers: MA-850, R37
// An empty Enter at the password prompt asks again without counting as one of the
// three tries (T2-8).
func TestAnEmptyEnterIsNotATry(t *testing.T) {
	flowRecord(t)
	out := makeArchive(t)
	a := &scripted{isAttended: true, answers: []string{"", "", flowPassword}}
	u, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
	assure.Reached(t, u, "opened after two empty Enters")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	tr := a.transcript()
	assure.Reached(t, len(a.prompts) == 3 && strings.Contains(tr, "Nothing was typed") && !strings.Contains(tr, "does not open"), "empty Enters asked again, not counted: "+tr)

	// Three real tries still end it, the empty ones not among them.
	a = &scripted{isAttended: true, answers: []string{"", "wrong one here", "", "wrong two here", "wrong three here"}}
	_, err = unlockForWrite(a, out, "a backup into it", noKeyFlags())
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("Three tries"), assure.NoSideEffect(func() bool { return len(a.prompts) == 5 }))
}

// covers: MA-850, R37
// Every keyed verb that takes the archive's lock refuses a busy archive before any
// question or password, saying what holds it and since when, once, in plain words
// — never the lock's pid, host or nonce (T2-1, B15). Positive twin: released, it
// opens.
func TestABusyArchiveIsRefusedBeforeAnything(t *testing.T) {
	out, pw := backedUpArchive(t)
	holder, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	busy := func(name, msg string, prompts int) {
		t.Helper()
		assure.Refused(t, 1, msg, assure.Code(1), assure.Names("another backup or check is using", out, "started", "try again when it finishes"),
			assure.Forbid("pid=", "host=", "nonce", "vault:"), assure.NoSideEffect(func() bool { return prompts == 0 }))
		if n := strings.Count(msg, "another backup or check"); n != 1 {
			t.Fatalf("%s: the busy refusal said %d times: %s", name, n, msg)
		}
	}
	for name, run := range map[string]func(a *scripted) error{
		"backup": func(a *scripted) error {
			_, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
			return err
		},
		"change-password":  func(a *scripted) error { return changePassword(a, out, noKeyFlags()) },
		"new-recovery-key": func(a *scripted) error { return newRecoveryKey(a, out, noKeyFlags()) },
		"restore-key-file": func(a *scripted) error { return restoreKeyFile(a, out, "", noKeyFlags()) },
		"forget-archive":   func(a *scripted) error { return forgetArchive(a, out) },
		"decrypt": func(a *scripted) error {
			var o, e bytes.Buffer
			return decryptWith(a, out, t.TempDir(), "", noKeyFlags(), decryptDay, &o, &e)
		},
	} {
		a := &scripted{isAttended: true, answers: []string{"y", flowPassword, flowPassword}}
		err := run(a)
		if err == nil {
			t.Fatalf("%s: a busy archive was not refused", name)
		}
		busy(name, err.Error(), len(a.prompts))
	}
	dest := t.TempDir()
	_, err = stdout(t, func() error {
		return runExtract([]string{"-out", out, "-format", "eml", "-dest", dest, "-password-file", pw})
	})
	if err == nil {
		t.Fatal("extract: a busy archive was not refused")
	}
	busy("extract", err.Error(), 0)
	_, err = stdout(t, func() error { return runVerify([]string{"-out", out, "-full", "-password-file", pw}) })
	if err == nil {
		t.Fatal("verify -full: a busy archive was not refused")
	}
	busy("verify -full", err.Error(), 0)

	holder.Release()
	a := &scripted{isAttended: true, answers: []string{flowPassword}}
	u, err := unlockForWrite(a, out, "a backup into it", noKeyFlags())
	assure.Reached(t, u, "positive twin: released, it opens")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
}

// covers: MA-850, R37
// change-password and new-recovery-key judge the folder before any prompt: a plain
// folder is not an encrypted archive, a missing one may be on an unplugged drive
// (B16). Positive twin: the encrypted archive passes the judgment.
func TestKeyChangesJudgeTheFolderFirst(t *testing.T) {
	flowRecord(t)
	out := makeArchive(t)
	assure.Reached(t, judgeKeyedFolder(out) == nil, "positive twin: an encrypted archive is judged fit")
	plain := t.TempDir()
	missing := filepath.Join(t.TempDir(), "not-here")
	for _, c := range []struct {
		dir   string
		names []string
	}{
		{plain, []string{plain, "not an encrypted archive", "nothing was asked or changed"}},
		{missing, []string{missing, "does not exist", "plugged in"}},
	} {
		for verb, run := range map[string]func(a *scripted) error{
			"change-password":  func(a *scripted) error { return changePassword(a, c.dir, noKeyFlags()) },
			"new-recovery-key": func(a *scripted) error { return newRecoveryKey(a, c.dir, noKeyFlags()) },
		} {
			a := &scripted{isAttended: true, answers: []string{flowPassword}}
			code, msg := refused(run(a))
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(c.names...), assure.Forbid("vault", "no vault found"),
				assure.NoSideEffect(func() bool { return len(a.prompts) == 0 && !exists(filepath.Join(c.dir, vault.FileName)) }))
			_ = verb
		}
	}
}

// covers: MA-850, R37
// Without a terminal each key verb says what it needs the terminal for: forget-key
// needs no password; change-password with -password-file is told the new password
// is typed; neither speaks of scheduled backups (B17). Positive twin: forget-key at
// a terminal forgets the key.
func TestTheNoTerminalRefusalFitsTheVerb(t *testing.T) {
	store := flowRecord(t)
	out := makeArchive(t)
	yes := true
	k := noKeyFlags()
	k.remember = &yes
	u, err := unlockForWrite(&scripted{isAttended: true, answers: []string{flowPassword}}, out, "", k)
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()

	pw := filepath.Join(t.TempDir(), "pw")
	os.WriteFile(pw, []byte(flowPassword+"\n"), 0o600)
	k = noKeyFlags()
	k.passwordFile = &pw
	code, msg := refused(changePassword(&scripted{}, out, k))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("terminal", "new password", "-password-file"), assure.Forbid("scheduled", "unattended"))

	code, msg = refused(forgetKey(&scripted{}, out))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("forget-key", "terminal", "needs no password"), assure.Forbid("scheduled", "unlock it with its password"),
		assure.NoSideEffect(func() bool { return keyring.HasRemembered(out, store) }))

	a := &scripted{isAttended: true}
	if err := forgetKey(a, out); err != nil || keyring.HasRemembered(out, store) {
		t.Fatalf("positive twin: forget-key at a terminal: %v", err)
	}
	// Run again with nothing remembered: said as it is, not as a key forgotten.
	a = &scripted{isAttended: true}
	err = forgetKey(a, out)
	assure.Reached(t, err == nil && strings.Contains(a.transcript(), "does not remember a key") && !strings.Contains(a.transcript(), "no longer remembers"),
		"forgetting nothing is not told as forgetting: "+a.transcript())
}

// covers: MA-850, R37
// The saved recovery-key file names its archive's folder and the day beside its
// id, and the run says where it was saved (B24); it still reads back as the key.
func TestTheSavedRecoveryKeyNamesItsArchive(t *testing.T) {
	flowRecord(t)
	out := filepath.Join(t.TempDir(), "My Archive")
	keyOut := filepath.Join(t.TempDir(), "recovery-key.txt")
	k := noKeyFlags()
	k.recoveryKeyOut = &keyOut
	a := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "", keyAnswer}}
	u, err := createEncrypted(a, out, k, false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	id := u.ArchiveID().String()
	u.Destroy()
	b, err := os.ReadFile(keyOut)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	assure.Reached(t, strings.Contains(text, "in "+out) && strings.Contains(text, "made "+time.Now().Format("2006-01-02")) && strings.Contains(text, id),
		"the key file names the folder, the day and the id: "+text)
	assure.Reached(t, strings.Contains(a.transcript(), "Recovery key saved to "+keyOut), "where it was saved, said: "+a.transcript())
	s, err := keyring.RecoveryKeyFile(keyOut)
	if err != nil || !s.IsRecoveryKey() {
		t.Fatalf("the saved key does not read back: %v", err)
	}
}

// covers: MA-850, R37
// forget-archive shows the folder as it was typed, not the record's case-folded
// form, and that path typed back is accepted (B27).
func TestForgetArchiveShowsThePathAsTyped(t *testing.T) {
	flowRecord(t)
	out := filepath.Join(t.TempDir(), "Mixed-Case Archive")
	a := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "n", "", keyAnswer}}
	u, err := createEncrypted(a, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	u.Destroy()
	os.WriteFile(filepath.Join(out, "index.html"), []byte("planted"), 0o600)
	a = &scripted{isAttended: true, answers: []string{out, "n"}}
	if err := forgetArchive(a, out); err != nil {
		t.Fatalf("the path as shown, typed back: %v\n%s", err, a.transcript())
	}
	tr := a.transcript()
	assure.Reached(t, strings.Contains(tr, "\n    "+out) && recordedAt(t, out) == "", "the typed spelling shown and accepted: "+tr)
	if (runtime.GOOS == "darwin" || runtime.GOOS == "windows") && strings.Contains(tr, strings.ToLower(out)) {
		t.Fatalf("the case-folded path was shown:\n%s", tr)
	}
}

// covers: MA-850, R37
// The Tier C lows of the encrypted archive's command line: search prints no file
// path an encrypted archive does not hold; -remember says the key is remembered;
// the record-again question explains itself and its no names the next step.
func TestTheEncryptedCommandLineSaysWhatIsTrue(t *testing.T) {
	out, pw := backedUpArchive(t)
	text, err := stdout(t, func() error { return runSearch([]string{"-out", out, "-password-file", pw, "Hello"}) })
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, strings.Contains(text, "1 match") && !strings.Contains(text, "->"), "a match, and no file path: "+text)

	yes := true
	k := noKeyFlags()
	k.remember = &yes
	a := &scripted{isAttended: true, answers: []string{flowPassword}}
	u, err := unlockForWrite(a, out, "a backup into it", k)
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	assure.Reached(t, strings.Contains(a.transcript(), "now remembers the key of "+out), "-remember confirmed: "+a.transcript())

	// Forgotten, then opened again: the question says what recording does, and no
	// says how to record it later.
	a = &scripted{isAttended: true, answers: []string{"y", "y", "y"}}
	if err := forgetArchive(a, out); err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	if err := forgetKey(&scripted{isAttended: true}, out); err != nil {
		t.Fatal(err)
	}
	a = &scripted{isAttended: true, answers: []string{flowPassword, "n"}}
	_, err = unlockForWrite(a, out, "a backup into it", noKeyFlags())
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("not recorded again", "answer yes"),
		assure.NoSideEffect(func() bool { return recordedAt(t, out) == "" }))
	assure.Reached(t, strings.Contains(a.transcript(), "Answering n opens nothing and changes nothing"), "the question explained: "+a.transcript())
}

// covers: MA-850, R37
// decrypt puts its plan on stderr, keeping stdout for the result, and says the
// count once (X8).
func TestDecryptSaysItsPlanOnStderrAndItsCountOnce(t *testing.T) {
	flowRecord(t)
	out := makeArchive(t)
	var o, e bytes.Buffer
	a := &scripted{isAttended: true, answers: []string{"y", flowPassword}}
	logged, err := stderrOf(t, func() error {
		return decryptWith(a, out, t.TempDir(), "", noKeyFlags(), decryptDay, &o, &e)
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, a.transcript())
	}
	all := o.String() + e.String() + logged
	assure.Reached(t, strings.Contains(e.String(), "UNENCRYPTED copy of") && !strings.Contains(o.String(), "UNENCRYPTED copy of") &&
		strings.Contains(o.String(), "Unencrypted copy made at") && strings.Count(all, "messages copied") == 1,
		"the plan on stderr, the result on stdout, the count once:\n"+all)
}

// covers: MA-850, R37
// A run Ctrl-C stopped does not open its summary with "Done." (Tier C); a run that
// finished does.
func TestAnInterruptedRunIsNotDone(t *testing.T) {
	var b bytes.Buffer
	logger := log.New(&b, "", 0)
	printSummaryLed(logger, app.Result{}, false, false, "/x", summaryLead(context.Canceled))
	stopped := b.String()
	b.Reset()
	printSummaryLed(logger, app.Result{}, false, false, "/x", summaryLead(nil))
	assure.Reached(t, !strings.HasPrefix(stopped, "Done.") && strings.Contains(stopped, "interrupted") && strings.HasPrefix(b.String(), "Done."),
		"stopped: "+stopped)
}

// covers: MA-850, R37
// serve says its bare address opens only through a private link, so the address
// answering "not signed in" is not a surprise (Tier C).
func TestServeSaysTheAddressNeedsTheLink(t *testing.T) {
	out := buildSearchFixture(t)
	g := startSurface(t, rigOpts{stdoutTTY: true, display: true, foreground: true}, serveMain, out, serveArgs(out)...)
	assure.Reached(t, strings.Contains(g.stdout.String(), "that address opens only through a private link"), "the address explained: "+g.stdout.String())
}
