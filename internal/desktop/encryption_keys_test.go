package desktop

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/vault"
)

// The key changes on the Encryption card (design-encrypted-archives-phase3 §9.1–§9.3,
// K2, K13; the P3-3 plan, 3e; MA-807).

// covers: MA-807, R37
// A new password (§9.1): two that differ, and one too short, are refused before
// anything is asked; a good one is asked about first — the archive is unlocked here,
// so no current password was typed for it (K13) — and changes nothing until the
// person agrees; then the new password opens the archive and the old one no longer
// does.
func TestTheCardChangesThePassword(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	const newPw = "a brand new passphrase for this archive"
	for _, body := range []map[string]any{{"newPassword": newPw, "again": newPw + "x"}, {"newPassword": "short", "again": "short"}} {
		if _, m := e.post(t, "api/encryption/change-password", body); m["ok"] != false || m["confirm"] != nil {
			t.Fatalf("refused before anything is asked: %v", m)
		}
	}
	_, m := e.post(t, "api/encryption/change-password", map[string]any{"newPassword": newPw, "again": newPw})
	if m["confirm"] != "keychange" || !strings.Contains(toText(m), "unlocked here") {
		t.Fatalf("asked first: %v", m)
	}
	if u, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{}); err != nil {
		t.Fatal("nothing changed before the person agreed")
	} else {
		u.Destroy()
	}
	if _, m := e.post(t, "api/encryption/change-password", map[string]any{"newPassword": newPw, "again": newPw, "confirm": true}); m["ok"] != true || strings.Contains(toText(m), newPw) {
		t.Fatalf("changed: %v", m)
	}
	_, oldErr := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{})
	u, newErr := keyring.Open(out, keyring.Password(newPw), keyring.OpenOptions{})
	if newErr == nil {
		u.Destroy()
	}
	assure.Reached(t, oldErr != nil && newErr == nil, "the new password opens it, the old one no longer")
}

// covers: MA-807, R37
// A new recovery key (§9.2, §9.4, K2): asked about first, then shown once; its
// recovery file is saved owner-only in the folder the person picks, never sent to
// the page; typed back wrong it is refused and still waits; typed back right it takes
// effect and opens the archive, the earlier key's warning told. One discarded — by
// Cancel or by a lock of the archive — never takes effect, and its saved file is
// voided.
func TestTheCardMakesANewRecoveryKey(t *testing.T) {
	out := backedUpEncrypted(t)
	saveDir := t.TempDir()
	orig := pickFolder
	pickFolder = func(context.Context, string) (string, error) { return saveDir, nil }
	t.Cleanup(func() { pickFolder = orig })
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})

	_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{})
	if m["confirm"] != "keychange" {
		t.Fatalf("asked first: %v", m)
	}
	_, m = e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	token, key := m["token"].(string), m["recoveryKey"].(string)
	if m["ok"] != true || token == "" || key == "" {
		t.Fatalf("prepared: %v", m)
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": token})
	file, _ := m["path"].(string)
	fi, err := os.Stat(file)
	if m["ok"] != true || err != nil || filepath.Dir(file) != saveDir || strings.Contains(toText(m), key) {
		t.Fatalf("the recovery file: %v %v", m, err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the recovery file is not owner-only: %v", fi.Mode())
	}
	if _, m := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": "AAAAA-AAAAA-AAAAA"}); m["ok"] != false {
		t.Fatalf("typed back wrong: %v", m)
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": key})
	if m["ok"] != true || !strings.Contains(toText(m), "earlier recovery key") {
		t.Fatalf("in effect: %v", m)
	}
	u, err := keyring.Open(out, keyring.RecoveryKey(key), keyring.OpenOptions{})
	if err != nil {
		t.Fatalf("the new recovery key opens the archive: %v", err)
	}
	u.Destroy()

	// Discarded by a lock: never in effect, its file voided.
	_, m = e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	token2, key2 := m["token"].(string), m["recoveryKey"].(string)
	os.Remove(file)
	_, m = e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": token2})
	file2, _ := m["path"].(string)
	e.post(t, "api/encryption/lock", nil)
	_, m = e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token2, "typed": key2})
	_, statErr := os.Stat(file2)
	_, openErr := keyring.Open(out, keyring.RecoveryKey(key2), keyring.OpenOptions{})
	assure.Reached(t, m["ok"] == false && os.IsNotExist(statErr) && openErr != nil, "a lock discards it: not in effect, its file voided")
}

