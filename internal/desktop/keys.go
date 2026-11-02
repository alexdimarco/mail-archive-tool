package desktop

import (
	"errors"
	"net/http"
	"os"
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
	watchers map[int]keyWatch // one-shot: told when their archive locks
	watchID  int
	entries  map[string]*keyEntry
	busy     map[string]bool   // an open under way: deriving its key, or a remembered key looked up
	held     map[string]bool   // locked by the person: no silent re-open this process (O3)
	gen      map[string]uint64 // bumped for an archive each time it is locked: an open begun before loses
	// deleting counts the deletes under way of an archive (design-archive-delete §5.3 step 1
	// (b), MA-974): every open of it is refused meanwhile, the person's own Unlock included.
	deleting map[string]int
	// deletingAs are those deletes' folders by file identity, and the other keys each marked
	// (an entry open under another spelling of the same folder), so an open under any spelling
	// is refused and EndDelete clears what LockForDelete set (del-s3a gap 3, MA-978).
	deletingAs []*deleteMark
	hushed     int    // opens refused meanwhile: every remembered key is being forgotten
	forgets    uint64 // bumped by every forget-all: a key change prepared across one is not kept
	closed     bool   // Shut down: nothing more is unlocked, for good
	// uninstalling counts confirmed uninstalls under way: nothing is unlocked
	// meanwhile; one that is refused gives the Desktop back (reopen), never Shut
	// down's refusal (the P3-3 follow-up pass, LIFE-04).
	uninstalling int
	// lastUsed is the last time a person used the Desktop (K4).
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

// keyWatch is a one-shot watcher of one archive's lock.
type keyWatch struct {
	key string
	fn  func()
}

// watchLock has fn called if the archive under key locks before the returned func
// is: a key change in progress gives up its lease (the P3-3 pass, KEY-03).
func (k *archiveKeys) watchLock(key string, fn func()) (stop func()) {
	if k == nil {
		return func() {}
	}
	k.mu.Lock()
	if k.watchers == nil {
		k.watchers = map[int]keyWatch{}
	}
	k.watchID++
	id := k.watchID
	k.watchers[id] = keyWatch{key: key, fn: fn}
	k.mu.Unlock()
	return func() {
		k.mu.Lock()
		delete(k.watchers, id)
		k.mu.Unlock()
	}
}

// locked tells the listeners, and the watchers of that archive, that the archive
// under key locked.
func (k *archiveKeys) locked(key string) {
	k.mu.Lock()
	fns := append([]func(string){}, k.onLock...)
	var once []func()
	for id, w := range k.watchers {
		if w.key == key {
			once = append(once, w.fn)
			delete(k.watchers, id)
		}
	}
	k.mu.Unlock()
	for _, fn := range once {
		fn()
	}
	for _, fn := range fns {
		fn(key)
	}
}

// keyEntry is one open archive and, once the reader asked, its reader.
type keyEntry struct {
	u    *keyring.Unlocked
	path string
	id   string

	buildMu  sync.Mutex // one reader made at a time; never held by close
	readerMu sync.Mutex // guards the fields below, never held across a load
	reader   *keyring.Reader
	lease    *keyring.Unlocked // the reader's own lease: its lock flag is its own (§3.2)
	building *keyring.Unlocked // the lease of a reader being made, until it is kept or dropped
	closed   bool

	// lastSync is the last encrypted Run now's Thunderbird sync phrase while this
	// entry is open (design-presync-attended §4.7, C-27): memory only, gone with the
	// entry at any lock. Guarded by lastSyncMu.
	lastSync string
}

// buildWait bounds how long Shut down and uninstall wait for a reader being made to
// give up after its lease is locked.
const buildWait = 10 * time.Second

// close locks the entry: its open, its reader's lease and the lease of a reader
// being made first, so no request decrypts after it and a build fails at once (the
// P3-3 follow-up pass, LIFE-01), then its reader, so its index, pages and scratch
// file go. It never waits on a load (§3.2). wait: also wait until the reader's
// working copy of its index is removed and a build has given up, as Shut down and
// uninstall do before what follows them.
func (e *keyEntry) close(wait bool) {
	e.readerMu.Lock()
	r, l, b := e.reader, e.lease, e.building
	e.reader, e.lease, e.closed = nil, nil, true
	e.readerMu.Unlock()
	e.u.Lock()
	for _, x := range []*keyring.Unlocked{l, b} {
		if x != nil {
			x.Lock()
		}
	}
	if r != nil {
		if wait {
			r.Close() // locks it, and waits for its scratch file to go
		} else {
			r.Lock()
			go r.Close() // removes the scratch file; it may wait, the lock never does
		}
	}
	if wait {
		done := make(chan struct{})
		go func() { e.buildMu.Lock(); e.buildMu.Unlock(); close(done) }()
		select {
		case <-done:
		case <-time.After(buildWait):
		}
	}
}

func newArchiveKeys(store graphconfig.SecretStore, note string) *archiveKeys {
	k := &archiveKeys{store: store, now: time.Now, note: note,
		entries: map[string]*keyEntry{}, busy: map[string]bool{}, held: map[string]bool{}, gen: map[string]uint64{}, deleting: map[string]int{}}
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
	return k.open(out, false, nil, func(abs string) (*keyring.Unlocked, error) { return openArchive(abs, typed, o) })
}

// unlockRemembered opens the archive at out with the key this computer remembers
// because the person clicked Unlock: a Lock they made earlier no longer holds it.
func (k *archiveKeys) unlockRemembered(out string) (*keyEntry, error) {
	return k.open(out, false, nil, func(abs string) (*keyring.Unlocked, error) {
		return rememberedArchive(abs, keyring.OpenOptions{Store: k.store})
	})
}

// remembered opens the archive at out by itself with the key this computer
// remembers for it (O3), unless the person locked it in this process; nil when
// there is no such key. The remembered key is looked up as part of the open, so a
// Lock or a switch that lands meanwhile wins.
func (k *archiveKeys) remembered(out string) *keyEntry {
	if k == nil {
		return nil
	}
	e, err := k.open(out, true, func(abs string) bool { return hasRemembered(abs, k.store) }, func(abs string) (*keyring.Unlocked, error) {
		return rememberedArchive(abs, keyring.OpenOptions{Store: k.store})
	})
	if err != nil {
		return nil
	}
	return e
}

// errNotRemembered: a silent re-open found no remembered key, or was not allowed.
var errNotRemembered = errors.New("no remembered key opens this archive now")

// open opens the archive at out through opener, one open at a time per archive. A
// silent open (a remembered key, nobody asking) is refused while the person holds
// the archive locked or every remembered key is being forgotten, and goes on only
// when may (if given) says so. An open begun before a lock of the same archive, or before
// Shut down, loses: its key is destroyed (§3.2). Only an open the person asked for
// clears their Lock.
func (k *archiveKeys) open(out string, silent bool, may func(abs string) bool, opener func(abs string) (*keyring.Unlocked, error)) (*keyEntry, error) {
	abs, key := keyOf(out)
	fi := statOrNil(abs) // the folder's identity, for a delete of it under another spelling
	k.mu.Lock()
	switch {
	case k.shut():
		k.mu.Unlock()
		return nil, errKeysClosed
	case k.deleting[key] > 0 || k.deletingSame(fi):
		k.mu.Unlock()
		return nil, errBeingDeleted
	case k.hushed > 0 && !silent:
		k.mu.Unlock()
		return nil, errKeysHushed
	case k.entries[key] != nil:
		e := k.entries[key]
		k.mu.Unlock()
		return e, nil
	case k.busy[key]:
		k.mu.Unlock()
		return nil, errUnlockBusy
	case silent && (k.held[key] || k.hushed > 0):
		k.mu.Unlock()
		return nil, errNotRemembered
	}
	k.busy[key] = true
	gen := k.gen[key]
	k.mu.Unlock()

	var u *keyring.Unlocked
	var err error
	if may != nil && !may(abs) {
		err = errNotRemembered
	} else {
		u, err = opener(abs)
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.busy, key)
	if err != nil {
		return nil, err
	}
	// A Lock and a hush both bump the generation of an open under way, so this
	// also refuses a silent open the person's Lock or a forget-all landed on; a delete of the
	// same folder under another spelling begun meanwhile does too.
	if k.shut() || k.gen[key] != gen {
		u.Destroy()
		return nil, keyring.ErrLocked
	}
	if k.deletingSame(fi) {
		u.Destroy()
		return nil, errBeingDeleted
	}
	e := &keyEntry{u: u, path: abs, id: u.ArchiveID().String()}
	k.entries[key] = e
	if !silent {
		delete(k.held, key)
	}
	k.lastUsed = k.now()
	return e, nil
}

// ifOpen runs fn while e is still the archive open at out, under the registry's
// lock, so no lock of it lands meanwhile; false, fn not run, when it is not. fn must
// be quick and must not call back into the registry.
func (k *archiveKeys) ifOpen(out string, e *keyEntry, fn func()) bool {
	_, key := keyOf(out)
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.shut() || e == nil || k.entries[key] != e {
		return false
	}
	fn()
	return true
}

// readerFor is the reader of the open archive at out, made on the first request
// from a lease of its own; nil when the archive is locked. The reader is made
// without holding what a lock takes, so a lock never waits on it; one made for an
// archive locked meanwhile is not kept.
func (k *archiveKeys) readerFor(out string) (*keyring.Reader, error) {
	e := k.get(out)
	if e == nil {
		e = k.remembered(out)
	}
	if e == nil {
		return nil, nil
	}
	e.buildMu.Lock()
	defer e.buildMu.Unlock()
	e.readerMu.Lock()
	r, closed := e.reader, e.closed
	e.readerMu.Unlock()
	if r != nil || closed {
		return r, nil
	}
	l, err := e.u.Lease()
	if errors.Is(err, keyring.ErrLocked) {
		return nil, nil // locked as the reader was asked for: the locked page (CRIT-01)
	}
	if err != nil {
		return nil, err
	}
	// The build's lease is on the entry, so a lock that lands while it loads locks it
	// and the load fails at once (LIFE-01).
	e.readerMu.Lock()
	if e.closed {
		e.readerMu.Unlock()
		l.Lock()
		return nil, nil
	}
	e.building = l
	e.readerMu.Unlock()
	r, err = readerSource(l, keyring.ReaderOptions{LockedNote: k.note})
	e.readerMu.Lock()
	e.building = nil
	closedMeanwhile := e.closed
	e.readerMu.Unlock()
	if err != nil {
		l.Lock()
		if closedMeanwhile {
			return nil, nil // the lock, not damage: the locked page (CRIT-01)
		}
		return nil, err
	}
	e.readerMu.Lock()
	kept := !e.closed && k.get(out) == e
	if kept {
		e.reader, e.lease = r, l
	}
	e.readerMu.Unlock()
	if !kept { // locked while the reader was made
		l.Lock()
		k.mu.Lock()
		closing := k.shut()
		k.mu.Unlock()
		if closing {
			r.Close() // Shut down waits for its working copy to go
		} else {
			r.Lock()
			go r.Close()
		}
		return nil, nil
	}
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
	k.gen[key]++
	k.mu.Unlock()
	if e != nil {
		e.close(false)
	}
	k.locked(key)
}

// errBeingDeleted refuses every open of an archive a delete has locked (MA-974).
var errBeingDeleted = errors.New("This archive is being deleted.")

// LockForDelete is the delete's step 1 (b) (design-archive-delete §5.3, §12 Q3;
// archdelete.KeyRegistry): all under the registry's lock it does what lock does — the
// archive's entry removed, held set, its generation bumped, so an open begun before it loses —
// and sets the archive's "being deleted" mark, which every open checks, the person's own
// Unlock (typed or remembered) included, until EndDelete; then it closes the archive's reader
// and waits for it (close(true), as lockAll does: lock's close does not wait, R4:NF-R4-LOCK-
// NO-WAIT). It never hushes: that would lock every archive (C13).
func (k *archiveKeys) LockForDelete(folder string) error {
	if k == nil {
		return errors.New("MailArchive Desktop keeps no encrypted archives open here")
	}
	_, key := keyOf(folder)
	fi := statOrNil(folder)
	// The archive may be open here under another spelling of the folder than the row's (the
	// current archive set to a mapped drive, the row its network path): found by file identity,
	// asked of the file system outside the registry's lock (del-s3a gap 3, MA-978). The folder's
	// mark goes on the delete list in this first section, so an Unlock under another spelling that
	// finishes between the two sections is refused by open's own check (del-s3a re-verification,
	// R2); its other keys are filled in the second.
	mark := &deleteMark{key: key, fi: fi, keys: []string{key}}
	k.mu.Lock()
	k.deletingAs = append(k.deletingAs, mark)
	open := map[string]string{}
	for ek, e := range k.entries {
		if ek != key {
			open[ek] = e.path
		}
	}
	k.mu.Unlock()
	keys := []string{key}
	if fi != nil {
		for ek, p := range open {
			if pfi := statOrNil(p); pfi != nil && os.SameFile(pfi, fi) {
				keys = append(keys, ek)
			}
		}
	}
	if lockForDeleteGap != nil {
		lockForDeleteGap()
	}
	k.mu.Lock()
	var closing []*keyEntry
	for _, kk := range keys {
		if e := k.entries[kk]; e != nil {
			closing = append(closing, e)
		}
		delete(k.entries, kk)
		k.held[kk] = true
		k.gen[kk]++
		k.deleting[kk]++
	}
	mark.keys = keys
	k.mu.Unlock()
	for _, e := range closing {
		e.close(true)
	}
	for _, kk := range keys {
		k.locked(kk)
	}
	return nil
}

// lockForDeleteGap, when set, runs between LockForDelete's two locked sections: a test seam that
// lands an Unlock in that gap (fault injection only).
var lockForDeleteGap func()

// deleteMark is one LockForDelete: its folder's key and identity, and every key it marked.
type deleteMark struct {
	key  string
	fi   os.FileInfo
	keys []string
}

// statOrNil is the folder's os.Stat, nil when it cannot be read.
func statOrNil(p string) os.FileInfo {
	fi, err := os.Stat(p)
	if err != nil {
		return nil
	}
	return fi
}

// deletingSame reports whether a delete under way is of the folder fi is. Called with k.mu held.
func (k *archiveKeys) deletingSame(fi os.FileInfo) bool {
	if fi == nil {
		return false
	}
	for _, m := range k.deletingAs {
		if m.fi != nil && os.SameFile(m.fi, fi) {
			return true
		}
	}
	return false
}

// EndDelete clears the "being deleted" mark LockForDelete set, under every key it set it; the
// archive stays locked (held) until the person unlocks it again.
func (k *archiveKeys) EndDelete(folder string) {
	if k == nil {
		return
	}
	_, key := keyOf(folder)
	k.mu.Lock()
	keys := []string{key}
	for i := len(k.deletingAs) - 1; i >= 0; i-- {
		if k.deletingAs[i].key == key {
			keys = k.deletingAs[i].keys
			k.deletingAs = append(k.deletingAs[:i:i], k.deletingAs[i+1:]...)
			break
		}
	}
	for _, kk := range keys {
		if k.deleting[kk] > 0 {
			k.deleting[kk]--
		}
		if k.deleting[kk] == 0 {
			delete(k.deleting, kk)
		}
	}
	k.mu.Unlock()
}

// lockExcept locks every open archive but the one at out: the current archive
// changed (K4). An archive the person locked and switched away from may re-open by
// its remembered key once it is current again (O3: a switch).
func (k *archiveKeys) lockExcept(out string) {
	if k == nil {
		return
	}
	_, keep := keyOf(out)
	k.closeWhere(func(key string) bool { return key != keep }, false, false, true)
}

// lockAll locks every open archive, waiting until each reader's working copy of its
// index is removed, and, when closing, refuses every unlock after it: MailArchive
// Desktop is shutting down, or being uninstalled.
func (k *archiveKeys) lockAll(closing bool) {
	if k == nil {
		return
	}
	k.closeWhere(func(string) bool { return true }, closing, true, false)
}

// shut reports whether nothing may be unlocked now: Shut down, or an uninstall
// under way. Called with k.mu held.
func (k *archiveKeys) shut() bool { return k.closed || k.uninstalling > 0 }

// lockForUninstall locks every open archive, waiting for each reader's working copy
// to go, and refuses every unlock until reopen: a confirmed uninstall is under way.
func (k *archiveKeys) lockForUninstall() {
	if k == nil {
		return
	}
	k.mu.Lock()
	k.uninstalling++
	k.mu.Unlock()
	k.closeWhere(func(string) bool { return true }, false, true, false)
}

// reopen lets archives be unlocked again after an uninstall that was refused. It
// never undoes Shut down.
func (k *archiveKeys) reopen() {
	if k == nil {
		return
	}
	k.mu.Lock()
	if k.uninstalling > 0 {
		k.uninstalling--
	}
	k.mu.Unlock()
}

// hush locks every open archive and refuses every silent re-open until the returned
// func is called: every remembered key is being forgotten, and none may open an
// archive meanwhile.
func (k *archiveKeys) hush() (release func()) {
	if k == nil {
		return func() {}
	}
	k.mu.Lock()
	k.hushed++
	k.forgets++
	k.mu.Unlock()
	k.closeWhere(func(string) bool { return true }, false, true, false)
	// Every open already under way finishes first, so what it remembered or recorded
	// on this computer is forgotten with the rest (the P3-3 follow-up pass, CRIT-02).
	for deadline := time.Now().Add(hushWait); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		k.mu.Lock()
		n := len(k.busy)
		k.mu.Unlock()
		if n == 0 {
			break
		}
	}
	return func() {
		k.mu.Lock()
		k.hushed--
		k.mu.Unlock()
	}
}

