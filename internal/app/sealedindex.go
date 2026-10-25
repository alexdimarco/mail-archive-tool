package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
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
	sealed  int64  // the change count at the last capture; -1: never captured
	damaged bool   // the sealed index proved damaged when loaded, and was replaced by an empty one
	path    string // its scratch file, worked on there above the memory budget
	inFile  bool   // worked on in its scratch file
	full    bool   // it takes no more messages this run: outgrown memory with no room for its scratch file, or unsound
	unsound bool   // a batch was lost when written: it is not sealed again this run (noteLost)
	adds    int    // messages added this run (keepInBudget)
	lost    int    // rows lost with a batch, as last seen (noteLost)
}

// register hands the store a capture of si's index that runs only when the change
// count moved since the last seal — counted as sealed only once its commit landed, so
// a commit that fails after the capture leaves it for the next one.
func (si *sealedIndex) register(store *container.Store) {
	store.SetIndex(func() (container.IndexCapture, bool, error) {
		if si.noteLost(store) {
			return container.IndexCapture{}, false, nil // the record keeps the last sound image
		}
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
		if si.noteLost(store) { // a batch the capture's own commit lost
			return container.IndexCapture{}, false, nil
		}
		rows, err := si.idx.Count()
		if err != nil {
			return container.IndexCapture{}, false, err
		}
		return container.IndexCapture{Image: img, Size: size, Version: si.idx.Version(), Rows: rows, Sealed: func() { si.sealed = n }}, true, nil
	})
}

// noteLost reports whether the index is unsound: SQLite abandoned a batch since the
// run opened it — rows whose Add succeeded but whose batch commit failed (a full
// scratch drive, say). Those rows are in no image, and a commit that failed part-way
// through writing may leave the database itself inconsistent, so the index is marked
// behind, takes no more messages and is not sealed again this run: the record keeps
// the last sound image, and the next run catches it up (the adversarial pass's
// CON-01). Called from the capture, so the same commit records the mark.
func (si *sealedIndex) noteLost(store *container.Store) bool {
	if l := si.idx.Lost(); l > si.lost {
		si.lost, si.unsound, si.full = l, true, true
		store.MarkIndexBehind()
	}
	return si.unsound
}

// budgetCheckEvery is how many messages a run adds between checks that its in-memory
// index still fits the memory budget; a var so a test can check at every one.
var budgetCheckEvery = 256

// keepInBudget keeps a run's in-memory index within the memory budget as messages are
// added (the adversarial pass's CRIT-1): the choice made at open sees only what the
// record held, so a first backup always starts in memory. One that outgrows the budget
// moves to its scratch file; with no room for it, the index takes no more messages
// this run and is marked behind — the backup goes on, and the next run with room
// catches it up. It reports whether the index takes the next message.
func (si *sealedIndex) keepInBudget(store *container.Store, logger *log.Logger) bool {
	if si.full {
		return false
	}
	if si.adds++; si.inFile || si.adds%budgetCheckEvery != 0 {
		return true
	}
	size, err := si.idx.MemorySize()
	if err != nil || size <= indexMemoryBudget {
		return true
	}
	dir := filepath.Dir(si.path)
	need := size + scratchMargin
	if free, short := scratchSpaceShort(dir, need); short {
		err = fmt.Errorf("working on it needs about %d MB on the drive that holds %s, which has %d MB free — free space on that drive", need>>20, dir, free>>20)
	} else if err = os.MkdirAll(dir, 0o700); err == nil {
		err = si.idx.MoveToScratch(si.path)
	}
	if err != nil {
		logger.Printf("warning: the search index is not updated for the rest of this run: it outgrew memory, and %v; the next run with room catches it up", err)
		store.MarkIndexBehind()
		si.full = true
		return false
	}
	si.inFile = true
	return true
}

// indexExported adds one exported message to the run's index: Run's and RunGraph's
// OnExported step. A sealed index is kept within the memory budget first
// (keepInBudget); a message it does not take, or whose add fails, leaves it marked
// behind (P2b-C16), so the next run adds it.
func indexExported(idx *index.Index, si *sealedIndex, sealed *container.Store, logger *log.Logger, store string, folderPath []string, m *model.Message, relPath, key string) error {
	if si != nil && !si.keepInBudget(sealed, logger) {
		return nil
	}
	err := indexAdd(idx, store, folderPath, m, relPath, key)
	if err != nil {
		markSealedIndexBehind(sealed)
	}
	return err
}

// drop takes a sealed index that cannot be loaded or repaired out of the run: the
// backup goes on without it and the index is marked behind, as it is with no room to
// work on it (P2b-C16; the adversarial pass's CRIT-3).
func (si *sealedIndex) drop(store *container.Store, err error, logger *log.Logger) {
	logger.Printf("warning: the search index is not updated this run: %v; the next run catches it up", err)
	store.MarkIndexBehind()
	store.SetIndex(nil)
}

// closeRunIndex closes a run's index at its end. A sealed index that does not close
// cleanly is said (the adversarial pass's LEAK-03): its scratch file, if one is left,
// holds the index in plaintext until the archive's next keyed run removes it.
func closeRunIndex(idx *index.Index, sealed bool, logger *log.Logger) {
	if err := idx.Close(); err != nil && sealed {
		logger.Printf("warning: the search index did not close cleanly (%v); a working copy of it left in MailArchive's working folder is removed by the archive's next backup", err)
	}
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
	if si == nil || si.unsound || !si.unsealed() { // an unsound index is already marked, and never sealed
		return nil
	}
	logger.Printf("warning: the search index changed after its last save; it is marked to be caught up")
	store.MarkIndexBehind()
	return manifest.Save()
}

// indexAdd adds one message to the run's index (exported, or caught up); a var so a
// test can fail it.
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
	Failed     int // records read, that the index could not take (it stays marked behind)
}

// catchUpIndex adds to idx every record of manifest it does not hold, deriving each
// message from its sealed units (the preserved original when there is one, else the
// page, with attachment names from the zip) — the encrypted archive's rebuild (§6,
// P2b-C16). A record that cannot be read is logged and left out, never removed from
// the record. When every record is either indexed or unreadable the index is no
// longer behind; the run's next commit seals it. A record the index could not take —
// its scratch drive full, say — keeps it behind, so a later run adds it (the
// adversarial pass's LOC-02, FID-03).
func catchUpIndex(idx *index.Index, manifest *state.Manifest, units *export.Units, store *container.Store, logger *log.Logger) (rep IndexCatchUp, err error) {
	defer func() {
		if err != nil {
			// What it could not add waits for a later run — also for an index that was
			// not marked behind, only absent beside records.
			store.MarkIndexBehind()
		}
	}()
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
		if err := indexAdd(idx, segs[0], append([]string{}, segs[1:len(segs)-1]...), m, rec.Path, key); err != nil {
			logger.Printf("index catch-up: a message could not be indexed: %v", err)
			rep.Failed++
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
	if rep.Failed > 0 {
		return rep, fmt.Errorf("index catch-up: %d message(s) could not be added; the index stays marked behind", rep.Failed)
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
