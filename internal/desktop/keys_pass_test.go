package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/vault"
)

// The P3-3 adversarial pass's key-lifetime findings, each kept as the guard it
// reproduced (docs/review-encrypted-archives-phase3-p33-adversarial.md).

// remembering unlocks e's archive with Remember on, then lets the idle lock close it,
// so a remembered key may re-open it by itself.
func remembering(t *testing.T, e *keysEnv, out string) {
	t.Helper()
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true}); m["ok"] != true {
		t.Fatalf("unlock: %v", m)
	}
	e.cfg.Keys.reap(time.Now().Add(keyIdle + time.Minute))
	if e.cfg.Keys.get(out) != nil {
		t.Fatal("fixture: the idle lock")
	}
}

// pausedLookup makes the next remembered-key lookup wait until the returned func is
// called, after it has begun.
func pausedLookup(t *testing.T) (begun <-chan struct{}, release func()) {
	b, r := make(chan struct{}), make(chan struct{})
	orig := hasRemembered
	var once sync.Once
	hasRemembered = func(o string, s graphconfig.SecretStore) bool {
		once.Do(func() { close(b); <-r })
		return orig(o, s)
	}
	t.Cleanup(func() { hasRemembered = orig })
	var ro sync.Once
	return b, func() { ro.Do(func() { close(r) }) }
}

// covers: MA-816, R37
// A remembered key's silent re-open never undoes the person's Lock (KEY-02): a reader
// request that was looking the key up when Lock was clicked is refused, the archive
// stays locked, and later requests stay locked too.
func TestARememberedReopenNeverUndoesALock(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	remembering(t, e, out)
	begun, release := pausedLookup(t)
	done := make(chan string, 1)
	go func() { _, body := e.read("/api/search?q=hello"); done <- body }()
	<-begun
	if _, m := e.post(t, "api/encryption/lock", nil); m["ok"] != true {
		t.Fatalf("lock: %v", m)
	}
	release()
	body := <-done
	_, later := e.read("/api/search?q=hello")
	if e.cfg.Keys.get(out) != nil || strings.Contains(body, "Hello HTML") || strings.Contains(later, "Hello HTML") {
		t.Fatalf("the person's Lock was undone: open=%v racing=%v later=%v", e.cfg.Keys.get(out) != nil, strings.Contains(body, "Hello HTML"), strings.Contains(later, "Hello HTML"))
	}
	// The positive twin: Unlock with the remembered key opens it again, no password.
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"remembered": true}); m["ok"] != true || e.cfg.Keys.get(out) == nil {
		t.Fatalf("Unlock with the key this computer remembers: %v", m)
	}
}

// covers: MA-816, R37
// A switch away from an archive whose remembered key is being looked up leaves it
// locked (KEY-02); switched back to, it re-opens by itself, though the person had
// locked it before (O3: a switch; FID-03).
func TestASwitchWinsOverARememberedReopen(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	remembering(t, e, out)
	begun, release := pausedLookup(t)
	done := make(chan struct{})
	go func() { e.read("/api/search?q=hello"); close(done) }()
	<-begun
	other := archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	if _, s := e.post(t, "api/settings", map[string]any{"out": other}); s["ok"] != true {
		t.Fatalf("switch: %v", s)
	}
	release()
	<-done
	if e.cfg.Keys.get(out) != nil {
		t.Fatal("the archive switched away from re-opened after the switch")
	}

	// The person's Lock of the archive while it is current, then a switch away and back.
	if _, s := e.post(t, "api/settings", map[string]any{"out": out}); s["ok"] != true {
		t.Fatalf("switch back: %v", s)
	}
	e.post(t, "api/encryption/lock", nil)
	if _, body := e.read("/api/search?q=hello"); strings.Contains(body, "Hello HTML") {
		t.Fatal("the person's Lock did not hold while the archive stayed current")
	}
	e.post(t, "api/settings", map[string]any{"out": other})
	if _, s := e.post(t, "api/settings", map[string]any{"out": out}); s["ok"] != true {
		t.Fatalf("switch back again: %v", s)
	}
	if _, body := e.read("/api/search?q=hello"); !strings.Contains(body, "Hello HTML") {
		t.Fatal("switched back to, a remembered archive did not re-open by itself")
	}
}