// hushWait bounds how long a hush waits for opens under way to finish.
const hushWait = 30 * time.Second

// forgetMark is the count of forget-alls so far: a key change prepared from now on
// is kept only while it has not moved (LIFE-06).
func (k *archiveKeys) forgetMark() uint64 {
	if k == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.forgets
}

// forgotSince reports whether a forget-all began or ran since mark.
func (k *archiveKeys) forgotSince(mark uint64) bool {
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.forgets != mark || k.hushed > 0
}

// unlessForgotten runs fn under the registry's lock unless a forget-all began or ran
// since mark: what fn keeps is then either kept before the forget-all, which ends it,
// or not kept at all (LIFE-06). fn must be quick and must not call back into the
// registry.
func (k *archiveKeys) unlessForgotten(mark uint64, fn func()) bool {
	if k == nil {
		fn()
		return true
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.forgets != mark || k.hushed > 0 {
		return false
	}
	fn()
	return true
}

// errKeysHushed refuses an unlock while every remembered key is being forgotten.
var errKeysHushed = errors.New("every encrypted archive is being forgotten on this computer — try again in a moment")

// closeWhere locks every open archive match names, and makes every open of one under
// way lose. unhold: the person's Lock no longer holds those archives.
func (k *archiveKeys) closeWhere(match func(key string) bool, closing, wait, unhold bool) {
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
			k.gen[key]++
		}
	}
	for key := range k.busy {
		if match(key) {
			k.gen[key]++
		}
	}
	if unhold {
		for key := range k.held {
			if match(key) {
				delete(k.held, key)
			}
		}
	}
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
		k.closeWhere(func(string) bool { return true }, false, false, false)
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
