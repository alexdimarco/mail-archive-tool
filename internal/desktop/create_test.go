package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/vault"
	"mail-archive-tool/internal/wizard"
)

// Making an encrypted archive from MailArchive Desktop (design-encrypted-archives-
// phase3 §5.1, §5.2, D1, K2, O5; the P3-3 plan, 3f; MA-808).

// covers: MA-808, R37
// A create cancelled after its key was made, or one whose archive locks or whose
// Desktop shuts down, writes nothing: no key file, nothing recorded (§5.1 step 6).
func TestACancelledCreateWritesNothing(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "New")
	orig := addEncryptedFn
	t.Cleanup(func() { addEncryptedFn = orig })
	addEncryptedFn = func(check func(string) error) (*wizard.PendingAdd, error) {
		return wizard.PendingAddFor(out, nil), check(out)
	}
	e := newKeysEnv(t, archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"}))
	_, m := e.post(t, "api/encryption/create", map[string]any{})
	token := m["token"]
	const pw = "the new archive's own passphrase"
	e.post(t, "api/encryption/create/key", map[string]any{"token": token, "newPassword": pw, "again": pw})
	e.post(t, "api/encryption/new-recovery-key/cancel", map[string]any{"token": token})
	_, statErr := os.Stat(filepath.Join(out, vault.FileName))
	rec, _ := encrecord.Load()
	assure.Reached(t, os.IsNotExist(statErr) && len(rec.Archives) == 0, "cancelled: nothing written")
}

// covers: MA-816, R37
// A new archive's key made while its create was cancelled is never kept (the P3-3
// pass's fix round): the key step answers that it is no longer waiting, and no
// recovery key is shown for it.
func TestACreateCancelledWhileItsKeyIsMadeKeepsNoKey(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "New")
	orig := addEncryptedFn
	t.Cleanup(func() { addEncryptedFn = orig })
	addEncryptedFn = func(check func(string) error) (*wizard.PendingAdd, error) {
		return wizard.PendingAddFor(out, nil), check(out)
	}
	e := newKeysEnv(t, archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"}))
	_, m := e.post(t, "api/encryption/create", map[string]any{})
	token := m["token"]
	turn := memguard.Process.Derivations()
	turn.Lock()
	done := make(chan map[string]any, 1)
	go func() {
		const pw = "the new archive's own passphrase"
		_, k := e.post(t, "api/encryption/create/key", map[string]any{"token": token, "newPassword": pw, "again": pw})
		done <- k
	}()
	time.Sleep(300 * time.Millisecond) // the key waits for the derivation turn
	e.post(t, "api/encryption/new-recovery-key/cancel", map[string]any{"token": token})
	turn.Unlock()
	k := <-done
	assure.Reached(t, k["ok"] != true && k["recoveryKey"] == nil, "no key kept for a cancelled create")
}

// covers: MA-808, R37
// The folder the wizard's questions choose is judged at once — one that is not empty
// is refused before any password; then the password is typed in the page (two that
// differ refused), the recovery key shown once and, typed back wrong, nothing written;
// typed back right, the key file is written, the folder recorded and listed (not made
// current), the key remembered (on by default), the run job Run now repeats written
// with no schedule (O5), and the first backup runs — after it the archive is set up,
// its key file opened by the password, and nothing in it readable without the key.
func TestDesktopMakesAnEncryptedArchive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	box := filepath.Join(t.TempDir(), "Inbox")
	os.WriteFile(box, []byte(keysMbox), 0o600)
	out := filepath.Join(t.TempDir(), "New")
	busy := t.TempDir()
	os.WriteFile(filepath.Join(busy, "something.txt"), []byte("x"), 0o600)
	orig := addEncryptedFn
	t.Cleanup(func() { addEncryptedFn = orig })
	var folder string
	addEncryptedFn = func(check func(string) error) (*wizard.PendingAdd, error) {
		if err := check(folder); err != nil {
			return nil, err
		}
		return wizard.PendingAddFor(folder, []string{box}), nil
	}
	plain := archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	e := newKeysEnv(t, plain)

	folder = busy
	if _, m := e.post(t, "api/encryption/create", map[string]any{}); m["ok"] != false {
		t.Fatalf("a folder that is not empty: %v", m)
	}
	folder = out
	_, m := e.post(t, "api/encryption/create", map[string]any{})
	token, _ := m["token"].(string)
	if m["ok"] != true || token == "" || m["path"] != out {
		t.Fatalf("start: %v", m)
	}
	const pw = "the new archive's own passphrase"
	if _, m := e.post(t, "api/encryption/create/key", map[string]any{"token": token, "newPassword": pw, "again": pw + "x"}); m["ok"] != false {
		t.Fatalf("two passwords that differ: %v", m)
	}
	_, m = e.post(t, "api/encryption/create/key", map[string]any{"token": token, "newPassword": pw, "again": pw, "remember": true})
	key, _ := m["recoveryKey"].(string)
	if m["ok"] != true || key == "" || strings.Contains(toText(m), pw) {
		t.Fatalf("the key made: %v", m)
	}
	if _, m := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": "AAAAA-BBBBB"}); m["ok"] != false {
		t.Fatalf("typed back wrong: %v", m)
	}
	if _, err := os.Stat(filepath.Join(out, vault.FileName)); err == nil {
		t.Fatal("a key file was written before the recovery key was typed back")
	}
	_, m = e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": key})
	if m["ok"] != true || m["running"] != true {
		t.Fatalf("made: %v", m)
	}
	if _, errText := waitIdle(t, e); errText != "" {
		t.Fatalf("the first backup: %s", errText)
	}
	rec, _ := encrecord.Load()
	_, recorded := rec.Lookup(out)
	jobPath, _ := job.PathFor(schedule.DefaultNameFor(out))
	_, jobErr := os.Stat(jobPath)
	page := sessionPage(t, e.h)
	u, openErr := keyring.Open(out, keyring.Password(pw), keyring.OpenOptions{})
	if openErr == nil {
		u.Destroy()
	}
	if !recorded || jobErr != nil || encmark.NotSetUp(out) != "" || openErr != nil || !keyring.HasRemembered(out, e.cfg.Store) ||
		!strings.Contains(page, out) || e.cfg.effectiveOut() != plain {
		t.Fatalf("recorded %v, job %v, not set up %q, open %v, page lists it %v", recorded, jobErr, encmark.NotSetUp(out), openErr, strings.Contains(page, out))
	}
	var plainFiles []string
	filepath.Walk(out, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && (strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".log") || strings.HasSuffix(p, ".eml") || strings.HasSuffix(p, ".db")) {
			plainFiles = append(plainFiles, p)
		}
		return nil
	})
	assure.Reached(t, len(plainFiles) == 0, "nothing readable without the key")
}
