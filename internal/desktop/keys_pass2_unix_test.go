//go:build unix

package desktop

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// The P3-3 follow-up adversarial pass's Desktop guard that needs a named pipe (a
// FIFO) to stand for a disk that hangs: Unix only, so the package's other tests build
// for Windows (the P3-8 Windows lab, B30).

// covers: MA-825, R37
// A recovery file on a disk that hangs never freezes the Desktop (LIFE-02, LIFE-03): a
// second new recovery key replacing one whose file cannot be voided, and a second save
// of its file, leave the registry, Lock, the idle lock and Shut down's drains running.
func TestAHungRecoveryFileNeverFreezesTheDesktop(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	d := newDashboard(e.cfg)
	m := callAction(t, d, d.newRecoveryAction, map[string]any{"confirm": true})
	tok := str(m["token"])
	fifo := filepath.Join(t.TempDir(), "recovery.txt")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	d.pending.mu.Lock()
	d.pending.items[tok].file = fifo // its recovery file, on a disk that hangs
	d.pending.mu.Unlock()
	defer func() { // unblock the void
		if f, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			time.Sleep(200 * time.Millisecond)
			f.Close()
		}
	}()
	go callAction(t, d, d.newRecoveryAction, map[string]any{"confirm": true}) // replaces it
	time.Sleep(time.Second)
	done := make(chan struct{})
	go func() {
		e.cfg.Keys.touch()
		e.post(t, "api/encryption/lock", nil)
		e.cfg.Keys.reap(time.Now().Add(keyIdle + time.Minute))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a recovery file on a hung disk froze the registry, Lock or the idle lock")
	}

	// A second save, and Shut down's drains behind the store's lock.
	saveDir := t.TempDir()
	origPick := pickFolder
	pickFolder = func(context.Context, string) (string, error) { return saveDir, nil }
	defer func() { pickFolder = origPick }()
	life := NewLifecycle(context.Background())
	store := graphconfig.NewFileSecretStore(t.TempDir())
	keys := newArchiveKeys(store, readerLockedNote)
	life.OnDrain(func(context.Context) error { keys.lockAll(true); return nil })
	settings := filepath.Join(t.TempDir(), "s.json")
	SaveSettings(settings, Settings{Out: out})
	d2 := newDashboard(Config{SettingsPath: settings, Store: store, Keys: keys, Lifecycle: life})
	later := make(chan struct{})
	life.OnDrain(func(context.Context) error { close(later); return nil })
	if _, err := keys.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	m = callAction(t, d2, d2.newRecoveryAction, map[string]any{"confirm": true})
	tok = str(m["token"])
	fifo2 := filepath.Join(t.TempDir(), "first recovery file.txt")
	syscall.Mkfifo(fifo2, 0o600)
	d2.pending.mu.Lock()
	d2.pending.items[tok].file = fifo2
	d2.pending.mu.Unlock()
	defer func() {
		if f, err := os.OpenFile(fifo2, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			time.Sleep(200 * time.Millisecond)
			f.Close()
		}
	}()
	go callAction(t, d2, d2.saveRecoveryFileAction, map[string]any{"token": tok})
	time.Sleep(time.Second)
	life.Stop()
	select {
	case <-later:
	case <-time.After(5 * time.Second):
		t.Fatal("Shut down's later drains stalled behind a hung recovery file")
	}
}
