package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
	"mail-archive-tool/internal/wizard"
)

// The P3-8 friction review's Desktop follow-ups (docs/review-encrypted-archives-friction.md):
// B14's second voice, B27's Desktop half, B23 in the Desktop, and the copy plan's size.

// covers: MA-852, R37
// B14's second voice: a password or recovery key that does not open the archive is
// answered in the command line's words — nothing was changed, try the password or the
// recovery key, and if neither opens it, the exact command that puts back key files
// that may have been replaced — never the vault's own words, never what was typed;
// nothing is opened. The positive twin first: the right password opens it.
func TestTheDesktopsWrongSecretSpeaksAsTheCommandLine(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword}); m["ok"] != true || e.cfg.Keys.get(out) == nil {
		t.Fatalf("positive twin: the right password opens it: %v", m)
	}
	e.post(t, "api/encryption/lock", nil)
	code, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": "not the password at all"})
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK),
		assure.Names("does not open this archive", "nothing was changed", "the recovery key if you have forgotten it",
			"mailarchive restore-key-file -out", "-from FILE"),
		assure.Forbid("vault:", "not the password at all"),
		assure.NoSideEffect(func() bool { return e.cfg.Keys.get(out) == nil }))
	msg, _ := m["error"].(string)
	assure.Reached(t, strings.Contains(msg, "mailarchive restore-key-file -out "+encmark.CommandPath(out)+" -from FILE"), "the exact command for this archive: "+msg)
}

// covers: MA-852, R37
// B14's second voice, for a recovery key typed back: a wrong one is refused in the
// same words the command line uses (keyring.RecoveryKeyRetry) — a typing mistake, not a
// recovery key, or another recovery key — never the vault's own words, and the new key
// is not in effect; the one shown, typed back, then takes effect (the positive twin).
func TestARecoveryKeyTypedBackWrongIsToldAsTheCommandLineTellsIt(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	token, key := m["token"].(string), m["recoveryKey"].(string)
	if m["ok"] != true || token == "" || key == "" {
		t.Fatalf("prepared: %v", m)
	}
	typo := []byte(key)
	if typo[0] == 'A' {
		typo[0] = 'B'
	} else {
		typo[0] = 'A'
	}
	for _, typed := range []string{"not a recovery key", string(typo)} {
		want := keyring.RecoveryKeyRetry(keyring.CheckRecoveryKey(key, typed))
		code, m := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": typed})
		_, openErr := keyring.Open(out, keyring.RecoveryKey(key), keyring.OpenOptions{})
		assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names(want), assure.Forbid("vault:", "(vault"),
			assure.NoSideEffect(func() bool { return m["error"] == want && openErr != nil }))
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": key})
	u, err := keyring.Open(out, keyring.RecoveryKey(key), keyring.OpenOptions{})
	if err == nil {
		u.Destroy()
	}
	assure.Reached(t, m["ok"] == true && err == nil, "the recovery key shown, typed back, takes effect")
}

// covers: MA-852, R37
// B27's Desktop half: the forget plan shows the folder as its row names it — the letter
// case kept — not in the record's case-folded comparison form, and typing back exactly
// what is shown forgets it; the result names it the same way.
func TestTheForgetPlanShowsThePathAsTheRowNamesIt(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	planted := recordedArchive(t, t.TempDir(), "Planted-MixedCase")
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(planted, name))
	}
	os.WriteFile(filepath.Join(planted, "something-else.txt"), []byte("x"), 0o600)
	e := newKeysEnv(t, planted)
	_, m := e.post(t, "api/encryption/forget/plan", map[string]any{"path": planted})
	plan := toText(map[string]any{"lines": m["lines"]})
	// The record folds the case on a Mac and on Windows; the row's name has it.
	if m["strong"] != true || !strings.Contains(plan, "at "+planted+".") || strings.Contains(plan, "planted-mixedcase") {
		t.Fatalf("the plan shows %s as its row names it, not case-folded:\n%s", planted, plan)
	}
	_, done := e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "typed": planted})
	rec, _ := encrecord.Load()
	_, stillThere := rec.Lookup(planted)
	assure.Reached(t, done["ok"] == true && !stillThere && strings.Contains(toText(done), planted),
		"the path typed back exactly as shown forgets it: "+toText(done))
}

