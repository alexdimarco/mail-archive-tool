package keyring

import (
	"errors"
	"fmt"
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
	// WriteTimeout bounds each file response's write, so a client that stops
	// reading cannot hold the memory budget past it (§3.2). Zero: five minutes.
	WriteTimeout time.Duration
	// Guard is the memory guard unit reads share (§3.7). Nil: the process's.
	Guard *memguard.Guard
}

// Reader is an encrypted archive's server.Source. Lock ends it.
type Reader struct {
	u   *Unlocked
	out string
	o   ReaderOptions

	mu       sync.Mutex
	view     *readView
	peekedAt time.Time
	stale    bool // a request missed something: load again, peek or not
	locked   bool
}

// readView is one loaded generation of the sealed record.
type readView struct {
	peek  container.Peeked
	m     *state.Manifest
	store *container.Store
	gaps  int

	ixMu   sync.RWMutex
	ix     *index.Index
	ixErr  error // the index cannot be used: none yet, or damaged
	behind bool  // the record says the index is behind the archive
	closed bool

	pagesOnce sync.Once
	pages     map[string][]byte
	pagesErr  error
}

// Seams for tests: the clock freshness is judged by, a hook between a load's two
// peeks (a test commits there), and one between a request's view and its unit read
// (a test locks there).
var (
	readerNow    = time.Now
	readerLoad   = func() {}
	readerBefore = func() {}
)

// readerPeekEvery is how often a reader looks for a backup's commit (§7.3).
const readerPeekEvery = 2 * time.Second

// readerIndexBudget is the largest index a reader loads into memory (§7.4).
var readerIndexBudget int64 = 256 << 20

// readerLoads numbers this process's scratch index loads, so no two share a file.
var readerLoads atomic.Uint64

// errBusy refuses a load that two commits in a row overtook (§7.3).
var errBusy = errors.New("a backup is running on this archive; try again in a moment")

// errNoIndex is an encrypted archive with no search index yet.
var errNoIndex = errors.New("this encrypted archive has no search index yet — `mailarchive reindex -rebuild` builds one")

// ReaderSource is the reader of the encrypted archive u opened (§3.1, §7.2). It
// loads the record now, so a refusal is told before anything is served.
func ReaderSource(u *Unlocked, o ReaderOptions) (*Reader, error) {
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 5 * time.Minute
	}
	if o.Guard == nil {
		o.Guard = memguard.Process
	}
	r := &Reader{u: u, out: u.Out(), o: o}
	app.SweepReaderScratch(r.out, u.ArchiveID())
	if _, err := r.ensure(); err != nil {
		return nil, err
	}
	return r, nil
}

// Lock ends the reader: every request from now gets the locked page; the index and
// rendered pages are dropped once the requests using them end. It never waits.
func (r *Reader) Lock() {
	r.mu.Lock()
	r.locked = true
	v := r.view
	r.view = nil
	r.mu.Unlock()
	if v != nil {
		go v.close()
	}
}

// LockedNote implements server.Source.
func (r *Reader) LockedNote() string {
	if r.isLocked() {
		return r.o.LockedNote
	}
	return ""
}

func (r *Reader) isLocked() bool {
	r.mu.Lock()
	locked := r.locked
	r.mu.Unlock()
	return locked || r.u.isLocked()
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

// ensure is the current view: reloaded when a peek, at most every two seconds,
// shows a backup committed since it was loaded (§7.3).
func (r *Reader) ensure() (*readView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locked || r.u.isLocked() {
		return nil, server.ErrLocked
	}
	now := readerNow()
	if r.view != nil && !r.stale && now.Sub(r.peekedAt) < readerPeekEvery {
		return r.view, nil
	}
	if r.view != nil && !r.stale {
		if p, err := container.PeekFile(r.out); err == nil && p == r.view.peek {
			r.peekedAt = now
			return r.view, nil
		}
	}
	r.stale = false
	v, err := r.load()
	if err != nil {
		return nil, err
	}
	if old := r.view; old != nil {
		go old.close()
	}
	r.view, r.peekedAt = v, now
	return v, nil
}

// reload makes the next ensure load the record again: a unit or a chunk a request
// missed is looked for once more in what is current (§7.3).
func (r *Reader) reload(seen *readView) {
	r.mu.Lock()
	if r.view == seen {
		r.stale = true
	}
	r.mu.Unlock()
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
	m, s, err := container.LoadManifest(r.out, container.LoadOptions{Key: k, ReadOnly: true})
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
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err // a chunk a backup's sweep removed: load() reloads
		}
		if err != nil {
			if err := lockedErr(err); errors.Is(err, server.ErrLocked) {
				return nil, err
			}
			v.ixErr = fmt.Errorf("search is unavailable: the search index is damaged — `mailarchive reindex -rebuild` rebuilds it (%w)", err)
			break
		}
		if v.ix, err = index.OpenMemory(img); err != nil {
			v.ixErr = fmt.Errorf("search is unavailable: the search index is damaged — `mailarchive reindex -rebuild` rebuilds it (%w)", err)
		}
	}
	return v, nil
}

