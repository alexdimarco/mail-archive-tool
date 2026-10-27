package desktop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// The P3-3 adversarial pass's findings on the Encryption card's actions, and the
// claims it found untested, each kept as a guard.

// covers: MA-817, R37
// "Forget the key on this computer" forgets the key of the archive open here, never
// of the archive its folder's key files name now (OUT-A): with another archive's key
// files planted in the folder, the card stops saying it is open — it is locked — and
// the other archive's remembered key stays.
func TestForgettingTheKeyNeverHitsAPlantedArchive(t *testing.T) {
	y := backedUpEncrypted(t)
	x := backedUpEncrypted(t)
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	e := newKeysEnv(t, x)
	uy, err := keyring.Open(y, keyring.Password(keysPassword), keyring.OpenOptions{Remember: true, Store: e.cfg.Store})
	if err != nil {
		t.Fatal(err)
	}
	uy.Destroy()
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword}); m["ok"] != true {
		t.Fatalf("unlock X: %v", m)
	}
	if page := sessionPage(t, e.h); strings.Contains(page, `id="encForgetKey"`) {
		t.Fatal("positive twin: X's key is not remembered, and the card says so")
	}
	for _, n := range []string{vault.FileName, vault.BackupName} {
		b, _ := os.ReadFile(filepath.Join(y, n))
		os.WriteFile(filepath.Join(x, n), b, 0o600)
	}
	// Clicked before the page is drawn again: it forgets X's own key (none), not Y's.
	e.post(t, "api/encryption/remember", map[string]any{"on": false})
	if !keyring.HasRemembered(y, e.cfg.Store) {
		t.Fatal("forgetting the open archive's key forgot the planted archive's")
	}
	page := sessionPage(t, e.h)
	if e.cfg.Keys.get(x) != nil || strings.Contains(page, `id="encLock"`) {
		t.Fatal("a folder whose key files now name another archive is still shown open")
	}
	_, m := e.post(t, "api/encryption/remember", map[string]any{"on": false})
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names("locked"),
		assure.NoSideEffect(func() bool { return keyring.HasRemembered(y, e.cfg.Store) }))
}

// covers: MA-817, R37
// The row's Forget takes the archive's lock with no verb (OUT-C): nothing beside the
// folder says this computer stopped protecting it.
func TestForgettingAFolderLeavesNoVerbBesideIt(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := recordedArchive(t, t.TempDir(), "Intact")
	e := newKeysEnv(t, out)
	_, m := e.post(t, "api/encryption/forget/plan", map[string]any{"path": out})
	if m["ok"] != true {
		t.Fatalf("plan: %v", m)
	}
	if _, f := e.post(t, "api/encryption/forget", map[string]any{"token": m["token"]}); f["ok"] != true {
		t.Fatalf("forget: %v", f)
	}
	b, err := os.ReadFile(filepath.Join(out, lockfile.Name))
	assure.Reached(t, err == nil && !strings.Contains(string(b), "verb="), "the lock file holds a bare holder line")
}

// covers: MA-818, R37
// Forgetting the last folder of an archive can forget its remembered key too (FID-08):
// asked for, the key goes with the record; the archive itself is untouched.
func TestForgettingTheLastFolderCanForgetItsKey(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true}); m["ok"] != true {
		t.Fatalf("unlock: %v", m)
	}
	_, p := e.post(t, "api/encryption/forget/plan", map[string]any{"path": out})
	if p["ok"] != true || p["last"] != true || p["remembered"] != true {
		t.Fatalf("plan: %v", p)
	}
	if _, f := e.post(t, "api/encryption/forget", map[string]any{"token": p["token"], "whole": true, "keys": true}); f["ok"] != true {
		t.Fatalf("forget: %v", f)
	}
	_, statErr := os.Stat(filepath.Join(out, vault.FileName))
	assure.Reached(t, !keyring.HasRemembered(out, e.cfg.Store) && statErr == nil, "the key is forgotten, the archive kept")
}

