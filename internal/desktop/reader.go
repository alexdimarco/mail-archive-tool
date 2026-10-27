package desktop

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/server"
)

// readerMux serves the archive reader (search UI + go-back + files) for the
// dashboard's CURRENT archive location, reusing server.New unchanged (R19). It
// opens the index lazily and reopens it when the archive location changes or once
// a first capture has created it, so "Open archive" works without a restart. It
// runs on its own loopback port (the reader's links are root-relative).
type readerMux struct {
	cfg    Config
	mu     sync.Mutex
	curOut string
	h      http.Handler
	ix     *index.Index
}

// ReaderHandler returns the archive-reader handler for cfg's current archive.
func ReaderHandler(cfg Config) http.Handler { return &readerMux{cfg: cfg} }

func (rm *readerMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out := rm.cfg.effectiveOut()
	rm.mu.Lock()
	if out == "" {
		rm.mu.Unlock()
		http.Error(w, "No archive location set yet — open MailArchive Desktop and choose one.", http.StatusServiceUnavailable)
		return
	}
	// An encrypted archive is read through the Desktop's own open of it, never by
	// asking for a secret here: the reader has no form and no POST route (§7.1; the
	// P3-3 plan, 3c). Its archive at its own folder only — a folder inside one, or a
	// recorded folder that no longer holds it, is the keyless refusal.
	if rm.cfg.Keys != nil && app.ArchiveEncrypted(out) && app.UnlockableAt(out) {
		if rm.ix != nil {
			rm.ix.Close()
			rm.ix, rm.h = nil, nil
		}
		rm.curOut = ""
		rm.mu.Unlock()
		rm.serveSealed(w, r, out)
		return
	}
	if rm.h == nil || out != rm.curOut {
		if rm.ix != nil {
			rm.ix.Close()
			rm.ix, rm.h = nil, nil
		}
		rm.curOut = out
		// An encrypted archive the Desktop cannot open here is refused, never served
		// as missing files (phase2a §5).
		if kerr := app.RefuseKeyless("open it in the reader", out); kerr != nil {
			rm.mu.Unlock()
			http.Error(w, kerr.Error(), http.StatusServiceUnavailable)
			return
		}
		ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
		if err != nil {
			rm.mu.Unlock()
			http.Error(w, "No archive yet — run a capture first, then reopen the archive.", http.StatusServiceUnavailable)
			return
		}
		rm.ix, rm.h = ix, server.New(out, ix)
	}
	h := rm.h
	rm.mu.Unlock()
	h.ServeHTTP(w, r)
}

// serveSealed serves the encrypted archive at out through its open in the Desktop's
// registry — opened by itself from a remembered key (O3) — or, locked, the page that
// says how to unlock it (§7.1).
func (rm *readerMux) serveSealed(w http.ResponseWriter, r *http.Request, out string) {
	// Reading mail is using the Desktop (K4): a page or a message, never the
	// reader's own script fetches.
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		rm.cfg.Keys.touch()
	}
	src, err := rm.cfg.Keys.readerFor(out)
	switch {
	case err != nil:
		http.Error(w, "This encrypted archive could not be opened to read: "+err.Error(), http.StatusServiceUnavailable)
	case src == nil:
		server.Serve(lockedSource{note: rm.cfg.Keys.note}).ServeHTTP(w, r)
	default:
		server.Serve(src).ServeHTTP(w, r)
	}
}

// lockedSource is a locked encrypted archive: every page is the locked page, and
// the reader's API answers that it is locked.
type lockedSource struct{ note string }

func (s lockedSource) UseIndex(func(*index.Index) error) error { return server.ErrLocked }
func (s lockedSource) GoBack() (server.GoBack, error)          { return server.GoBack{}, server.ErrLocked }
func (s lockedSource) LockedNote() string                      { return s.note }
func (s lockedSource) Files() http.Handler {
	return server.SealedFiles(func(*http.Request, string) ([]byte, func(), error) { return nil, nil, server.ErrLocked },
		func() string { return s.note }, 0)
}

// Routes names the reader's routes. The reader always serves through a
// server.New handler, whichever archive it is pointed at, so its routes are
// exactly the ones server.New registers (design-local-confidentiality CC15).
func (rm *readerMux) Routes() []string { return localweb.RoutesOf(server.New("", nil)) }

// Route returns the pattern r reaches on the reader's server.New handler.
func (rm *readerMux) Route(r *http.Request) string {
	return localweb.RouteOf(server.New("", nil), r)
}

// Close releases the lazily-opened index, if any. It is safe to call more than
// once and concurrently with ServeHTTP. The desktop process normally holds the
// reader open for its lifetime; Close matters for an orderly shutdown and for
// tests that must release search.db before their tempdir is removed (on Windows
// an open file cannot be deleted).
func (rm *readerMux) Close() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.ix == nil {
		return nil
	}
	err := rm.ix.Close()
	rm.ix, rm.h = nil, nil
	return err
}
