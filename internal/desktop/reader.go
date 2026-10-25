package desktop

import (
	"net/http"
	"path/filepath"
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
	if rm.h == nil || out != rm.curOut {
		if rm.ix != nil {
			rm.ix.Close()
			rm.ix, rm.h = nil, nil
		}
		rm.curOut = out
		// The reader's unlock arrives in Phase 3; until then an encrypted archive is
		// refused here, never served as missing files (phase2a §5).
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