// covers: MA-852, R37
// B23 in the Desktop: Run now's result says the copies of MailArchive it wrote or
// removed — tens of megabytes on the archive's drive — in the backup's one line. The
// positive twin first: a Run now that changes no copies says nothing of them. Turned
// on, the next Run now says it wrote the copy, which is in the program folder; turned
// off, the next says it removed it, as the person chose.
func TestRunNowSaysWhatItDidToTheCopiesOfMailArchive(t *testing.T) {
	out := backedUpEncrypted(t)
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	runJobFor(t, out, box)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	runNow := func(what string) string {
		t.Helper()
		if code, m := e.post(t, "api/run-backup", nil); code != http.StatusOK || m["ok"] != true {
			t.Fatalf("%s: run now: %d %v", what, code, m)
		}
		result, errText := waitIdle(t, e)
		if errText != "" || !strings.Contains(result, "Backup complete") {
			t.Fatalf("%s: %q %q", what, result, errText)
		}
		return result
	}
	if same := runNow("no copies chosen"); strings.Contains(same, programLinePrefix) {
		t.Fatalf("positive twin: a Run now that changes no copies says nothing of them: %s", same)
	}
	if _, m := e.post(t, "api/encryption/program-copies", map[string]any{"on": true}); m["ok"] != true {
		t.Fatalf("copies on: %v", m)
	}
	wrote := runNow("copies on")
	program, _ := os.ReadDir(filepath.Join(out, container.ProgramDir))
	if !strings.Contains(wrote, "Copies of MailArchive: wrote 1 file(s)") || len(program) == 0 {
		t.Fatalf("Run now's result does not say it wrote the copy (%d in program/): %s", len(program), wrote)
	}
	e.post(t, "api/encryption/program-copies", map[string]any{"on": false})
	removed := runNow("copies off")
	assure.Reached(t, strings.Contains(removed, "Copies of MailArchive: removed 1 file(s)") && strings.Contains(removed, "as you chose") &&
		strings.Contains(removed, "Open archive"), "Run now's result says it removed the copy: "+removed)
}

// covers: MA-852, R37
// B23 in the Desktop, for a new archive: its first backup's result says the copy of
// MailArchive it wrote, when the box was left ticked; unticked, it says nothing of copies
// (the positive twin).
func TestTheFirstBackupSaysItWroteTheCopyOfMailArchive(t *testing.T) {
	const pw = "the new archive's own passphrase"
	first := func(t *testing.T, programCopies any) string {
		t.Helper()
		t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
		box := filepath.Join(t.TempDir(), "Inbox")
		os.WriteFile(box, []byte(keysMbox), 0o600)
		out := filepath.Join(t.TempDir(), "New")
		orig := addEncryptedFn
		t.Cleanup(func() { addEncryptedFn = orig })
		addEncryptedFn = func(check func(string) error) (*wizard.PendingAdd, error) {
			return wizard.PendingAddFor(out, []string{box}), check(out)
		}
		e := newKeysEnv(t, archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"}))
		_, m := e.post(t, "api/encryption/create", map[string]any{})
		body := map[string]any{"token": m["token"], "newPassword": pw, "again": pw}
		if programCopies != nil {
			body["programCopies"] = programCopies
		}
		_, k := e.post(t, "api/encryption/create/key", body)
		if _, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": m["token"], "typed": k["recoveryKey"]}); c["running"] != true {
			t.Fatalf("made: %v %v", k, c)
		}
		result, errText := waitIdle(t, e)
		if errText != "" || !strings.Contains(result, "Backup complete") {
			t.Fatalf("the first backup: %q %q", result, errText)
		}
		return result
	}
	if unticked := first(t, false); strings.Contains(unticked, programLinePrefix) {
		t.Fatalf("positive twin: unticked, the first backup says nothing of copies: %s", unticked)
	}
	ticked := first(t, nil)
	assure.Reached(t, strings.Contains(ticked, "Copies of MailArchive: wrote 1 file(s)") && strings.Contains(ticked, "README.txt lists them"),
		"the first backup's result says it wrote the copy: "+ticked)
}

// covers: MA-852, R37
// The copy plan's size (P3-8 friction review, w3 c6): about the mail's own size and,
// apart from it, the room kept free for the copy's pages, report and README — never
// their sum as one "up to" figure that reads as the copy's size; the free space is
// still shown. The card's plan says it so.
func TestTheCopyPlanShowsTheMailApartFromTheReserve(t *testing.T) {
	const mail, reserve = 10 << 20, 32 << 20
	p := &app.DecryptPlan{MailBytes: mail, ReserveBytes: reserve, NeedBytes: mail + reserve, FreeBytes: 1 << 30, FreeKnown: true}
	got := copySizeText(p)
	want := "about 10.0 MiB of mail, plus up to 32.0 MiB kept free for its pages, report and README (1.0 GiB free)"
	if got != want || strings.Contains(got, "42.0 MiB") {
		t.Fatalf("the size: %q", got)
	}
	out := backedUpEncrypted(t)
	pickingParent(t, t.TempDir())
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m := e.post(t, "api/encryption/copy/plan", nil)
	lines := strings.Join(toLines(m["lines"]), "\n")
	assure.Reached(t, m["ok"] == true && strings.Contains(lines, "Size: about ") && strings.Contains(lines, " of mail, plus up to ") &&
		strings.Contains(lines, "kept free for its pages, report and README"), lines)
}
