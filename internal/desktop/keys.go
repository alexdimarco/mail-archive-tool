package desktop

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// The Desktop's encrypted archives open now (design-encrypted-archives-phase3 §3.1,
// §3.2, §3.7, K4, O3; the P3-3 plan, 3c). Only in memory, never written anywhere:
// the folder-only archive list holds no unlock state (MA-662). Each open is the
// keyring's — no key leaves internal/keyring — kept here by the folder the open
// used, with its archive id beside it as a check. It is locked on Lock, after 30
// minutes of no one using the Desktop, when the current archive changes, and at
// Shut down; locking never waits on a request that is still reading.

// keyIdle is how long the Desktop may go unused before every open archive locks (K4).
const keyIdle = 30 * time.Minute

// readerLockedNote is what the reader of a locked encrypted archive says (§7.1).
const readerLockedNote = "Unlock it in MailArchive Desktop: on the Overview, its Encryption card asks for the password or recovery key. Then reload this page."

// The keyring calls the registry makes: seams, so a test can count them.
var (
	openArchive       = keyring.OpenTyped
	rememberedArchive = keyring.Remembered
	hasRemembered     = func(out string, s graphconfig.SecretStore) bool { return s != nil && keyring.HasRemembered(out, s) }
	readerSource      = keyring.ReaderSource
)

// errUnlockBusy refuses a second unlock of an archive while one is still deriving
// its key: one derivation at a time (§3.7).
var errUnlockBusy = errors.New("this archive is already being unlocked — wait for it to finish")

// errKeysClosed refuses an unlock once MailArchive Desktop is shutting down.
var errKeysClosed = errors.New("MailArchive Desktop is shutting down, so nothing more is unlocked")

// archiveKeys is the registry. Its zero value is not usable; newArchiveKeys makes one.
type archiveKeys struct {
	store graphconfig.SecretStore
	now   func() time.Time
	note  string // the reader's locked note

	mu       sync.Mutex
	entries  map[string]*keyEntry
	busy     map[string]bool // an unlock deriving its key
	held     map[string]bool // locked by the person: no silent re-open this process (O3)
	closed   bool
	epoch    uint64
	lastUsed time.Time
	onLock   []func(key string) // told each archive that locks: its prepared key changes go (3e)
}

// whenLocked has fn told the key of every archive that locks, by any path.
func (k *archiveKeys) whenLocked(fn func(key string)) {
	if k == nil {
		return
	}
	k.mu.Lock()
	k.onLock = append(k.onLock, fn)
	k.mu.Unlock()
}

// locked tells the listeners that the archive under key locked.
func (k *archiveKeys) locked(key string) {
	k.mu.Lock()
	fns := append([]func(string){}, k.onLock...)
	k.mu.Unlock()
	for _, fn := range fns {
		fn(key)
	}
}

// keyEntry is one open archive and, once the reader asked, its reader.
type keyEntry struct {
	u    *keyring.Unlocked
	path string
	id   string

	readerMu sync.Mutex
	reader   *keyring.Reader
	lease    *keyring.Unlocked // the reader's own lease: its lock flag is its own (§3.2)
}

// close locks the entry: its reader first (never waiting), then its lease and its
// open, so no request decrypts after it and the reader's index, pages and scratch
// file go at once. wait: also wait for the reader's scratch file to be removed, as
// Shut down does before the program ends.
func (e *keyEntry) close(wait bool) {
	e.readerMu.Lock()
	r, l := e.reader, e.lease
	e.reader, e.lease = nil, nil
	e.readerMu.Unlock()
	if r != nil {
		r.Lock()
		if wait {
			r.Close()
		} else {
			go r.Close() // removes the scratch file; it may wait, the lock never does
		}
	}
	if l != nil {
		l.Lock()
	}
	e.u.Lock()
}

func newArchiveKeys(store graphconfig.SecretStore, note string) *archiveKeys {
	k := &archiveKeys{store: store, now: time.Now, note: note,
		entries: map[string]*keyEntry{}, busy: map[string]bool{}, held: map[string]bool{}}
	k.lastUsed = k.now()
	return k
}

// keyOf is the registry's key for out: the absolute folder in this computer's
// spelling (a letter case folded where paths fold).
func keyOf(out string) (abs, key string) {
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = filepath.Clean(out)
	}
	return abs, encrecord.Spelling(abs)
}

// get is the open archive at out, or nil when it is locked.
func (k *archiveKeys) get(out string) *keyEntry {
	if k == nil {
		return nil
	}
	_, key := keyOf(out)
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.entries[key]
}

// unlock opens the archive at out with what the person typed. One derivation at a
// time per archive; a lock or Shut down that lands while it derives wins — the new
// key is destroyed and the unlock refused (§3.2).
func (k *archiveKeys) unlock(out, typed string, o keyring.OpenOptions) (*keyEntry, error) {
	return k.open(out, func(abs string) (*keyring.Unlocked, error) { return openArchive(abs, typed, o) })
}

// remembered opens the archive at out by itself with the key this computer
// remembers for it (O3), unless the person locked it in this process; nil when
// there is no such key.
func (k *archiveKeys) remembered(out string) *keyEntry {
	if k == nil {
		return nil
	}
	abs, key := keyOf(out)
	k.mu.Lock()
	e, held := k.entries[key], k.held[key]
	k.mu.Unlock()
	if e != nil {
		return e
	}
	if held || !hasRemembered(abs, k.store) {
		return nil
	}
	e, err := k.open(out, func(abs string) (*keyring.Unlocked, error) {
		return rememberedArchive(abs, keyring.OpenOptions{Store: k.store})
	})
	if err != nil {
		return nil
	}
	return e
}

