package keyring

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The encrypted archive's reader (design-encrypted-archives-phase3 §7): a
// server.Source over the sealed record, built here so no key leaves this package.
// It loads without the archive lock and writes nothing (§7.3); it holds the key
// only while it names or opens something (§3.2), and once locked every request
// gets the locked page, never a damage message.

// ReaderOptions say how an encrypted archive's reader behaves.
type ReaderOptions struct {
	// LockedNote is what the reader says once locked, and how to unlock it again.
	LockedNote string
	// WriteTimeout bounds how long a file response may make no progress, so a
	// client that stops reading gives back its memory budget within it (§3.2).
	// Zero: fifteen seconds.
	WriteTimeout time.Duration
	// Guard is the memory guard unit reads share (§3.7). Nil: the process's.
	Guard *memguard.Guard
}

// Reader is an encrypted archive's server.Source. Lock ends it; Close ends it and
// waits until what it held is gone.
type Reader struct {
	u   *Unlocked
	out string
	o   ReaderOptions

	locked atomic.Bool
	loadMu sync.Mutex // one load at a time; requests that need a view wait behind it (§7.4)

	mu       sync.Mutex // guards the fields below, briefly: never held across a load
	view     *readView
	peekedAt time.Time
	stale    bool // a request missed something while the record moved: load again
}

// readView is one loaded generation of the sealed record.
type readView struct {
	peek  container.Peeked
	m     *state.Manifest
	store *container.Store
	gaps  int

	ixMu   sync.RWMutex
	ix     *index.Index
	hold   io.Closer // the OS lock on a scratch index's file, while it is open
	ixErr  error     // the index cannot be used: none yet, damaged, or missing in part
	behind bool      // the record says the index is behind the archive
	closed bool

	pagesOnce sync.Once
	pages     map[string][]byte
	pagesErr  error
}

// Seams for tests: the clock freshness is judged by; a hook between a load's two
// peeks (a test commits there); hooks around a request's unit read (a test locks
// there); and the record load itself (a test fails or locks one).
var (
	readerNow          = time.Now
	readerLoad         = func() {}
	readerBefore       = func() {}
	readerBetween      = func() {}
	readerLoadManifest = container.LoadManifest
)

// readerPeekEvery is how often a reader looks for a backup's commit (§7.3).
const readerPeekEvery = 2 * time.Second

// readerCloseWait bounds how long Close waits for requests still using the view.
var readerCloseWait = 10 * time.Second

// readerIndexBudget is the largest index a reader loads into memory (§7.4).
var readerIndexBudget int64 = 256 << 20

// readerLoads numbers this process's scratch index loads, so no two share a file.
var readerLoads atomic.Uint64

// errBusy refuses a load that two commits in a row overtook (§7.3).
var errBusy = fmt.Errorf("%w: a backup is running on this archive; try again in a moment", server.ErrUnavailable)

// errNoIndex is an encrypted archive with no search index yet.
var errNoIndex = fmt.Errorf("%w: search is unavailable — this encrypted archive has no search index yet; `mailarchive reindex -rebuild` builds one", server.ErrUnavailable)

// indexUnusable is search unavailable for a reason, naming the remedy (§7.4).
func indexUnusable(why string, err error) error {
	return fmt.Errorf("%w: search is unavailable — %s; `mailarchive reindex -rebuild` rebuilds it (%v)", server.ErrUnavailable, why, err)
}

