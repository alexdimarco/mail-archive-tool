package desktop

import (
	"net/http"
	"path/filepath"
	"sync"

	"mail-archive-tool/internal/index"
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