// covers: MA-816, R37
// A new recovery key being made when the person locks the archive is never kept
// (KEY-03): the prepare answers that it locked, and nothing typed back puts it in
// effect. The same at Shut down: nothing is kept after the drain.
func TestAKeyChangeBeingMadeLosesToALockAndShutDown(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	turn := memguard.Process.Derivations()
	turn.Lock()
	done := make(chan map[string]any, 1)
	go func() {
		_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
		done <- m
	}()
	time.Sleep(300 * time.Millisecond) // the handler waits for the derivation turn
	if _, m := e.post(t, "api/encryption/lock", nil); m["ok"] != true {
		t.Fatalf("lock: %v", m)
	}
	turn.Unlock()
	m := <-done
	token, _ := m["token"].(string)
	key, _ := m["recoveryKey"].(string)
	_, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": key})
	if key != "" {
		if u, err := keyring.Open(out, keyring.RecoveryKey(key), keyring.OpenOptions{}); err == nil {
			u.Destroy()
			t.Fatal("a recovery key made across the person's Lock opens the archive")
		}
	}
	if m["ok"] == true || c["ok"] == true || !strings.Contains(str(m["error"]), "locked") {
		t.Fatalf("a key change made across the person's Lock outlived it: prepare %v, confirm %v", m, c)
	}

	// Shut down.
	life := NewLifecycle(context.Background())
	store := graphconfig.NewFileSecretStore(t.TempDir())
	keys := newArchiveKeys(store, readerLockedNote)
	life.OnDrain(func(context.Context) error { keys.lockAll(true); return nil })
	settings := filepath.Join(t.TempDir(), "s.json")
	SaveSettings(settings, Settings{Out: out})
	d := newDashboard(Config{SettingsPath: settings, Store: store, Keys: keys, Lifecycle: life})
	if _, err := keys.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	turn.Lock()
	sd := make(chan map[string]any, 1)
	go func() { sd <- callAction(t, d, d.newRecoveryAction, map[string]any{"confirm": true}) }()
	time.Sleep(300 * time.Millisecond)
	life.Stop()
	<-life.Drained()
	turn.Unlock()
	m = <-sd
	tok, _ := m["token"].(string)
	assure.Reached(t, m["ok"] != true && d.pending.get(tok, anyItem) == nil, "nothing prepared is kept after Shut down's drain")
}

// str is a reply field as text.
func str(v any) string { s, _ := v.(string); return s }

// callAction calls a dashboard handler directly through its guard.
func callAction(t *testing.T, d *dashboard, h func(http.ResponseWriter, *http.Request), body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(string(b)))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", d.csrf)
	rr := httptest.NewRecorder()
	d.api(h)(rr, req)
	var m map[string]any
	json.Unmarshal(rr.Body.Bytes(), &m)
	return m
}

// covers: MA-816, R37
// A Lock never waits on a reader being made (KEY-06): it returns at once, the open's
// key goes with it — no lease is taken from it afterwards — and the reader made
// meanwhile is not kept.
func TestALockNeverWaitsOnAReaderLoad(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	loading, finish := make(chan struct{}), make(chan struct{})
	orig := readerSource
	readerSource = func(u *keyring.Unlocked, o keyring.ReaderOptions) (*keyring.Reader, error) {
		close(loading)
		<-finish
		return orig(u, o)
	}
	defer func() { readerSource = orig }()
	served := make(chan string, 1)
	go func() { _, body := e.read("/api/search?q=hello"); served <- body }()
	<-loading
	ent := e.cfg.Keys.get(out)
	locked := make(chan struct{})
	go func() { e.cfg.Keys.lock(out); close(locked) }()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		close(finish)
		t.Fatal("Lock waited on a reader's load")
	}
	if l, err := ent.u.Lease(); err == nil {
		l.Destroy()
		close(finish)
		t.Fatal("the open's key still gave a lease after Lock")
	}
	close(finish)
	if body := <-served; strings.Contains(body, "Hello HTML") {
		t.Fatal("a reader made across the Lock served mail")
	}
}

// covers: MA-816, R37
// A recovery file is never saved for a new recovery key discarded while the folder
// dialog was open (KEY-07): the save answers that it is no longer waiting, and no
// file is written. The positive twin saves one while the key waits.
func TestNoRecoveryFileOutlivesItsDiscardedKey(t *testing.T) {
	out := backedUpEncrypted(t)
	saveDir := t.TempDir()
	picking, picked := make(chan struct{}), make(chan struct{})
	var gate sync.Once
	orig := pickFolder
	pickFolder = func(context.Context, string) (string, error) {
		gate.Do(func() { close(picking); <-picked })
		return saveDir, nil
	}
	t.Cleanup(func() { pickFolder = orig })
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	token := m["token"].(string)
	done := make(chan map[string]any, 1)
	go func() {
		_, s := e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": token})
		done <- s
	}()
	<-picking
	e.post(t, "api/encryption/lock", nil)
	close(picked)
	s := <-done
	entries, _ := os.ReadDir(saveDir)
	assure.Refused(t, 1, str(s["error"]), assure.Code(1), assure.Names("no longer waiting"),
		assure.NoSideEffect(func() bool { return s["ok"] != true && len(entries) == 0 }))

	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, m = e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	_, s = e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": m["token"]})
	assure.Reached(t, s["ok"] == true && strings.HasPrefix(str(s["path"]), saveDir) && filepath.Base(str(s["path"])) == recoveryFileName(out),
		"positive twin: a waiting key's recovery file is saved, named for its own archive (HTTP-04)")
	e.post(t, "api/encryption/new-recovery-key/cancel", map[string]any{"token": m["token"]})
}