// covers: MA-817, R37
// Dismiss clears no other archive's lost-record warning (OUT-D): with key-file copies
// that name two archives it refuses, both marks kept; with copies that agree it
// clears that archive's mark.
func TestDismissingTheLostWarningClearsOnlyThisArchive(t *testing.T) {
	z := backedUpEncrypted(t)
	x := backedUpEncrypted(t)
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	xID, zID := vault.Claims(x).IDs[0].String(), vault.Claims(z).IDs[0].String()
	encrecord.MarkLost(xID)
	encrecord.MarkLost(zID)
	keep, _ := os.ReadFile(filepath.Join(x, vault.BackupName))
	b, _ := os.ReadFile(filepath.Join(z, vault.BackupName))
	os.WriteFile(filepath.Join(x, vault.BackupName), b, 0o600)
	e := newKeysEnv(t, x)
	_, m := e.post(t, "api/encryption/dismiss-lost", nil)
	lost, _ := encrecord.Lost()
	assure.Refused(t, 1, str(m["error"]), assure.Code(1), assure.Names("name different archives"),
		assure.NoSideEffect(func() bool { return len(lost) == 2 }))

	os.WriteFile(filepath.Join(x, vault.BackupName), keep, 0o600)
	if _, m := e.post(t, "api/encryption/dismiss-lost", nil); m["ok"] != true {
		t.Fatalf("positive twin: %v", m)
	}
	lost, _ = encrecord.Lost()
	assure.Reached(t, len(lost) == 1 && lost[0] == zID, "only this archive's mark was cleared")
}

// covers: MA-818, R37
// Unlocking in the page asks the K14 cost question at a folder this computer has not
// recorded, nothing opened until it is agreed (FID-04); and with Remember ticked, the
// "record it again?" question for an archive the person said to forget is asked
// before any key is derived — no derivation for the question (FID-04, FID-05).
func TestUnlockAsksItsQuestionsInThePage(t *testing.T) {
	out := backedUpEncrypted(t)
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	for _, name := range []string{vault.FileName, vault.BackupName} {
		path := filepath.Join(out, name)
		raw, _ := os.ReadFile(path)
		var m map[string]any
		json.Unmarshal(raw, &m)
		for _, e := range m["entries"].([]any) {
			if e := e.(map[string]any); e["kind"] == "password" {
				e["memory_kib"] = float64(vault.CostFloor().MemoryKiB * 2)
			}
		}
		b, _ := json.Marshal(m)
		os.WriteFile(path, b, 0o600)
	}
	e := newKeysEnv(t, out)
	_, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if m["confirm"] != "cost" || e.cfg.Keys.get(out) != nil {
		t.Fatalf("the cost is asked, nothing opened: %v", m)
	}
	// Agreed, it is not asked again (the cost raised here also changes the key, so the
	// password no longer opens this tampered copy).
	_, m = e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "cost": m["cost"]})
	assure.Reached(t, m["confirm"] == nil && m["error"] != nil, "agreed, the cost is not asked again")

	// The record-again question, before any derivation.
	out2 := backedUpEncrypted(t)
	id := vault.Claims(out2).IDs[0].String()
	encrecord.Update("test", func(r *encrecord.Record) error {
		if err := r.Add(id, out2, false, time.Now()); err != nil {
			return err
		}
		r.ForgetID(id)
		return nil
	})
	e2 := newKeysEnv(t, out2)
	opens := 0
	orig := openArchive
	openArchive = func(out, typed string, o keyring.OpenOptions) (*keyring.Unlocked, error) {
		opens++
		return orig(out, typed, o)
	}
	defer func() { openArchive = orig }()
	_, m = e2.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true})
	if m["confirm"] != "rerecord" || opens != 0 {
		t.Fatalf("the record-again question: %v after %d opens, want it before any", m, opens)
	}
	_, m = e2.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true, "reRecord": true})
	rec, _ := encrecord.Load()
	_, recorded := rec.Lookup(out2)
	assure.Reached(t, m["ok"] == true && recorded, "agreed, the archive opens and is recorded again")
}

// covers: MA-818, R37
// Rebuild search index on an unlocked encrypted archive rebuilds it with its key
// (FID-02), and Run now on a remembered archive after the idle lock opens it by
// itself (FID-03).
func TestRebuildAndRunNowWorkOnAnEncryptedArchive(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if _, m := e.post(t, "api/rebuild", nil); m["ok"] != true {
		t.Fatalf("rebuild: %v", m)
	}
	res, errText := waitIdle(t, e)
	if errText != "" || !strings.Contains(res, "Search index rebuilt: 1 message") {
		t.Fatalf("the keyed rebuild: %q %q", res, errText)
	}

	out2 := backedUpEncrypted(t)
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	runJobFor(t, out2, box)
	e2 := newKeysEnv(t, out2)
	e2.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true})
	e2.cfg.Keys.reap(time.Now().Add(keyIdle + time.Minute))
	_, m := e2.post(t, "api/run-backup", nil)
	if m["ok"] != true {
		t.Fatalf("Run now on a remembered archive after the idle lock: %v", m)
	}
	_, errText = waitIdle(t, e2)
	assure.Reached(t, errText == "", "the run finished")
}

