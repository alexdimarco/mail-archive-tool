package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// sealedIndex is an encrypted archive's in-memory search index as a run holds it:
// the index, and whether it changed since the record last sealed it.
type sealedIndex struct {
	idx     *index.Index
	sealed  int64 // the change count at the last capture; -1: never captured
	damaged bool  // the sealed index proved damaged when loaded, and was replaced by an empty one
}

// register hands the store a capture of si's index that runs only when the change
// count moved since the last seal — counted as sealed only once its commit landed, so
// a commit that fails after the capture leaves it for the next one.
func (si *sealedIndex) register(store *container.Store) {
	store.SetIndex(func() (container.IndexCapture, bool, error) {
		n, err := si.idx.Changes()
		if err != nil {
			return container.IndexCapture{}, false, err
		}
		if n == si.sealed {
			return container.IndexCapture{}, false, nil
		}
		img, size, err := si.idx.Capture()
		if err != nil {
			return container.IndexCapture{}, false, err
		}
		rows, err := si.idx.Count()
		if err != nil {
			return container.IndexCapture{}, false, err
		}
		return container.IndexCapture{Image: img, Size: size, Version: si.idx.Version(), Rows: rows, Sealed: func() { si.sealed = n }}, true, nil
	})
}

// catchUp catches si's index up to manifest (catchUpIndex), and with it any damage
// already found: the index is whole again or still marked behind.
func (si *sealedIndex) catchUp(manifest *state.Manifest, units *export.Units, store *container.Store, logger *log.Logger) {
	if _, err := catchUpIndex(si.idx, manifest, units, store, logger); err != nil {
		logger.Printf("warning: %v", err)
	}
	si.damaged = false
}

// healLateDamage catches up a sealed index first loaded during the run's walk and
// found damaged then, and saves the record, so the backup still heals it in the same
// run (P2b-C16). Nothing to do for one found damaged before the walk: the run's
// start caught it up.
func healLateDamage(si *sealedIndex, store *container.Store, manifest *state.Manifest, units *export.Units, logger *log.Logger) error {
	if si == nil || !si.damaged {
		return nil
	}
	si.catchUp(manifest, units, store, logger)
	return manifest.Save()
}

// unsealed reports whether the index changed after the record last sealed it: a
// write after a run's final commit, which no commit would ever seal (P2b-C15).
func (si *sealedIndex) unsealed() bool {
	n, err := si.idx.Changes()
	return err != nil || n != si.sealed
}

// guardLateIndexWrite is the end-of-run check that the run's last commit sealed
// every index write: one that did not is marked behind and committed once more, so
// the mark is durable and the next keyed run catches the index up (§6).
func guardLateIndexWrite(si *sealedIndex, store *container.Store, manifest *state.Manifest, logger *log.Logger) error {
	if si == nil || !si.unsealed() {
		return nil
	}
	logger.Printf("warning: the search index changed after its last save; it is marked to be caught up")
	store.MarkIndexBehind()
	return manifest.Save()
}

// indexAdd adds one exported message to the run's index; a var so a test can fail it.
var indexAdd = (*index.Index).Add

// markSealedIndexBehind marks a sealed index behind when a message the run exported
// could not be added to it: only a catch-up adds it later, and only an index marked
// behind is caught up (P2b-C16). A plaintext archive (sealed nil) counts the failure
// in its result, as before.
func markSealedIndexBehind(sealed *container.Store) {
	if sealed != nil {
		sealed.MarkIndexBehind()
	}
}

// IndexCatchUp is what a catch-up or keyed rebuild did.
type IndexCatchUp struct {
	Added      int // records added to the index
	Unreadable int // records whose sealed units could not be read (left out, logged)
}