// ReaderSource is the reader of the encrypted archive u opened (§3.1, §7.2). It
// sweeps the scratch indexes gone readers left, and loads the record now, so a
// refusal is told before anything is served.
func ReaderSource(u *Unlocked, o ReaderOptions) (*Reader, error) {
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 15 * time.Second
	}
	if o.Guard == nil {
		o.Guard = memguard.Process
	}
	r := &Reader{u: u, out: u.Out(), o: o}
	app.SweepReaderScratch(r.out, u.ArchiveID())
	if _, err := r.ensure(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

// Lock ends the reader: every request from now gets the locked page; the index,
// the rendered pages and a scratch index's file go once the requests using them
// end. It never waits.
func (r *Reader) Lock() {
	r.locked.Store(true)
	if v := r.take(); v != nil {
		go v.close()
	}
}

// Close is Lock for a reader's last holder — serve or search ending: it waits,
// within a bound, for a load in progress and for the requests still using the view,
// so a scratch index's plaintext file is gone before the process ends (§7.4).
func (r *Reader) Close() {
	r.locked.Store(true)
	done := make(chan struct{})
	go func() {
		r.loadMu.Lock() // a load in progress publishes nothing once locked, and closes its view
		r.loadMu.Unlock()
		if v := r.take(); v != nil {
			v.close()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(readerCloseWait):
	}
}

// take removes the current view, for its holder to close.
func (r *Reader) take() *readView {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.view
	r.view = nil
	return v
}

// LockedNote implements server.Source.
func (r *Reader) LockedNote() string {
	if r.isLocked() {
		return r.o.LockedNote
	}
	return ""
}

// isLocked reports a lock of the reader, or of the open it reads through — which
// locks the reader too, so what it held is dropped as at its own lock (§3.2).
func (r *Reader) isLocked() bool {
	if r.locked.Load() {
		return true
	}
	if r.u.isLocked() {
		r.Lock()
		return true
	}
	return false
}

// IndexNotice is the search page's note on an index behind the archive (§7.4).
func (r *Reader) IndexNotice() string {
	v, err := r.ensure()
	if err != nil || !v.behind {
		return ""
	}
	return "search may be missing messages: the search index is behind the archive (`mailarchive reindex -rebuild` catches it up)"
}

// lockedErr maps a lock seen while reading to server.ErrLocked.
func lockedErr(err error) error {
	if errors.Is(err, ErrLocked) || errors.Is(err, vault.ErrKeyDestroyed) {
		return fmt.Errorf("%w: %w", server.ErrLocked, err)
	}
	return err
}

// ensure is the current view: loaded again when a peek, at most every two seconds,
// shows a backup committed since it was loaded, or when a request missed something
// while the record moved (§7.3). A view a load finishes after a lock is closed,
// never served.
func (r *Reader) ensure() (*readView, error) {
	if r.isLocked() {
		return nil, server.ErrLocked
	}
	if v := r.fresh(); v != nil {
		return v, nil
	}
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	if r.isLocked() {
		return nil, server.ErrLocked
	}
	if v := r.fresh(); v != nil {
		return v, nil
	}
	r.mu.Lock()
	cur, stale := r.view, r.stale
	r.mu.Unlock()
	if cur != nil && !stale {
		if p, err := container.PeekFile(r.out); err == nil && p == cur.peek {
			r.mu.Lock()
			r.peekedAt = readerNow()
			r.mu.Unlock()
			return cur, nil
		}
	}
	v, err := r.load()
	if err != nil {
		return nil, err
	}
	if r.isLocked() {
		v.close()
		return nil, server.ErrLocked
	}
	r.mu.Lock()
	old := r.view
	r.view, r.peekedAt, r.stale = v, readerNow(), false
	r.mu.Unlock()
	if old != nil {
		go old.close()
	}
	return v, nil
}

// fresh is the current view when it was peeked at within readerPeekEvery.
func (r *Reader) fresh() *readView {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.view != nil && !r.stale && readerNow().Sub(r.peekedAt) < readerPeekEvery {
		return r.view
	}
	return nil
}

// moved reports whether the record moved since seen was loaded, and if so makes
// the next ensure load it again: a unit or chunk a request missed is looked for once
// more in what is current — only when there is something new to look in (§7.3).
func (r *Reader) moved(seen *readView) bool {
	p, err := container.PeekFile(r.out)
	if err == nil && p == seen.peek {
		return false
	}
	r.mu.Lock()
	if r.view == seen {
		r.stale = true
	}
	r.mu.Unlock()
	return true
}

// load reads the record: peek, load, peek again; on a change, once more; on a
// second change, errBusy. A load that failed while the record changed is retried
// the same way: only a refusal of a record that held still is reported (§7.3).
func (r *Reader) load() (*readView, error) {
	for attempt := 0; attempt < 2; attempt++ {
		before, err := container.PeekFile(r.out)
		if err != nil {
			return nil, err
		}
		v, lerr := r.loadOnce(before)
		readerLoad()
		after, err := container.PeekFile(r.out)
		if err == nil && after == before {
			return v, lerr
		}
		if v != nil {
			v.close()
		}
	}
	return nil, errBusy
}

func (r *Reader) loadOnce(peek container.Peeked) (*readView, error) {
	k, _, err := r.u.acquire()
	if err != nil {
		return nil, lockedErr(err)
	}
	defer r.u.release()
	m, s, err := readerLoadManifest(r.out, container.LoadOptions{Key: k, ReadOnly: true})
	if err != nil {
		return nil, lockedErr(err)
	}
	v := &readView{peek: peek, m: m, store: s, behind: s.IndexBehind(),
		gaps: m.MissingGapCount() + m.UnreadableCount() + m.UnparsedCount()}
	switch {
	case !s.HasIndex():
		v.ixErr = errNoIndex
	case s.IndexSize() > readerIndexBudget:
		// Above the memory budget the index is worked on in this reader's own
		// scratch file, outside the archive, removed when the view closes (§7.4).
		if err := r.scratchIndex(v, s); err != nil {
			return nil, err
		}
	default:
		img, err := s.IndexImage()
		if err != nil {
			if lerr := lockedErr(err); errors.Is(lerr, server.ErrLocked) {
				return nil, lerr
			}
			v.ixErr = indexWhy(err)
			break
		}
		if v.ix, err = index.OpenMemory(img); err != nil {
			v.ixErr = indexUnusable("the search index is damaged", err)
		}
	}
	return v, nil
}

// indexWhy is search unavailable for an index that could not be read: part of it
// missing — never called damage (§7.3) — or damaged. A load whose record moved
// meanwhile is loaded again (load), so a chunk a backup swept is not reported.
func indexWhy(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		// The cause is not quoted: the index layer words every unreadable chunk as damage.
		return fmt.Errorf("%w: search is unavailable — part of the search index is missing; `mailarchive reindex -rebuild` rebuilds it", server.ErrUnavailable)
	}
	return indexUnusable("the search index is damaged", err)
}

// scratchIndex loads the index into the reader's scratch file now, under the key
// the load holds, and holds an OS lock on the file while it is open, so a sweep
// never takes a live reader's (§7.4): an image that cannot be read leaves search
// unavailable, a lock is the load's error.
func (r *Reader) scratchIndex(v *readView, s *container.Store) error {
	path, err := app.ReaderScratch(r.out, r.u.ArchiveID(), readerLoads.Add(1))
	if err != nil {
		v.ixErr = indexUnusable("there is no working folder for the search index", err)
		return nil
	}
	// The lock first, then the file: a sweep never finds this index without it.
	hold, err := app.HoldReaderScratch(path)
	if err != nil {
		v.ixErr = indexUnusable("its working copy could not be made", err)
		return nil
	}
	var bad error
	version, rows := s.IndexMeta()
	ix, err := index.OpenScratch(path, s.WriteIndexImage, rows, version, func(err error) { bad = err })
	if err == nil {
		_, err = ix.Folders() // load it now, while the key is held
	}
	if err == nil && bad != nil {
		err = bad
	}
	if err != nil {
		if ix != nil {
			ix.Close()
		}
		hold.Close()
		if lerr := lockedErr(err); errors.Is(lerr, server.ErrLocked) {
			return lerr
		}
		v.ixErr = indexWhy(err)
		return nil
	}
	v.ix, v.hold = ix, hold
	return nil
}

// close drops the view's index, its scratch file and its pages once the requests
// using them end.
func (v *readView) close() {
	v.ixMu.Lock()
	defer v.ixMu.Unlock()
	v.closed = true
	if v.ix != nil {
		v.ix.Close() // removes a scratch index's file
		v.ix = nil
	}
	if v.hold != nil {
		v.hold.Close()
		v.hold = nil
	}
	v.pages = nil
}

// withIndex runs f on the current view's index, looking once more if a reload
// closed the view it got.
func (r *Reader) withIndex(f func(v *readView) error) error {
	for attempt := 0; ; attempt++ {
		v, err := r.ensure()
		if err != nil {
			return err
		}
		v.ixMu.RLock()
		if !v.closed {
			defer v.ixMu.RUnlock()
			return f(v)
		}
		v.ixMu.RUnlock()
		if attempt > 0 || r.isLocked() {
			return server.ErrLocked
		}
	}
}

// UseIndex implements server.Source.
func (r *Reader) UseIndex(f func(*index.Index) error) error {
	return r.withIndex(func(v *readView) error {
		if v.ixErr != nil {
			return v.ixErr
		}
		return f(v.ix)
	})
}

// GoBack implements server.Source: the decrypted manifest and history; a record's
// message is present when it has a page unit — an encrypted archive's messages are
// never removed by deleting a file (the owner's ruling, 2026-10-26). A history chunk
// missing while the record held still is a partial timeline, never an error (§7.3).
func (r *Reader) GoBack() (server.GoBack, error) {
	for attempt := 0; ; attempt++ {
		v, err := r.ensure()
		if err != nil {
			return server.GoBack{}, err
		}
		events, err := r.history(v)
		missing := errors.Is(err, fs.ErrNotExist)
		if missing && attempt == 0 && r.moved(v) {
			continue
		}
		if missing {
			events, err = v.store.History(), nil // the gone chunk stands as a barrier
		}
		if err != nil {
			return server.GoBack{}, err
		}
		g := server.GoBack{Events: events, Records: v.m, Encrypted: true, Present: sealedPresent}
		_, _, gap := v.store.HistoryGap()
		switch {
		case len(v.store.Chunks(vault.ContainerHistory)) == 0:
			g.Timeline = server.TimelineUnavailable
		case gap || hasBarrier(events):
			g.Timeline = server.TimelinePartial
		}
		return g, nil
	}
}

// sealedPresent is go-back's presence for an encrypted archive: a record with a
// page unit (a stub or a gap has none, and shows nowhere).
func sealedPresent(rec state.Record) bool { return rec.Sealed != nil && rec.Sealed.HTML != nil }

func (r *Reader) history(v *readView) ([]state.HistoryEvent, error) {
	if _, _, err := r.u.acquire(); err != nil {
		return nil, lockedErr(err)
	}
	defer r.u.release()
	events, err := v.store.HistoryChecked()
	return events, lockedErr(err)
}

func hasBarrier(events []state.HistoryEvent) bool {
	for _, e := range events {
		if e.Barrier {
			return true
		}
	}
	return false
}

// Files implements server.Source.
func (r *Reader) Files() http.Handler {
	return server.SealedFiles(r.open, r.LockedNote, r.o.WriteTimeout)
}

// open is the bytes of one archive-relative path: a rendered folder or root page,
// or a record's page, attachment zip or original, read whole within the memory
// budget (§7.4). A unit gone is looked for once more when the record moved since
// the view was loaded (§7.3); a unit whose bytes are not its record's is damage,
// never a reason to load again.
func (r *Reader) open(req *http.Request, rel string) ([]byte, func(), error) {
	for attempt := 0; ; attempt++ {
		v, err := r.ensure()
		if err != nil {
			return nil, nil, err
		}
		if rel == "README.txt" {
			// The note as this program writes it, never the file beside the archive,
			// which anyone with the drive could have changed (phase 3 §13.1).
			return []byte(app.EncryptedNote()), func() {}, nil
		}
		if page, ok, err := r.page(v, rel); ok || err != nil {
			return page, func() {}, err
		}
		unit, kind, sealed, ok := lookupUnit(v.m, rel)
		if !ok {
			return nil, nil, fs.ErrNotExist
		}
		release, err := r.o.Guard.Reserve(req.Context(), 2*sealed.Size)
		if errors.Is(err, memguard.ErrTooLarge) {
			// Named by the message's own size, not the reservation's (§7.4).
			return nil, nil, fmt.Errorf("%w (%d MB)", memguard.ErrTooLarge, (sealed.Size+(1<<20)-1)>>20)
		}
		if err != nil {
			return nil, nil, err
		}
		readerBefore()
		data, err := r.readUnit(unit, kind, sealed)
		if err == nil {
			return data, release, nil
		}
		release()
		if attempt == 0 && errors.Is(err, fs.ErrNotExist) && r.moved(v) {
			continue
		}
		return nil, nil, err
	}
}

// page is a rendered folder or root page, rendered once per view. With no usable
// index there are none to render: the request is told why, never as damage to a
// message.
func (r *Reader) page(v *readView, rel string) ([]byte, bool, error) {
	if rel != "index.html" && !isFolderPage(rel) {
		return nil, false, nil
	}
	var page []byte
	var ok bool
	err := r.withIndex(func(cur *readView) error {
		cur.pagesOnce.Do(func() {
			if cur.ixErr != nil {
				cur.pagesErr = fmt.Errorf("%w — the folder pages are built from the search index; each message still opens by its own link", cur.ixErr)
				return
			}
			cur.pages, cur.pagesErr = pages.Render(cur.ix, cur.gaps)
		})
		if cur.pagesErr != nil {
			return cur.pagesErr
		}
		page, ok = cur.pages[rel]
		return nil
	})
	if err != nil {
		return nil, true, err
	}
	if !ok {
		return nil, true, fs.ErrNotExist
	}
	return page, true, nil
}

// isFolderPage reports a folder's index.html or index-N.html.
func isFolderPage(rel string) bool {
	i := strings.LastIndexByte(rel, '/')
	base := rel[i+1:]
	if base == "index.html" {
		return true
	}
	n := strings.TrimSuffix(strings.TrimPrefix(base, "index-"), ".html")
	return n != base && n != "" && strings.Trim(n, "0123456789") == ""
}

// lookupUnit maps a request to the record and unit it names: a page by its own
// path, an attachment zip or an original by the page path they sit beside.
func lookupUnit(m *state.Manifest, rel string) (string, vault.UnitKind, state.SealedUnit, bool) {
	page, kind := rel, vault.UnitMessage
	switch {
	case strings.HasSuffix(rel, "-attachments.zip"):
		page, kind = strings.TrimSuffix(rel, "-attachments.zip")+".html", vault.UnitAttachment
	case strings.HasSuffix(rel, ".eml"):
		page, kind = strings.TrimSuffix(rel, ".eml")+".html", vault.UnitOriginal
	case !strings.HasSuffix(rel, ".html"):
		return "", 0, state.SealedUnit{}, false
	}
	key, ok := m.KeyForPath(page)
	if !ok {
		return "", 0, state.SealedUnit{}, false
	}
	rec, ok := m.Get(key)
	if !ok || rec.Sealed == nil {
		return "", 0, state.SealedUnit{}, false
	}
	var s *state.SealedUnit
	switch kind {
	case vault.UnitMessage:
		s = rec.Sealed.HTML
	case vault.UnitAttachment:
		s = rec.Sealed.Zip
	case vault.UnitOriginal:
		s = rec.Sealed.EML
	}
	if s == nil {
		return "", 0, state.SealedUnit{}, false
	}
	return rec.Unit, kind, *s, true
}

// readUnit reads a unit whole, holding the key only to name it and to open it: the
// ciphertext is read from disk without it (§3.2).
func (r *Reader) readUnit(unit string, kind vault.UnitKind, s state.SealedUnit) ([]byte, error) {
	k, _, err := r.u.acquire()
	if err != nil {
		return nil, lockedErr(err)
	}
	units := &export.Units{Root: r.out, Key: k}
	rel, err := units.Locate(unit, kind, s)
	r.u.release()
	if err != nil {
		return nil, lockedErr(err)
	}
	sealed, err := export.ReadSealedAt(r.out, rel, s)
	if err != nil {
		return nil, err
	}
	readerBetween()
	if k, _, err = r.u.acquire(); err != nil {
		return nil, lockedErr(err)
	}
	defer r.u.release()
	plain, err := (&export.Units{Root: r.out, Key: k}).Open(unit, kind, s, sealed)
	return plain, lockedErr(err)
}