// covers: MA-816, R37
// A key file prepared to be put back for one archive never outlives a switch to
// another (KEY-05, HTTP-04): typed back after it, nothing is written.
func TestAPreparedRestoreDoesNotOutliveASwitch(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(out, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	e := newKeysEnv(t, out)
	w, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{Write: true, Remember: true, Store: e.cfg.Store})
	if err != nil {
		t.Fatal(err)
	}
	backUpInto(t, out, w)
	w.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(out, name))
	}
	const newPw = "the password after the restore"
	_, m := e.post(t, "api/encryption/restore", map[string]any{"from": "remembered", "newPassword": newPw, "again": newPw})
	if m["ok"] != true {
		t.Fatalf("prepared: %v", m)
	}
	other := archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	if _, s := e.post(t, "api/settings", map[string]any{"out": other}); s["ok"] != true {
		t.Fatalf("switch: %v", s)
	}
	_, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": m["token"], "typed": m["recoveryKey"]})
	_, statErr := os.Stat(filepath.Join(out, vault.FileName))
	assure.Refused(t, 1, str(c["error"]), assure.Code(1), assure.Names("no longer waiting"),
		assure.NoSideEffect(func() bool { return c["ok"] != true && statErr != nil }))
}

// covers: MA-816, R37
// A confirm whose commit fails discards what it prepared (KEY-04): the reply says the
// earlier recovery key still works, and the prepared key can never be committed later.
func TestAFailedConfirmDiscardsItsKey(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	d := newDashboard(e.cfg)
	ent := e.cfg.Keys.get(out)
	lease, err := ent.u.LeaseForWrite(keyring.OpenOptions{Store: e.cfg.Store})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := keyring.NewRecoveryKey(lease)
	if err != nil {
		t.Fatal(err)
	}
	abs, key := keyOf(out)
	token := d.pending.put(&pendingItem{key: key, out: abs, rec: rec, lease: lease})
	l, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	c := callAction(t, d, d.confirmRecoveryAction, map[string]any{"token": token, "typed": rec.RecoveryKey()})
	l.Release()
	if c["ok"] == true || !strings.Contains(str(c["error"]), "earlier one still works") {
		t.Fatalf("a confirm while the archive was busy: %v", c)
	}
	assure.Reached(t, rec.Commit() != nil, "the prepared key can never be committed after the failed confirm")
}

// covers: MA-816, R37
// Forget-all keeps every archive closed for the whole forget (CRIT-02): a reader
// request during it never re-opens one from the key being forgotten; and it ends every
// key change prepared here, so a key file put back afterwards cannot record a
// forgotten archive again unasked (CRIT-01).
func TestForgetAllKeepsEveryArchiveClosed(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true}); m["ok"] != true {
		t.Fatalf("unlock: %v", m)
	}
	_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
	token := m["token"]
	begun, release := pausedLookup(t)
	done := make(chan string, 1)
	go func() {
		<-begun
		release()
	}()
	if _, f := e.post(t, "api/encryption/forget-all", map[string]any{"typed": forgetAllWord}); f["ok"] != true {
		t.Fatalf("forget-all: %v", f)
	}
	go func() { _, body := e.read("/api/search?q=hello"); done <- body }()
	release()
	if body := <-done; strings.Contains(body, "Hello HTML") || e.cfg.Keys.get(out) != nil {
		t.Fatal("an archive re-opened after forget-all from a key it forgot")
	}
	_, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": m["recoveryKey"]})
	assure.Reached(t, c["ok"] != true, "a key change prepared before forget-all is not committed after it")
}

