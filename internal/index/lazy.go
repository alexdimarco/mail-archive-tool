package index

import "fmt"

// An encrypted archive's sealed index is loaded only when a run first needs it
// (design phase2b §6, P2b-C15): a scheduled run that finds nothing new neither
// decrypts nor restores it. Until then the index answers only what its sealed record
// already says — its row count and format version — and every other use loads it.

// lazyLoad is how a lazily opened index loads, and what is known before it does.
type lazyLoad struct {
	load  func() ([]byte, error) // the decrypted image
	onBad func(error)            // told when the image cannot be loaded; an empty index is used instead
	rows  int                    // the row count the record holds for the index
}

// OpenLazy is an in-memory index that loads its image with load on first use; rows
// and version are what the record says of it. A version a newer MailArchive wrote is
// refused here, as OpenMemory refuses its image. An image that cannot be loaded is
// reported to onBad and replaced by an empty index: a backup never stops for its
// index (P2b-C16).
func OpenLazy(load func() ([]byte, error), rows, version int, onBad func(error)) (*Index, error) {
	if version > indexVersion {
		return nil, fmt.Errorf("the sealed search index was %w (format version %d; this build understands %d): upgrade this copy of mailarchive", ErrNewerFormat, version, indexVersion)
	}
	return &Index{lazy: &lazyLoad{load: load, onBad: onBad, rows: rows}, version: version, memory: true}, nil
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
	img, err := ix.lazy.load()
	var m *Index
	if err == nil {
		m, err = OpenMemory(img)
	}
	if err != nil {
		if ix.lazy.onBad != nil {
			ix.lazy.onBad(err)
		}
		if m, err = OpenMemory(nil); err != nil {
			return err
		}
	}
	if err := m.db.QueryRow(`SELECT total_changes()`).Scan(&ix.base); err != nil {
		m.db.Close()
		return err
	}
	ix.db, ix.version, ix.loaded = m.db, m.version, true
	return nil
}