// covers: MA-807, R37
// Put back the key file (§9.4, §9.5, K15): a recorded archive whose key files are
// gone cannot be unlocked, and its card offers to put them back; from its recovery
// file — picked with the system's dialog, never pasted into the page — and its
// recovery key, with a new password, a new recovery key is shown and, typed back,
// the key file is written: the new password then opens the archive. From the key
// this computer remembers, no recovery key is needed. A wrong recovery key writes
// nothing.
func TestTheCardPutsBackTheKeyFile(t *testing.T) {
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
	u.Destroy()
	rfPath := filepath.Join(t.TempDir(), "recovery.txt")
	os.WriteFile(rfPath, rf, 0o600)
	orig := pickFile
	pickFile = func(context.Context, string) (string, error) { return rfPath, nil }
	t.Cleanup(func() { pickFile = orig })
	e := newKeysEnv(t, out)
	w, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{Write: true, Remember: true, Store: e.cfg.Store})
	if err != nil {
		t.Fatal(err)
	}
	backUpInto(t, out, w) // sealed messages beside the key files that go
	w.Destroy()
	// An archive whose key files are whole is not offered a restore, and one asked for
	// anyway writes nothing — the vault refuses it as well.
	if _, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": rk, "newPassword": "the password after the restore", "again": "the password after the restore"}); m["ok"] != false {
		t.Fatalf("a restore over whole key files: %v", m)
	}
	if u, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{}); err != nil {
		t.Fatal("the old password still opens it")
	} else {
		u.Destroy()
	}
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	if page := sessionPage(t, e.h); !strings.Contains(page, `id="encRestoreForm"`) || !strings.Contains(page, `id="encRestoreRememberedBtn"`) {
		t.Fatal("an archive without its key files is offered to put them back")
	}
	const newPw = "the password after the restore"
	if _, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": "AAAAA-BBBBB-CCCCC-DDDDD-EEEEE-FF", "newPassword": newPw, "again": newPw}); m["ok"] != false {
		t.Fatalf("a wrong recovery key: %v", m)
	}
	if _, err := os.Stat(filepath.Join(out, vault.FileName)); err == nil {
		t.Fatal("a wrong recovery key wrote a key file")
	}
	_, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": rk, "newPassword": newPw, "again": newPw})
	token, newKey := m["token"].(string), m["recoveryKey"].(string)
	if m["ok"] != true || token == "" || newKey == "" || newKey == rk {
		t.Fatalf("prepared: %v", m)
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": newKey})
	if m["ok"] != true || !strings.Contains(toText(m), "put back") {
		t.Fatalf("put back: %v", m)
	}
	u, err = keyring.Open(out, keyring.Password(newPw), keyring.OpenOptions{})
	if err != nil {
		t.Fatalf("the new password opens it: %v", err)
	}
	u.Destroy()

	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	_, m = e.post(t, "api/encryption/restore", map[string]any{"from": "remembered", "newPassword": newPw + " again", "again": newPw + " again"})
	if m["ok"] != true {
		t.Fatalf("from the remembered key: %v", m)
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": m["token"], "typed": m["recoveryKey"]})
	u, err = keyring.Open(out, keyring.Password(newPw+" again"), keyring.OpenOptions{})
	if err == nil {
		u.Destroy()
	}
	assure.Reached(t, m["ok"] == true && err == nil, "put back from the remembered key")
}

// covers: MA-807, R37
// Forget this encrypted archive here (§4.6): the row offers it; the plan is shown
// first and nothing changes until it is confirmed; the archive's last folder may
// forget the whole archive, and this computer then no longer records it. A folder
// that may have been tampered with — another archive planted there — needs its path
// typed back: a wrong one changes nothing.
func TestARowForgetsAnEncryptedArchive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	gone := recordedArchive(t, t.TempDir(), "Gone")
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(gone, name))
	}
	e := newKeysEnv(t, gone)
	_, m := e.post(t, "api/encryption/forget/plan", map[string]any{"path": gone})
	if m["ok"] != true || m["strong"] != false || m["last"] != true {
		t.Fatalf("the plan: %v", m)
	}
	rec, _ := encrecord.Load()
	if len(rec.Archives) != 1 {
		t.Fatal("a plan changes nothing")
	}
	var id string
	for k := range rec.Archives {
		id = k
	}
	_, m = e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "whole": true})
	rec, _ = encrecord.Load()
	if _, ok := rec.Lookup(gone); m["ok"] != true || ok {
		t.Fatalf("forgotten: %v", m)
	}
	if e := rec.Archives[id]; e == nil || !e.Forgotten {
		t.Fatal("the whole archive is forgotten: its tombstone stays")
	}

	planted := recordedArchive(t, t.TempDir(), "Planted")
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(planted, name))
	}
	os.WriteFile(filepath.Join(planted, "something-else.txt"), []byte("x"), 0o600)
	_, m = e.post(t, "api/encryption/forget/plan", map[string]any{"path": planted})
	if m["strong"] != true {
		t.Fatalf("a tampered folder asks for its path: %v", m)
	}
	spelling := m["spelling"].(string)
	_, wrong := e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "typed": "/not/the/path"})
	_, m = e.post(t, "api/encryption/forget/plan", map[string]any{"path": planted})
	_, right := e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "typed": spelling})
	rec, _ = encrecord.Load()
	_, stillThere := rec.Lookup(planted)
	assure.Reached(t, wrong["ok"] == false && right["ok"] == true && !stillThere, "only the path typed back forgets a tampered folder")
}

