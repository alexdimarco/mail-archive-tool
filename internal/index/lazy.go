package index

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// An encrypted archive's sealed index is loaded only when a run first needs it
// (design phase2b §6, P2b-C15): a scheduled run that finds nothing new neither
// decrypts nor restores it. Until then the index answers only what its sealed record
// already says — its row count and format version — and every other use loads it.
//
// It is loaded into memory, or — above the memory budget — into a scratch file in
// MailArchive's per-user cache folder (OpenScratch), worked on with the same pragmas
// and removed when the index is closed.

// lazyLoad is how a lazily opened index loads, and what is known before it does.
type lazyLoad struct {
	load  func() ([]byte, error)    // the decrypted image, into memory
	write func(w io.Writer) error   // or: the decrypted image, streamed (nil: a new index)
	path  string                    // the scratch file, when the index is worked on in one
	onBad func(error)               // told when the image cannot be loaded; an empty index is used instead
	rows  int                       // the row count the record holds for the index
}

// OpenLazy is an in-memory index that loads its image with load on first use; rows
// and version are what the record says of it. A version a newer MailArchive wrote is
// refused here, as OpenMemory refuses its image. An image that cannot be loaded is
// reported to onBad and replaced by an empty index: a backup never stops for its
// index (P2b-C16).
func OpenLazy(load func() ([]byte, error), rows, version int, onBad func(error)) (*Index, error) {
	return openLazy(&lazyLoad{load: load, onBad: onBad, rows: rows}, version)
}

// OpenScratch is OpenLazy for an index worked on in the scratch file at path: on
// first use write streams the decrypted image into it (nil: a new, empty index),
// and Close removes it. Whatever is at path before the load is replaced.
func OpenScratch(path string, write func(w io.Writer) error, rows, version int, onBad func(error)) (*Index, error) {
	return openLazy(&lazyLoad{write: write, path: path, onBad: onBad, rows: rows}, version)
}

func openLazy(l *lazyLoad, version int) (*Index, error) {
	if version > indexVersion {
		return nil, fmt.Errorf("the sealed search index was %w (format version %d; this build understands %d): upgrade this copy of mailarchive", ErrNewerFormat, version, indexVersion)
	}
	return &Index{lazy: l, version: version, memory: true}, nil
}

// Version is the index's key-format version.
func (ix *Index) Version() int { return ix.version }

// idle reports whether the index is lazy and not yet loaded.
func (ix *Index) idle() bool { return ix.lazy != nil && !ix.loaded }

// ensure loads a lazy index if it is not loaded yet.
func (ix *Index) ensure() error {
	if !ix.idle() {
		return nil
	}
	open := ix.openMemoryImage
	if ix.lazy.path != "" {
		open = ix.openScratchImage
	}
	m, err := open(true)
	if err != nil {
		if ix.lazy.onBad != nil {
			ix.lazy.onBad(err)
		}
		if m, err = open(false); err != nil {
			return err
		}
	}
	if err := m.db.QueryRow(`SELECT total_changes()`).Scan(&ix.base); err != nil {
		m.close()
		return err
	}
	ix.db, ix.version, ix.scratch, ix.loaded = m.db, m.version, m.scratch, true
	return nil
}

// openMemoryImage opens the record's image in memory, or a new empty index.
func (ix *Index) openMemoryImage(withImage bool) (*Index, error) {
	if !withImage {
		return OpenMemory(nil)
	}
	img, err := ix.lazy.load()
	if err != nil {
		return nil, err
	}
	return OpenMemory(img)
}

// openScratchImage writes the record's image (or nothing, for an empty index) into
// the scratch file and opens it there. Only a rollback-journal database is opened: a
// write-ahead-log one would make SQLite create its companion files beside it.
func (ix *Index) openScratchImage(withImage bool) (m *Index, err error) {
	path := ix.lazy.path
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("index scratch: %w", err)
	}
	defer func() {
		if err != nil {
			os.Remove(path) // nothing of a failed load is left in the scratch folder
		}
	}()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("index scratch: %w", err)
	}
	if withImage && ix.lazy.write != nil {
		err = ix.lazy.write(f)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = checkRollbackImage(path)
	}
	if err != nil {
		return nil, err
	}
	db, err := openOne(path)
	if err != nil {
		return nil, err
	}
	if m, err = begin(db); err != nil {
		return nil, err
	}
	if m.scratch, err = os.Open(path); err != nil {
		m.db.Close()
		return nil, fmt.Errorf("index scratch: %w", err)
	}
	return m, nil
}

// checkRollbackImage refuses a scratch file that holds something other than nothing
// or a rollback-journal SQLite database (ErrImage).
func checkRollbackImage(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("index scratch: %w", err)
	}
	defer f.Close()
	head := make([]byte, 20)
	n, err := io.ReadFull(f, head)
	switch {
	case n == 0 && errors.Is(err, io.EOF):
		return nil // empty: a new index
	case err != nil:
		return fmt.Errorf("%w: %d bytes", ErrImage, n)
	case !bytes.HasPrefix(head, []byte("SQLite format 3\x00")) || head[18] != 1 || head[19] != 1:
		return fmt.Errorf("%w: not a rollback-journal database", ErrImage)
	}
	return nil
}

// close closes a loaded index's database, and removes its scratch file.
func (ix *Index) close() error {
	err := ix.db.Close()
	if ix.scratch != nil {
		ix.scratch.Close()
		if rerr := os.Remove(ix.scratch.Name()); err == nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = rerr
		}
	}
	return err
}

// Capture commits any pending batch and returns the index's whole database to read
// — the image the record seals — and its size: the serialized image for an index in
// memory, the scratch file itself for one in a scratch file, read a chunk at a time.
func (ix *Index) Capture() (io.ReaderAt, int64, error) {
	if !ix.memory {
		return nil, 0, errors.New("index: only an encrypted archive's index is captured")
	}
	if err := ix.ensure(); err != nil {
		return nil, 0, err
	}
	if ix.scratch == nil {
		img, err := ix.Image()
		if err != nil {
			return nil, 0, err
		}
		return bytes.NewReader(img), int64(len(img)), nil
	}
	if err := ix.commitPending(); err != nil {
		return nil, 0, err
	}
	fi, err := ix.scratch.Stat()
	if err != nil {
		return nil, 0, err
	}
	return ix.scratch, fi.Size(), nil
}
