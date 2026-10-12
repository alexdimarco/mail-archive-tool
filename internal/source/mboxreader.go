package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// mboxReader reads Thunderbird / mbox mail stores. It implements Source and
// FaultWalker.
type mboxReader struct {
	root   string // a mail directory (store) or a single mbox file
	isDir  bool
	store  string
	follow bool // the input's directory-link opt-in (OpenOptions.FollowLinks)
}

func openMboxDir(dir string, opts OpenOptions) (*mboxReader, error) {
	clean := strings.TrimRight(dir, string(os.PathSeparator))
	return &mboxReader{root: dir, isDir: true, store: filepath.Base(clean), follow: opts.FollowLinks}, nil
}

func openMboxFile(path string, opts OpenOptions) (*mboxReader, error) {
	base := filepath.Base(path)
	return &mboxReader{root: path, isDir: false, store: strings.TrimSuffix(base, filepath.Ext(base)), follow: opts.FollowLinks}, nil
}

func (r *mboxReader) StoreName() string { return r.store }
func (r *mboxReader) Close() error      { return nil }

// Walk visits every message. A caller that wants the items and folders it could not
// read as they happen uses WalkWithFaults; Walk returns them as a *FaultsError, so a
// caller that ignores them still sees a non-nil error (FC29).
func (r *mboxReader) Walk(handler MessageHandler) error {
	return r.WalkWithFaults(handler, WalkOptions{})
}

// WalkWithFaults is Walk that delivers each fault through opts.Faults as it happens
// (the loops are in localwalk.go).
func (r *mboxReader) WalkWithFaults(handler MessageHandler, opts WalkOptions) error {
	w := newLocalWalk(handler, opts, r.follow)
	return w.sink.result(r.walk(w))
}

func (r *mboxReader) walk(w *localWalk) error {
	if !r.isDir {
		// A single mbox file is its own store; messages go directly under it (no
		// redundant <name>/<name> nesting). It is opened through the root of the
		// directory that holds it, once resolved.
		real, err := filepath.EvalSymlinks(r.root)
		if err != nil {
			return err
		}
		sc, err := openScope(filepath.Dir(real))
		if err != nil {
			return err
		}
		defer sc.close()
		return w.mboxFile(sc, filepath.Base(real), nil)
	}
	sc, err := openScope(r.root)
	if err != nil {
		return fmt.Errorf("read mail dir %s: %w", r.root, err)
	}
	defer sc.close()
	if w.follow {
		if fi, err := os.Stat(sc.path); err == nil {
			w.seen(fi) // the root is read; a link back to it is not read again
		}
	}
	// A directory that is itself a maildir folder is a single store.
	if isMaildirIn(sc, ".") {
		if err := w.maildir(sc, ".", nil); err != nil {
			return err
		}
	} else if err := w.mboxDir(sc, ".", nil, true); err != nil {
		return err
	}
	return w.drain()
}