// covers: MA-807, R37
// Forget every encrypted archive on this computer (K8, beside uninstall): offered in
// the Danger zone when there is a record; without the word typed nothing changes;
// with it, the record is emptied and every remembered key forgotten — and the
// archives themselves are untouched: the password still opens each.
func TestTheDangerZoneForgetsEveryEncryptedArchive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := backedUpEncrypted(t)
	recordedArchive(t, t.TempDir(), "Second")
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	e.post(t, "api/encryption/remember", map[string]any{"on": true})
	if !strings.Contains(sessionPage(t, e.h), `id="forgetAllBtn"`) {
		t.Fatal("the Danger zone offers it")
	}
	if _, m := e.post(t, "api/encryption/forget-all", map[string]any{"typed": "yes"}); m["ok"] != false {
		t.Fatalf("without the word: %v", m)
	}
	if rec, _ := encrecord.Load(); len(rec.Archives) != 2 || !keyring.HasRemembered(out, e.cfg.Store) {
		t.Fatal("without the word nothing changes")
	}
	_, m := e.post(t, "api/encryption/forget-all", map[string]any{"typed": "FORGET"})
	rec, _ := encrecord.Load()
	u, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{})
	if err == nil {
		u.Destroy()
	}
	assure.Reached(t, m["ok"] == true && len(rec.Archives) == 0 && !keyring.HasRemembered(out, e.cfg.Store) && err == nil && e.cfg.Keys.get(out) == nil,
		"all forgotten, the archives untouched")
}

// covers: MA-807, R37
// Remember or forget the key (§9.3, O3): remembered from the card, the archive then
// opens by itself; forgotten while it was opened by that key, it locks at once and
// asks for the password again.
func TestTheCardRemembersAndForgetsTheKey(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if _, m := e.post(t, "api/encryption/remember", map[string]any{"on": true}); m["ok"] != true || !keyring.HasRemembered(out, e.cfg.Store) {
		t.Fatalf("remembered: %v", m)
	}
	e.cfg.Keys.lockAll(false)
	if e.cfg.Keys.remembered(out) == nil {
		t.Fatal("a remembered archive opens by itself")
	}
	_, m := e.post(t, "api/encryption/remember", map[string]any{"on": false})
	assure.Reached(t, m["ok"] == true && !keyring.HasRemembered(out, e.cfg.Store) && e.cfg.Keys.get(out) == nil, "forgotten, and locked at once")
}
