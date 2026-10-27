package desktop

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/uninstall"
	"mail-archive-tool/internal/vault"
	"mail-archive-tool/internal/wizard"
)

// The P3-3 follow-up adversarial pass's findings on the Desktop's key lifetimes,
// each kept as the guard it reproduced (docs/review-encrypted-archives-phase3-p33-
// followup.md).

// covers: MA-825, R37
// A reader being made when Shut down locks everything is stopped (LIFE-01): its lease
// is locked with the archive, the build fails at once, Shut down waits for it to give
// up, and no reader is kept. A Lock that reaches the build answers with the locked
// reply, never "could not be opened" (CRIT-01).
func TestAReaderBeingMadeLosesToShutDown(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	loading, finish := make(chan struct{}), make(chan struct{})
	leaseLive := false
	orig := readerSource
	readerSource = func(u *keyring.Unlocked, o keyring.ReaderOptions) (*keyring.Reader, error) {
		close(loading)
		<-finish
		if l, err := u.Lease(); err == nil {
			leaseLive = true
			l.Destroy()
		}
		return orig(u, o)
	}
	defer func() { readerSource = orig }()
	served := make(chan string, 1)
	go func() { _, body := e.read("/api/search?q=hello"); served <- body }()
	<-loading
	locked := make(chan struct{})
	go func() { e.cfg.Keys.lockAll(true); close(locked) }()
	time.Sleep(200 * time.Millisecond)
	close(finish)
	body := <-served
	<-locked
	if leaseLive || strings.Contains(body, "Hello HTML") {
		t.Fatalf("a reader being made outlived Shut down: lease live %v", leaseLive)
	}
	assure.Reached(t, strings.Contains(body, "locked") && !strings.Contains(body, "could not be opened"), "the locked reply, not a failure to open")
}

// The FIFO guard, TestAHungRecoveryFileNeverFreezesTheDesktop, is in
// keys_pass2_unix_test.go: a named pipe is Unix only.

// covers: MA-825, R37
// A refused uninstall never undoes Shut down (LIFE-04): Shut down landing while it
// runs keeps every unlock refused after it.
func TestARefusedUninstallNeverUndoesShutDown(t *testing.T) {
	out := backedUpEncrypted(t)
	life := NewLifecycle(context.Background())
	store := graphconfig.NewFileSecretStore(t.TempDir())
	keys := newArchiveKeys(store, readerLockedNote)
	life.OnDrain(func(context.Context) error { keys.lockAll(true); return nil })
	cfgDir := t.TempDir()
	settings := filepath.Join(cfgDir, "s.json")
	SaveSettings(settings, Settings{Out: out})
	d := newDashboard(Config{ConfigPath: filepath.Join(cfgDir, "config.json"), SettingsPath: settings, Store: store, Keys: keys, Lifecycle: life})
	inRun, release := make(chan struct{}), make(chan struct{})
	origU := uninstallRun
	uninstallRun = func(string, []string, string, bool) (*uninstall.Report, error) {
		close(inRun)
		<-release
		return nil, errors.New("refused: something overlaps")
	}
	t.Cleanup(func() { uninstallRun = origU })
	done := make(chan map[string]any, 1)
	go func() { done <- callAction(t, d, d.uninstallAction, map[string]any{"Confirm": true}) }()
	<-inRun
	life.Stop()
	<-life.Drained()
	close(release)
	<-done
	_, err := keys.unlock(out, keysPassword, keyring.OpenOptions{})
	assure.Reached(t, errors.Is(err, errKeysClosed), "after Shut down every unlock stays refused")
}

// covers: MA-825, R37
// Forget-all covers what is still under way (LIFE-06, CRIT-02): a key file being put
// back while it runs is not kept, so typed back it records nothing; an unlock with
// Remember whose key is derived while it runs leaves no remembered key and records
// nothing.
func TestForgetAllCoversWhatIsUnderWay(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	enc := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(enc, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rk := p.RecoveryKey()
	rf, _ := p.RecoveryFile()
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	id := u.ArchiveID().String()
	backUpInto(t, enc, u)
	u.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(enc, name))
	}
	rfPath := filepath.Join(t.TempDir(), "recovery.txt")
	os.WriteFile(rfPath, rf, 0o600)
	origPick := pickFile
	pickFile = func(context.Context, string) (string, error) { return rfPath, nil }
	t.Cleanup(func() { pickFile = origPick })
	e := newKeysEnv(t, enc)
	turn := memguard.Process.Derivations()
	turn.Lock()
	done := make(chan map[string]any, 1)
	const newPw = "the password after the restore"
	go func() {
		_, m := e.post(t, "api/encryption/restore", map[string]any{"from": "file", "recoveryKey": rk, "newPassword": newPw, "again": newPw})
		done <- m
	}()
	time.Sleep(300 * time.Millisecond)
	e.post(t, "api/encryption/forget-all", map[string]any{"typed": forgetAllWord})
	turn.Unlock()
	m := <-done
	e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": m["token"], "typed": m["recoveryKey"]})
	rec, _ := encrecord.Load()
	if m["ok"] == true || rec.Archives[id] != nil {
		t.Fatalf("a key file put back across forget-all was kept (%v), the archive recorded again %v", m, rec.Archives[id] != nil)
	}

	// An unlock with Remember across forget-all.
	out := backedUpEncrypted(t)
	e2 := newKeysEnv(t, out)
	_, key := keyOf(out)
	turn.Lock()
	unlocked := make(chan map[string]any, 1)
	go func() {
		_, m := e2.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true})
		unlocked <- m
	}()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		e2.cfg.Keys.mu.Lock()
		busy := e2.cfg.Keys.busy[key]
		e2.cfg.Keys.mu.Unlock()
		if busy {
			break
		}
	}
	forgot := make(chan map[string]any, 1)
	go func() {
		_, f := e2.post(t, "api/encryption/forget-all", map[string]any{"typed": forgetAllWord})
		forgot <- f
	}()
	time.Sleep(200 * time.Millisecond)
	turn.Unlock()
	<-unlocked
	f := <-forgot
	rec, _ = encrecord.Load()
	_, body := e2.read("/api/search?q=hello")
	assure.Reached(t, f["ok"] == true && !keyring.HasRemembered(out, e2.cfg.Store) && len(rec.Archives) == 0 && !strings.Contains(body, "Hello HTML"),
		"after forget-all nothing an unlock under way remembered or recorded is left")
}