// scratchIndex loads the index into the reader's scratch file now, under the key
// the load holds: a damaged image leaves search unavailable, a chunk gone or a lock
// is the load's error.
func (r *Reader) scratchIndex(v *readView, s *container.Store) error {
	path, err := app.ReaderScratch(r.out, r.u.ArchiveID(), readerLoads.Add(1))
	if err != nil {
		v.ixErr = fmt.Errorf("search is unavailable: no working folder for the search index (%w)", err)
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
		if errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if lerr := lockedErr(err); errors.Is(lerr, server.ErrLocked) {
			return lerr
		}
		v.ixErr = fmt.Errorf("search is unavailable: the search index is damaged — `mailarchive reindex -rebuild` rebuilds it (%w)", err)
		return nil
	}
	v.ix = ix
	return nil
}

// close drops the view's index and pages once the requests using them end.
func (v *readView) close() {
	v.ixMu.Lock()
	defer v.ixMu.Unlock()
	v.closed = true
	if v.ix != nil {
		v.ix.Close()
		v.ix = nil
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
// never removed by deleting a file (the owner's ruling, 2026-10-26).
func (r *Reader) GoBack() (server.GoBack, error) {
	for attempt := 0; ; attempt++ {
		v, err := r.ensure()
		if err != nil {
			return server.GoBack{}, err
		}
		events, err := r.history(v)
		if errors.Is(err, fs.ErrNotExist) && attempt == 0 {
			r.reload(v)
			continue
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
// budget (§7.4). A unit missed is looked for once more in what is current (§7.3).
func (r *Reader) open(req *http.Request, rel string) ([]byte, func(), error) {
	for attempt := 0; ; attempt++ {
		v, err := r.ensure()
		if err != nil {
			return nil, nil, err
		}
		if page, ok, err := r.page(v, rel); ok || err != nil {
			return page, func() {}, err
		}
		unit, kind, sealed, ok := lookupUnit(v.m, rel)
		if !ok {
			return nil, nil, fs.ErrNotExist
		}
		release, err := r.o.Guard.Reserve(req.Context(), 2*sealed.Size)
		if err != nil {
			return nil, nil, err
		}
		readerBefore()
		data, err := r.readUnit(unit, kind, sealed)
		if err == nil {
			return data, release, nil
		}
		release()
		if attempt == 0 && (errors.Is(err, fs.ErrNotExist) || errors.Is(err, export.ErrUnitStore)) {
			r.reload(v)
			continue
		}
		return nil, nil, err
	}
}

// page is a rendered folder or root page, rendered once per view.
func (r *Reader) page(v *readView, rel string) ([]byte, bool, error) {
	if rel != "index.html" && !isFolderPage(rel) {
		return nil, false, nil
	}
	var page []byte
	var ok bool
	err := r.withIndex(func(cur *readView) error {
		if cur != v {
			v = cur
		}
		v.pagesOnce.Do(func() {
			if v.ixErr != nil {
				v.pagesErr = v.ixErr
				return
			}
			v.pages, v.pagesErr = pages.Render(v.ix, v.gaps)
		})
		if v.pagesErr != nil {
			return v.pagesErr
		}
		page, ok = v.pages[rel]
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
	if k, _, err = r.u.acquire(); err != nil {
		return nil, lockedErr(err)
	}
	defer r.u.release()
	plain, err := (&export.Units{Root: r.out, Key: k}).Open(unit, kind, s, sealed)
	return plain, lockedErr(err)
}
