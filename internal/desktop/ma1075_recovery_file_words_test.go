package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// The recovery key told from the recovery file, a second round (MA-1075): the owner's pick Q3 of
// 2026-11-04 (a clearer saved file name) and the independent review's LOW-2, LOW-3 and LOW-6 of
// bc84ca5. Words only, plus one cleared box: nothing weakened, nothing typed quoted or kept.

// covers: MA-1075, R37
// The recovery file saved from the Encryption card is named for what it is not: "MailArchive
// recovery file — NAME (not the recovery key).txt" (Q3). Once its key takes effect, the last line
// speaks of it in the Desktop's own words — never the command line's, with its backticked command
// and "Keep it with your backups" (LOW-3). Typed back, its contents are refused with the box to be
// emptied (LOW-6); a typing mistake is not (the positive twin, first).
func TestTheSavedRecoveryFileIsNamedNotTheRecoveryKey(t *testing.T) {
	out := backedUpEncrypted(t)
	saveDir := t.TempDir()
	orig := pickFolder
	pickFolder = func(context.Context, string) (string, error) { return saveDir, nil }
	t.Cleanup(func() { pickFolder = orig })
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	token, key := str(m["token"]), str(m["recoveryKey"])
	_, s := e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": token})
	path := str(s["path"])
	if s["ok"] != true || filepath.Base(path) != "MailArchive recovery file — Enc (not the recovery key).txt" || filepath.Dir(path) != saveDir {
		t.Errorf("the recovery file's name: %q (%v)", path, s)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": typo(key)}); c["ok"] != false || c["clear"] != nil {
		t.Errorf("the positive twin: a typing mistake is refused and left to fix: %v", c)
	}
	if _, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": string(data)}); c["ok"] != false || c["error"] != recoveryFileTypedBack || c["clear"] != true {
		t.Errorf("the recovery file's contents typed back: %v", c)
	}
	_, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": key})
	said := fmt.Sprint(c["lines"])
	if c["ok"] != true || !strings.Contains(said, recoveryFileLastNote) || strings.Contains(said, keyring.RecoveryFileNote) || strings.Contains(said, "`") || strings.Contains(said, "Keep it with your backups") {
		t.Errorf("the last lines: %v", c)
	}
	assure.Reached(t, c["ok"] == true, "the new recovery key took effect")
}

// covers: MA-1075, R37
// The Unlock box ("Password or recovery key") tells the recovery file's contents pasted there, after
// trying them, in words of its own (LOW-2): a password is never refused for looking like the file,
// so one with braces, quotes and colons still opens its archive. Positive twins first: a wrong
// password is still told as one, in the command line's words.
func TestTheUnlockBoxTellsTheRecoveryFileFromAPassword(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "Enc")
	const pw = `{"my": "pass phrase"}`
	p, err := keyring.Create(out, pw, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rf, err := p.RecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	backUpInto(t, out, u)
	u.Destroy()
	e := newKeysEnv(t, out)
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": "not the password at all"}); m["ok"] != false || m["error"] != wrongSecretText(out) {
		t.Errorf("the positive twin: a wrong password: %v", m)
	}
	_, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": string(rf)})
	if m["ok"] != false || m["error"] != recoveryFileInUnlockBox || strings.Contains(toText(m), "argon2id") || e.cfg.Keys.get(out) != nil {
		t.Errorf("the recovery file's contents in the Unlock box: %v", m)
	}
	_, m = e.post(t, "api/encryption/unlock", map[string]any{"secret": pw})
	assure.Reached(t, m["ok"] == true && e.cfg.Keys.get(out) != nil, "a password that looks like the file still opens its archive")
}

// covers: MA-1075, R37
// Put back the key file: the recovery file's contents in its recovery key box are refused with the
// box to be emptied (LOW-6); a typing mistake is not (the positive twin, first).
func TestTheRecoveryFilesContentsInTheRestoreBoxAreTakenOut(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(out, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rk := p.RecoveryKey()
	rf, err := p.RecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	backUpInto(t, out, u)
	u.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	e := newKeysEnv(t, out)
	const newPw = "the password after the restore"
	if _, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": typo(rk)[:10], "newPassword": newPw, "again": newPw}); m["ok"] != false || m["clear"] != nil {
		t.Errorf("the positive twin: a typing mistake is refused and left to fix: %v", m)
	}
	_, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": string(rf), "newPassword": newPw, "again": newPw})
	_, statErr := os.Stat(filepath.Join(out, vault.FileName))
	assure.Reached(t, m["ok"] == false && m["error"] == recoveryFileInKeyBox && m["clear"] == true && os.IsNotExist(statErr),
		"the recovery file's contents in the key box are refused, nothing written, the box to be emptied")
}