// catchUpIndex adds to idx every record of manifest it does not hold, deriving each
// message from its sealed units (the preserved original when there is one, else the
// page, with attachment names from the zip) — the encrypted archive's rebuild (§6,
// P2b-C16). A record that cannot be read is logged and left out, never removed from
// the record. When every record is either indexed or unreadable the index is no
// longer behind; the run's next commit seals it.
func catchUpIndex(idx *index.Index, manifest *state.Manifest, units *export.Units, store *container.Store, logger *log.Logger) (IndexCatchUp, error) {
	var rep IndexCatchUp
	have := map[string]bool{}
	if err := idx.EachRow(func(_ int64, key, _ string) error {
		have[key] = true
		return nil
	}); err != nil {
		return rep, err
	}
	records := manifest.All()
	keys := make([]string, 0, len(records))
	for k := range records {
		if !have[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lostBefore := idx.Lost()
	for _, key := range keys {
		rec := records[key]
		segs, ok := validRelPath(rec.Path)
		m := deriveSealedMessage(units, rec)
		if !ok || m == nil {
			logger.Printf("index catch-up: the message at %s could not be read; left out of the search index", units.Name(rec.Unit, vault.UnitMessage, revOf(rec.Sealed.HTML)))
			rep.Unreadable++
			continue
		}
		if err := idx.Add(segs[0], append([]string{}, segs[1:len(segs)-1]...), m, rec.Path, key); err != nil {
			logger.Printf("index catch-up: a message could not be indexed: %v", err)
			rep.Unreadable++
			continue
		}
		rep.Added++
	}
	if err := idx.Flush(); err != nil {
		return rep, err
	}
	if idx.Lost() > lostBefore {
		return rep, fmt.Errorf("index catch-up: %d message(s) were lost when SQLite abandoned a batch; the index stays marked behind", idx.Lost()-lostBefore)
	}
	store.ClearIndexBehind()
	if rep.Added+rep.Unreadable > 0 {
		logger.Printf("index catch-up: added %d message(s) to the search index, %d could not be read", rep.Added, rep.Unreadable)
	}
	return rep, nil
}

func revOf(s *state.SealedUnit) uint32 {
	if s == nil {
		return 0
	}
	return s.Rev
}

// deriveSealedMessage is deriveMessage for an encrypted archive's record: the same
// derivation, from the sealed units instead of files. nil when the page cannot be read.
func deriveSealedMessage(units *export.Units, rec state.Record) *model.Message {
	if rec.Sealed == nil || rec.Sealed.HTML == nil {
		return nil
	}
	if rec.Sealed.EML != nil {
		if data, err := units.Read(rec.Unit, vault.UnitOriginal, *rec.Sealed.EML); err == nil {
			if m := source.ParseRFC822(data); m != nil {
				return m
			}
		}
	}
	page, err := units.Read(rec.Unit, vault.UnitMessage, *rec.Sealed.HTML)
	if err != nil {
		return nil
	}
	m, _ := readArchivedHTML(page)
	if rec.Sealed.Zip != nil {
		if data, err := units.Read(rec.Unit, vault.UnitAttachment, *rec.Sealed.Zip); err == nil {
			if zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err == nil {
				for _, f := range zr.File {
					if !f.FileInfo().IsDir() {
						m.Attachments = append(m.Attachments, model.Attachment{Filename: f.Name})
					}
				}
			}
		}
	}
	return m
}

// RebuildEncrypted is an encrypted archive's keyed index rebuild at engine level
// (design phase2b §6): under the archive lock, a new in-memory index built from
// every record's sealed units, committed by the record's save. It is the remedy for
// a damaged index section or an index marked behind. Like Run it is handed its
// Options (Out and Key), so nothing outside tests reaches it until Phase 3 (MA-726).
func RebuildEncrypted(ctx context.Context, opts Options, logger *log.Logger) (IndexCatchUp, error) {
	if opts.Key == nil {
		return IndexCatchUp{}, ErrEncryptedNeedsKey
	}
	out := opts.Out
	if err := preflightEncrypted(opts); err != nil {
		return IndexCatchUp{}, err
	}
	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), lockVerb(opts, "rebuild"))
	if err != nil {
		return IndexCatchUp{}, withKind(state.FailArchiveUnwritable, err)
	}
	defer lock.Release()
	manifest, units, store, err := openEncrypted(opts, lock.StillHeld, time.Now().UTC(), logger, nil)
	if err != nil {
		return IndexCatchUp{}, err
	}
	si, err := openSealedIndex(store, out, manifest.Len(), true, logger)
	if err != nil {
		return IndexCatchUp{}, err
	}
	if si == nil {
		if err := manifest.Save(); err != nil { // the index marked behind
			return IndexCatchUp{}, manifestSaveFailure(out, err)
		}
		return IndexCatchUp{}, withKind(state.FailScratch, fmt.Errorf("%w: the search index cannot be rebuilt without room to work on it", ErrScratchSpace))
	}
	defer si.idx.Close()
	store.MarkIndexBehind()
	rep, err := catchUpIndex(si.idx, manifest, units, store, logger)
	if err != nil {
		return rep, withKind(state.FailIndex, err)
	}
	if err := manifest.Save(); err != nil {
		return rep, manifestSaveFailure(out, err)
	}
	if store.IndexBehind() {
		return rep, withKind(state.FailIndex, errors.New("the rebuilt search index is still marked behind"))
	}
	return rep, nil
}