func (k *archiveKeys) open(out string, opener func(abs string) (*keyring.Unlocked, error)) (*keyEntry, error) {
	abs, key := keyOf(out)
	k.mu.Lock()
	switch {
	case k.closed:
		k.mu.Unlock()
		return nil, errKeysClosed
	case k.entries[key] != nil:
		e := k.entries[key]
		k.mu.Unlock()
		return e, nil
	case k.busy[key]:
		k.mu.Unlock()
		return nil, errUnlockBusy
	}
	k.busy[key] = true
	epoch := k.epoch
	k.mu.Unlock()

	u, err := opener(abs)

	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.busy, key)
	if err != nil {
		return nil, err
	}
	if k.closed || k.epoch != epoch {
		u.Destroy()
		return nil, keyring.ErrLocked
	}
	e := &keyEntry{u: u, path: abs, id: u.ArchiveID().String()}
	k.entries[key] = e
	delete(k.held, key)
	k.lastUsed = k.now()
	return e, nil
}

// readerFor is the reader of the open archive at out, made on the first request
// from a lease of its own; nil when the archive is locked.
func (k *archiveKeys) readerFor(out string) (*keyring.Reader, error) {
	e := k.get(out)
	if e == nil {
		e = k.remembered(out)
	}
	if e == nil {
		return nil, nil
	}
	e.readerMu.Lock()
	defer e.readerMu.Unlock()
	if e.reader != nil {
		return e.reader, nil
	}
	l, err := e.u.Lease()
	if err != nil {
		return nil, err
	}
	r, err := readerSource(l, keyring.ReaderOptions{LockedNote: k.note})
	if err != nil {
		l.Lock()
		return nil, err
	}
	// Locked while the reader was made: it is not kept.
	if k.get(out) != e {
		r.Lock()
		go r.Close()
		l.Lock()
		return nil, nil
	}
	e.reader, e.lease = r, l
	return r, nil
}

// lock locks the archive at out because the person asked: a remembered key does not
// re-open it by itself until they unlock it again (O3).
func (k *archiveKeys) lock(out string) {
	_, key := keyOf(out)
	k.mu.Lock()
	e := k.entries[key]
	delete(k.entries, key)
	k.held[key] = true
	k.epoch++
	k.mu.Unlock()
	if e != nil {
		e.close(false)
	}
	k.locked(key)
}

// lockExcept locks every open archive but the one at out: the current archive
// changed (K4).
func (k *archiveKeys) lockExcept(out string) {
	if k == nil {
		return
	}
	_, keep := keyOf(out)
	k.closeWhere(func(key string) bool { return key != keep }, false, false)
}

// lockAll locks every open archive, waiting until each reader's working copy of its
// index is removed, and, when closing, refuses every unlock after it: MailArchive
// Desktop is shutting down, or being uninstalled.
func (k *archiveKeys) lockAll(closing bool) {
	if k == nil {
		return
	}
	k.closeWhere(func(string) bool { return true }, closing, true)
}

func (k *archiveKeys) closeWhere(match func(key string) bool, closing, wait bool) {
	k.mu.Lock()
	if closing {
		k.closed = true
	}
	var gone []*keyEntry
	var keys []string
	for key, e := range k.entries {
		if match(key) {
			gone = append(gone, e)
			keys = append(keys, key)
			delete(k.entries, key)
		}
	}
	k.epoch++
	k.mu.Unlock()
	for _, e := range gone {
		e.close(wait)
	}
	for _, key := range keys {
		k.locked(key)
	}
}

// keyPolls are the dashboard's own repeated requests, which are never a person's use
// of it (K4): counted, an open page would keep an archive unlocked for good.
var keyPolls = map[string]bool{
	"/api/activity": true, "/api/schedule-state": true, "/api/health": true,
	"/api/exports/progress": true, "/api/exports/history": true,
	"/api/m365/signin-status": true, "/api/workspace/preview-status": true,
}

// touching notes a person's use of the dashboard — a page, or an action they took —
// before next serves it (K4).
func touching(k *archiveKeys, next http.Handler) http.Handler {
	if k == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !keyPolls[r.URL.Path] {
			k.touch()
		}
		next.ServeHTTP(w, r)
	})
}

// touch notes that a person used the Desktop now (K4).
func (k *archiveKeys) touch() {
	if k == nil {
		return
	}
	k.mu.Lock()
	k.lastUsed = k.now()
	k.mu.Unlock()
}

// reap locks every open archive once the Desktop has gone unused for keyIdle
// (K4). A remembered archive re-opens by itself at its next use.
func (k *archiveKeys) reap(now time.Time) {
	if k == nil {
		return
	}
	k.mu.Lock()
	idle := now.Sub(k.lastUsed) >= keyIdle
	k.mu.Unlock()
	if idle {
		k.closeWhere(func(string) bool { return true }, false, false)
	}
}

// startReaper runs reap once a minute until done is closed.
func (k *archiveKeys) startReaper(done <-chan struct{}) {
	if k == nil {
		return
	}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-t.C:
				k.reap(now)
			}
		}
	}()
}
