package tbrestore

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// FS is everything the writer and the cleanup do to Local Folders, each through the
// *os.Root opened on it, so no name, link or ".." can reach outside it (design §9.2).
// Every name is created exclusively; nothing existing is opened for writing. rootFS is
// the one production implementation; a test wraps it to inject a fault at any step (the
// ceFS seam's shape, internal/app/copyexport_fs.go).
type FS interface {
	// Mkdir creates one folder, exclusively, at 0700: an existing entry of the name is
	// an error satisfying errors.Is(err, fs.ErrExist).
	Mkdir(name string) error
	// Create creates one file, exclusively, at 0600, for writing.
	Create(name string) (wfile, error)
	// Open opens a regular file for reading; a link or anything else is refused.
	Open(name string) (io.ReadCloser, error)
	Lstat(name string) (fs.FileInfo, error)
	// ReadDir gives a folder's entries in batches; fn returning false stops the read.
	ReadDir(name string, fn func([]fs.DirEntry) bool) error
	// RemoveSeen removes one file or one empty folder, and only the very entry the
	// cleanup's scan saw (ADV-02, settled 2026-11-07): it walks from the root one folder
	// at a time by handle, following no link, and requires each folder on the way and
	// the entry itself to be what seen recorded for its path (keys NFC, "/"-joined: the
	// same identity and kind, never a link, junction or reparse point); then it removes
	// the entry relative to its checked parent, never through a link. Any difference is
	// errChanged and nothing is removed; an entry already gone satisfies
	// errors.Is(err, fs.ErrNotExist).
	RemoveSeen(name string, seen map[string]fs.FileInfo) error
	// SyncDir flushes a folder's entries (a no-op where the OS has no such call).
	SyncDir(name string) error
}

type wfile interface {
	io.Writer
	Sync() error
	Close() error
}

type rootFS struct{ root *os.Root }

// NewRootFS is the writer's file system on a root opened on Local Folders.
func NewRootFS(root *os.Root) FS { return &rootFS{root: root} }

func newRootFS(root *os.Root) FS { return NewRootFS(root) }

func (r *rootFS) Mkdir(name string) error { return r.root.Mkdir(name, 0o700) }

func (r *rootFS) Create(name string) (wfile, error) {
	return r.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

func (r *rootFS) Open(name string) (io.ReadCloser, error) {
	before, err := r.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not an ordinary file", name)
	}
	f, err := r.root.Open(name)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(before, fi) {
		f.Close()
		return nil, fmt.Errorf("%s changed while it was opened", name)
	}
	return f, nil
}

func (r *rootFS) Lstat(name string) (fs.FileInfo, error) { return r.root.Lstat(name) }

func (r *rootFS) ReadDir(name string, fn func([]fs.DirEntry) bool) error {
	if fi, err := r.root.Lstat(name); err != nil {
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", name)
	}
	f, err := r.root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		batch, err := f.ReadDir(256)
		if len(batch) > 0 && !fn(batch) {
			return nil
		}
		if errors.Is(err, io.EOF) || (err == nil && len(batch) == 0) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (r *rootFS) RemoveSeen(name string, seen map[string]fs.FileInfo) error {
	parts := strings.Split(name, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return errChanged
		}
	}
	want := make([]fs.FileInfo, len(parts))
	for i := range parts {
		fi, ok := seen[norm.NFC.String(strings.Join(parts[:i+1], "/"))]
		if !ok || fi.Mode()&fs.ModeSymlink != 0 || (i < len(parts)-1 && !fi.IsDir()) {
			if _, err := r.root.Lstat(name); errors.Is(err, fs.ErrNotExist) {
				return err // gone: nothing to remove, whatever the scan saw
			}
			return errChanged
		}
		want[i] = fi
	}
	top, err := r.root.Open(".")
	if err != nil {
		return err
	}
	defer top.Close()
	return removeSeenAt(top, parts, want)
}

// errChanged is an entry, or a folder on the way to it, that is not what the cleanup's
// scan saw: removal stops there (W-LEFT's step-4 form).
var errChanged = errors.New("it changed since MailArchive looked at it")

// beforeUnlinkSeam runs between removeSeenAt's last check of an entry and its removal;
// a test swaps the entry there (ADV-02's narrowest window). It does nothing in
// production.
var beforeUnlinkSeam = func(name string) {}

// sameEntry reports a fresh look at an entry is the scan's: the same file, the same kind,
// not a link.
func sameEntry(want, got fs.FileInfo) bool {
	if got.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 || got.IsDir() != want.IsDir() || got.Mode().IsRegular() != want.Mode().IsRegular() {
		return false
	}
	return os.SameFile(want, got)
}

func (r *rootFS) SyncDir(name string) error {
	f, err := r.root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return syncDirFile(f)
}

// retryFor is how long a Windows sharing violation (antivirus scanning a new file) is
// retried on the post-write check's reopen and on a cleanup's removal (§8 foldcheck
// note, rule 5); a test shortens it.
var retryFor = 5 * time.Second

// withRetry runs op, retrying while it fails with a sharing violation, for retryFor.
func withRetry(op func() error) error {
	deadline := time.Now().Add(retryFor)
	for {
		err := op()
		if err == nil || !isSharingViolation(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// OpenLocalRoot opens Local Folders as the writer's root and checks it is still the
// folder the plan pinned (§5.3: identity, read from the open handle), so a folder
// swapped since the plan is refused.
func OpenLocalRoot(lf LocalFolders) (*os.Root, error) {
	root, err := os.OpenRoot(lf.Dir)
	if err != nil {
		return nil, refuse("W-LOCALGONE", fill(wLocalGone, "PATH", lf.Dir), err)
	}
	f, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, refuse("W-LOCALGONE", fill(wLocalGone, "PATH", lf.Dir), err)
	}
	id, err := openIdentity(f)
	f.Close()
	if err != nil || !id.Same(lf.Identity) {
		root.Close()
		return nil, refuse("W-CHANGED", fill(wChanged, "WHAT", "Thunderbird's Local Folders ("+lf.Dir+") is not the folder it was"), err)
	}
	return root, nil
}

const wChanged = "This changed since you clicked Check: WHAT. Click Check again."