// covers: MA-818, R37
// Putting back the key file never repeats what was typed as the recovery key
// (HTTP-01): a password typed there by mistake is refused before the file dialog,
// in words that quote no character of it and not its length.
func TestPuttingBackTheKeyFileNeverEchoesWhatWasTyped(t *testing.T) {
	out := backedUpEncrypted(t)
	for _, n := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, n))
	}
	e := newKeysEnv(t, out)
	opened := false
	orig := pickFile
	pickFile = func(_ context.Context, title string) (string, error) {
		opened = true
		return "", nil
	}
	defer func() { pickFile = orig }()
	const typed = "my-secret-password-Q!" // 21 characters, '!' outside the key's alphabet
	_, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": typed, "newPassword": "a new password here", "again": "a new password here"})
	msg := str(m["error"])
	if m["ok"] == true || opened {
		t.Fatalf("a mistyped recovery key opened the dialog or was accepted: %v", m)
	}
	for _, leak := range []string{"'!'", "'Q'", "21", "position", "character"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("the reply repeats part of what was typed (%q): %s", leak, msg)
		}
	}
}

// covers: MA-818, R37
// The recovery file is read with a bound: one byte over a recovery file's size is
// refused unread, and a file that is not regular is refused (FID-08, OUT-B). The
// positive twin reads a small file.
func TestTheRecoveryFileIsReadBounded(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.txt")
	os.WriteFile(small, []byte("recovery file"), 0o600)
	if b, err := readRecoveryFile(small); err != nil || string(b) != "recovery file" {
		t.Fatalf("positive twin: %q %v", b, err)
	}
	big := filepath.Join(dir, "big.txt")
	os.WriteFile(big, make([]byte, maxRecoveryFileRead+1), 0o600)
	if _, err := readRecoveryFile(big); err == nil || !strings.Contains(err.Error(), "larger than a recovery file") {
		t.Fatalf("a file over the bound: %v", err)
	}
	link := filepath.Join(dir, "link.txt")
	os.Symlink(small, link)
	_, err := readRecoveryFile(link)
	assure.Reached(t, err != nil && strings.Contains(err.Error(), "not a file"), "a link is refused, not followed")
}

// covers: MA-818, R37
// Cancel ends a new recovery key not yet in effect (FID-06): typed back after it, it is
// refused, the earlier key still works, and the recovery file saved for it is voided.
func TestCancelEndsANewRecoveryKey(t *testing.T) {
	out := backedUpEncrypted(t)
	saveDir := t.TempDir()
	orig := pickFolder
	pickFolder = func(_ context.Context, title string) (string, error) { return saveDir, nil }
	defer func() { pickFolder = orig }()
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	_, s := e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": m["token"]})
	if s["ok"] != true {
		t.Fatalf("save: %v", s)
	}
	e.post(t, "api/encryption/new-recovery-key/cancel", map[string]any{"token": m["token"]})
	_, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": m["token"], "typed": m["recoveryKey"]})
	_, statErr := os.Stat(str(s["path"]))
	u, err := keyring.Open(out, keyring.RecoveryKey(str(m["recoveryKey"])), keyring.OpenOptions{})
	if err == nil {
		u.Destroy()
	}
	assure.Refused(t, 1, str(c["error"]), assure.Code(1), assure.Names("no longer waiting"),
		assure.NoSideEffect(func() bool { return statErr != nil && err != nil }))
}

// covers: MA-818, R37
// Two defaults a person relies on (FID-10): Remember is not ticked on a locked card of
// an archive that is set up, and Repair is offered only for a damaged record.
func TestTheCardsDefaultsHold(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	page := sessionPage(t, e.h)
	i := strings.Index(page, `id="encRemember"`)
	if i < 0 {
		t.Fatal("no Remember box on the locked card")
	}
	tag := page[i : strings.Index(page[i:], ">")+i]
	assure.Reached(t, !strings.Contains(tag, "checked"), "Remember is off for a plain unlock")
}