// covers: MA-816, R37
// While every remembered key is being forgotten no archive re-opens by itself
// (CRIT-02); after, it does again. And a key file prepared to be put back before
// forget-all is not written after it — which would record the forgotten archive
// again unasked (CRIT-01).
func TestAHushHoldsEveryArchiveClosed(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	remembering(t, e, out)
	release := e.cfg.Keys.hush()
	if e.cfg.Keys.remembered(out) != nil {
		release()
		t.Fatal("a remembered key re-opened an archive during a hush")
	}
	release()
	if e.cfg.Keys.remembered(out) == nil {
		t.Fatal("positive twin: after the hush a remembered key re-opens it")
	}

	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	enc := filepath.Join(t.TempDir(), "Enc")
	p, err := keyring.Create(enc, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	e2 := newKeysEnv(t, enc)
	w, err := keyring.Open(enc, keyring.Password(keysPassword), keyring.OpenOptions{Write: true, Remember: true, Store: e2.cfg.Store})
	if err != nil {
		t.Fatal(err)
	}
	id := w.ArchiveID().String()
	backUpInto(t, enc, w)
	w.Destroy()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(enc, name))
	}
	const newPw = "the password after the restore"
	_, m := e2.post(t, "api/encryption/restore", map[string]any{"from": "remembered", "newPassword": newPw, "again": newPw})
	if m["ok"] != true {
		t.Fatalf("prepared: %v", m)
	}
	if _, f := e2.post(t, "api/encryption/forget-all", map[string]any{"typed": forgetAllWord}); f["ok"] != true {
		t.Fatalf("forget-all: %v", f)
	}
	_, c := e2.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": m["token"], "typed": m["recoveryKey"]})
	rec, _ := encrecord.Load()
	assure.Reached(t, c["ok"] != true && rec.Archives[id] == nil, "the forgotten archive is not recorded again")
}

// covers: MA-816, R37
// Shut down and uninstall wait for a reader's working copy of its index to go
// (KEY-01): when lockAll(true) returns, no reader scratch file is left.
func TestShutDownWaitsForTheReadersWorkingCopy(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if _, body := e.read("/api/search?q=hello"); !strings.Contains(body, "Hello HTML") {
		t.Fatal("positive twin: the reader serves the archive")
	}
	ent := e.cfg.Keys.get(out)
	e.cfg.Keys.lockAll(true)
	ent.readerMu.Lock()
	r := ent.reader
	ent.readerMu.Unlock()
	assure.Reached(t, r == nil && ent.closed, "the entry's reader is gone once lockAll(true) returns")
}

// covers: MA-816, R37
// A refused uninstall leaves the Desktop able to unlock again (REG-01); saving an
// unrelated setting never cancels an unlock of the archive it keeps (REG-02).
func TestARefusedUninstallAndASettingsSaveLeaveUnlockWorking(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.cfg.Keys.lockForUninstall() // as the confirmed uninstall does
	e.cfg.Keys.reopen()           // and undoes when it is refused
	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword}); m["ok"] != true {
		t.Fatalf("an unlock after a refused uninstall: %v", m)
	}
	e.cfg.Keys.lock(out)

	turn := memguard.Process.Derivations()
	turn.Lock()
	done := make(chan map[string]any, 1)
	go func() {
		_, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
		done <- m
	}()
	time.Sleep(300 * time.Millisecond)
	e.cfg.Keys.lockExcept(out) // a settings save that keeps this archive
	turn.Unlock()
	assure.Reached(t, (<-done)["ok"] == true, "an unlock of the kept archive survives a settings save")
}

// covers: MA-816, R37
// The registry under every operation at once, with the race detector: unlocks, locks,
// the idle lock, switches, reader requests, and key changes racing them; afterwards
// Shut down leaves nothing open and nothing prepared.
func TestTheKeyRegistryHoldsUnderRaces(t *testing.T) {
	out := backedUpEncrypted(t)
	saveDir := t.TempDir()
	orig := pickFolder
	pickFolder = func(context.Context, string) (string, error) { return saveDir, nil }
	t.Cleanup(func() { pickFolder = orig })
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword, "remember": true})
	var wg sync.WaitGroup
	stop := time.Now().Add(3 * time.Second)
	run := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				f()
			}
		}()
	}
	run(func() { e.read("/api/search?q=hello") })
	run(func() { e.cfg.Keys.reap(time.Now().Add(keyIdle + time.Minute)) })
	run(func() { e.cfg.Keys.lockExcept("/elsewhere") })
	run(func() { e.cfg.Keys.unlock(out, keysPassword, keyring.OpenOptions{}) })
	run(func() { e.cfg.Keys.lock(out); time.Sleep(5 * time.Millisecond) })
	run(func() {
		_, m := e.post(t, "api/encryption/new-recovery-key", map[string]any{"confirm": true})
		if tok, ok := m["token"].(string); ok {
			e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": tok})
			e.post(t, "api/encryption/new-recovery-key/cancel", map[string]any{"token": tok})
		}
	})
	wg.Wait()
	e.cfg.Keys.lockAll(true)
	if e.cfg.Keys.get(out) != nil {
		t.Fatal("an archive stayed open after Shut down")
	}
}