// covers: MA-825, R37
// An unlock asked for while every remembered key is being forgotten is refused, so
// nothing it would remember outlives the forget (CRIT-02); after, it works.
func TestAnUnlockDuringForgetAllIsRefused(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	release := e.cfg.Keys.hush()
	_, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true})
	release()
	if m["ok"] == true || keyring.HasRemembered(out, e.cfg.Store) {
		t.Fatalf("an unlock during forget-all: %v", m)
	}
	_, m = e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	assure.Reached(t, m["ok"] == true, "positive twin: after the forget an unlock works")
}

// covers: MA-825, R37
// A password change waiting for its turn when the person locks the archive changes
// nothing (FIX-01): the old password still opens it.
func TestAPasswordChangeWaitingForItsTurnLosesToALock(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	turn := memguard.Process.Derivations()
	turn.Lock()
	done := make(chan map[string]any, 1)
	const newPw = "a brand new password here"
	go func() {
		_, m := e.post(t, "api/encryption/change-password", map[string]any{"newPassword": newPw, "again": newPw, "confirm": true})
		done <- m
	}()
	time.Sleep(300 * time.Millisecond)
	e.post(t, "api/encryption/lock", nil)
	turn.Unlock()
	m := <-done
	u, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{})
	if err == nil {
		u.Destroy()
	}
	assure.Reached(t, m["ok"] != true && err == nil, "the change lost to the Lock; the old password still opens the archive")
}

// covers: MA-825, R37
// A new archive whose first backup cannot start — another task is running — still has
// the run job Run now repeats, written as it was made, so Run now finishes setting it
// up (MAKE-01); and a second password on a create that has its key is refused, so the
// recovery key typed back is always the key committed (MAKE-03, LIFE-05).
func TestANewArchiveKeepsItsRunJobEvenIfItsFirstBackupCannotStart(t *testing.T) {
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
	d := newDashboard(e.cfg)
	m := callAction(t, d, d.createStartAction, map[string]any{})
	token := m["token"]
	const pw = "the new archive's own passphrase"
	k := callAction(t, d, d.createKeyAction, map[string]any{"token": token, "newPassword": pw, "again": pw})
	const pw2 = "another passphrase entirely"
	if k2 := callAction(t, d, d.createKeyAction, map[string]any{"token": token, "newPassword": pw2, "again": pw2}); k2["ok"] == true {
		t.Fatalf("a second password replaced the key already made: %v", k2)
	}
	hold := make(chan struct{})
	if err := d.runJob(func(*log.Logger) (string, error) { <-hold; return "", nil }); err != nil {
		t.Fatal(err)
	}
	c := callAction(t, d, d.confirmRecoveryAction, map[string]any{"token": token, "typed": k["recoveryKey"]})
	close(hold)
	if c["ok"] != true || !strings.Contains(strings.Join(toLines(c["lines"]), " "), "could not start") {
		t.Fatalf("made while another task ran: %v", c)
	}
	assure.Reached(t, schedule.BuildKindEvidence(out).Has(schedule.KindRunJob), "the run job Run now repeats was written")
}

// covers: MA-825, R37
// Dismiss clears only the warning of the archive open here (DRIVE-01): locked, it is
// refused; with both key-file copies replaced by another lost archive's, or one copy
// alone left, the other archive's mark is never cleared; the archive's own is.
func TestDismissClearsOnlyTheOpenArchivesWarning(t *testing.T) {
	z := backedUpEncrypted(t)
	x := backedUpEncrypted(t)
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	xID, zID := vault.Claims(x).IDs[0].String(), vault.Claims(z).IDs[0].String()
	encrecord.MarkLost(xID)
	encrecord.MarkLost(zID)
	e := newKeysEnv(t, x)
	_, m := e.post(t, "api/encryption/dismiss-lost", nil)
	if m["ok"] == true || !strings.Contains(str(m["error"]), "unlock") {
		t.Fatalf("locked: %v", m)
	}
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	keep := map[string][]byte{}
	for _, n := range []string{vault.FileName, vault.BackupName} {
		keep[n], _ = os.ReadFile(filepath.Join(x, n))
		b, _ := os.ReadFile(filepath.Join(z, n))
		os.WriteFile(filepath.Join(x, n), b, 0o600) // both copies now name Z
	}
	// The folder now claims Z, but the archive open here is X: X's own mark is the one
	// cleared, never Z's.
	if _, m = e.post(t, "api/encryption/dismiss-lost", nil); m["ok"] != true {
		t.Fatalf("dismiss: %v", m)
	}
	for n, b := range keep {
		os.WriteFile(filepath.Join(x, n), b, 0o600)
	}
	lost, _ := encrecord.Lost()
	assure.Reached(t, len(lost) == 1 && lost[0] == zID, "only the open archive's mark was cleared")
}
