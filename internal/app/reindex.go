package app

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// Reindex reconciles the archive at out with what is actually on disk. Manually
// deleting, moving, or renaming exported files leaves dangling rows in the
// search index, stale entries in the manifest, and out-of-date folder pages;
// nothing else reconciles them. Reindex opens the index and manifest, prunes
// every entry whose exported file is gone, regenerates the browsable folder
// pages from the surviving set, and saves the manifest. Surviving files stay
// searchable; no exported message file is deleted — only stale .mailarchive-*.tmp
// temps and orphan -attachments.zip files with no sibling .html are swept
// (SweepOrphans). It returns how many rows were kept and pruned.
func Reindex(out string, logger *log.Logger) (kept, pruned int, err error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	// One run per archive at a time (R5): reindex sweeps and rewrites, so it
	// must never overlap an export.
	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "reindex")
	if err != nil {
		return 0, 0, err
	}
	defer lock.Release()

	idxPath := filepath.Join(out, "search.db")
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	// A present manifest means the archive can be rebuilt from its own files, so
	// a lost or unopenable index is not "run an export first" — it names the
	// `reindex -rebuild` recovery instead (PC7). Only a directory that was never
	// exported to (no manifest either) still points at export.
	_, mStatErr := os.Stat(mpath)
	manifestPresent := !errors.Is(mStatErr, fs.ErrNotExist)
	// Refuse on a directory that was never exported to, rather than silently
	// creating an empty index and reporting "kept=0 pruned=0" (index.Open would
	// create the file). This mirrors serve/search naming the missing index.
	if _, statErr := os.Stat(idxPath); errors.Is(statErr, fs.ErrNotExist) {
		if manifestPresent {
			return 0, 0, fmt.Errorf("no search index at %s, but the archive's manifest is intact: rebuild the index from the archive with `mailarchive reindex -rebuild -out %s`", idxPath, out)
		}
		return 0, 0, fmt.Errorf("no search index at %s (run an export first)", idxPath)
	}
	idx, err := index.Open(idxPath)
	if err != nil {
		if manifestPresent {
			return 0, 0, fmt.Errorf("search index at %s cannot be opened (%v): rebuild it from the archive with `mailarchive reindex -rebuild -out %s`", idxPath, err, out)
		}
		return 0, 0, fmt.Errorf("open search index at %s: %w", idxPath, err)
	}
	defer idx.Close()

	manifest, err := state.Load(mpath)
	if err != nil {
		return 0, 0, err
	}
	// A first open after upgrade re-scopes the manifest and index once (F2);
	// reindex is a valid place for it (it already opens both). Driven by the
	// manifest's re-key signal so an old-binary excursion is repaired here too.
	if manifest.StoresMigrated > 0 {
		logger.Printf("canonicalized %d store path%s (one-time upgrade)",
			manifest.StoresMigrated, plural(manifest.StoresMigrated, "", "s"))
	}
	if manifest.Rekeyed > 0 {
		logger.Printf("re-scoped %d manifest entr%s by store (one-time upgrade; cost scales with archive size)",
			manifest.Rekeyed, plural(manifest.Rekeyed, "y", "ies"))
	}
	if _, rkErr := idx.RepairKeys(manifest.Rekeyed > 0, state.MigrateKey, logger); rkErr != nil {
		return 0, 0, fmt.Errorf("migrate search index keys: %w", rkErr)
	}
	if n := export.SweepOrphans(out, time.Now(), logger); n > 0 {
		logger.Printf("swept %d orphaned temp/zip file(s)", n)
	}
	// A crashed `reindex -rebuild` leaves search.db.rebuild(+ -wal/-shm) that
	// only the next rebuild would otherwise remove. This reconcile holds the
	// archive lock, so no rebuild can be in progress: the leftover is garbage
	// and safe to reclaim now (INT-3).
	removeDBFiles(idxPath + ".rebuild")

	// A dangling row: its exported file no longer exists on disk. Collect first —
	// EachRow holds the DB connection open for the walk, so we must not delete
	// until it returns.
	type dangling struct {
		id  int64
		key string
	}
	var gone []dangling
	err = idx.EachRow(func(id int64, key, relPath string) error {
		full := filepath.Join(out, filepath.FromSlash(relPath))
		switch _, statErr := os.Stat(full); {
		case statErr == nil:
			kept++
			return nil
		case errors.Is(statErr, fs.ErrNotExist):
			gone = append(gone, dangling{id: id, key: key})
			return nil
		default:
			return fmt.Errorf("stat %s: %w", relPath, statErr)
		}
	})
	if err != nil {
		return 0, 0, err
	}

	// deleteMessageFiles removes a message's html + -attachments.zip + .eml triple
	// (each os.Remove no-ops if absent). This runs ONLY in reindex (redaction),
	// never on a normal run, so R13's "a normal run never deletes a message file"
	// holds; here the .html is already gone (that is why the row is pruned), and
	// leaving the .zip/.eml — or a collapse-loser's copy — would keep a redacted
	// message served at /files/ (rev-4 §6, #4/#10).
	deleteMessageFiles := func(relHTML string) {
		stem := strings.TrimSuffix(filepath.Join(out, filepath.FromSlash(relHTML)), ".html")
		for _, suffix := range []string{".html", "-attachments.zip", ".eml"} {
			if err := os.Remove(stem + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
				logger.Printf("warning: redaction could not remove %s: %v", stem+suffix, err)
			}
		}
	}
	for _, g := range gone {
		if rec, ok := manifest.Get(g.key); ok {
			deleteMessageFiles(rec.Path) // the pruned record's own orphaned siblings
			for _, also := range rec.AlsoFiles {
				deleteMessageFiles(also) // every collapse-loser copy folded in (#4)
			}
		}
		if delErr := idx.DeleteByID(g.id); delErr != nil {
			return 0, 0, fmt.Errorf("prune index row %d: %w", g.id, delErr)
		}
		manifest.Delete(g.key)
		pruned++
	}
	if err := idx.Flush(); err != nil {
		return 0, 0, err
	}

	// Regenerate folder + root index pages from the reconciled index so they no
	// longer list the pruned messages.
	if err := pages.Generate(out, idx, logger); err != nil {
		return 0, 0, fmt.Errorf("regenerate folder pages: %w", err)
	}
	writeReport(out, manifest, manifest.Migrated > 0, logger)
	writeArchiveReadme(out, logger)
	if err := manifest.Save(); err != nil {
		return 0, 0, err
	}

	// Redaction spans all dates (T5/R21): a message whose files were deleted is
	// pruned from the manifest above, so its history events would still surface
	// it in a past `serve` view (until the on-disk intersection also hid it).
	// Compact the timeline to drop every event whose key is no longer in the
	// reconciled manifest — a message still on disk (a departed-from-mailbox
	// record keeps its file and its manifest row, R13) is untouched, so only a
	// genuinely removed message loses its trace. Run headers/footers and folder
	// renames are kept, so the date track survives.
	//
	// This runs LAST, AFTER manifest.Save: the manifest is the keep-authority for
	// compaction (keep == manifest.Has), so persisting it first means a crash
	// between the two never leaves the log durably shy of events a still-persisted
	// manifest asserts. If compaction then fails or a crash precedes it, the log
	// keeps stale events for the redacted message, but serve already hides it via
	// the on-disk intersection and the next reindex compacts — the derived log
	// heals from (manifest ∩ disk) (#11, adversarial 2026-09-12; §3.5's ordering
	// applied to the redaction path).
	hpath := filepath.Join(out, state.HistoryName)
	if dropped, cerr := state.CompactHistory(hpath, manifest.Has); cerr != nil {
		return 0, 0, fmt.Errorf("compact history log: %w", cerr)
	} else if dropped > 0 {
		logger.Printf("compacted %d history event(s) for redacted message(s)", dropped)
	}
	return kept, pruned, nil
}

// RebuildReport is what `reindex -rebuild` reconstructed. FromEML records were
// parsed from preserved original bytes (full fidelity); FromHTML records were
// re-derived from the archived page (searchable text, not the original wire
// bytes). Unrecovered counts the individual core fields (subject/from/date) a
// from-HTML record could not recover. Pruned counts records dropped because
// their file was missing on disk or failed the path gate.
type RebuildReport struct {
	Rebuilt     int // records indexed (FromEML + FromHTML)
	FromEML     int
	FromHTML    int
	Unrecovered int
	Pruned      int
}

// rebuildMaxFileBytes caps how large a per-record file the rebuild reads into
// memory to parse, so a pathological or hostile multi-GB .eml/.html cannot OOM
// the rebuild (PC2). A var so a test can shrink it.
var rebuildMaxFileBytes int64 = 512 << 20 // 512 MiB

// Rebuild reconstructs the search index and the folder pages from the archive
// alone — the manifest and the on-disk per-message files — touching no reader
// source and no network (G1, P4a). It is the recovery for an archive whose
// search.db was lost, corrupted, or left out of a copy while its .html files
// (and, when the archive was built with -raw, its .eml siblings) survive.
//
// It runs under the archive lock and requires an intact manifest (manifest
// reconstruction is out of scope — PC7). It never deletes the live index in
// place: the fresh index is built into a sibling search.db.rebuild opened on an
// absent file (so both docs and docs_fts start empty — no DELETE, no orphaned
// or rowid-colliding FTS row can survive — F1/PC1) and renamed over search.db
// only on full success, so a crash leaves the old index untouched or an inert
// leftover temp (R5). A present-but-unopenable live index is simply replaced by
// the rename.
//
// For each manifest record: its recorded path is validated with the exact gate
// verify uses (validRelPath + component-wise Lstat, no symlink follow,
// regular-file-only, size-bounded) before anything is opened (PC2); a record
// whose file is missing or fails the gate is pruned and reported, never read.
// A record whose <stem>.eml is present and passes the gate is parsed from those
// original bytes (FromEML); otherwise it is re-derived from the archived
// <stem>.html with attachment names read from the sibling zip (FromHTML). Every
// per-record read/parse/zip error is counted and that record skipped, never
// fatal (PC4). The summary reports the from-eml vs re-derived split and the
// count of fields that could not be recovered (PC5); rebuild changes no message
// file, though it does regenerate the folder and root index.html pages.
func Rebuild(out string, logger *log.Logger) (RebuildReport, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	var rep RebuildReport

	// One run per archive at a time (R5): rebuild rewrites the index and pages.
	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "reindex")
	if err != nil {
		return rep, err
	}
	defer lock.Release()

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, statErr := os.Stat(mpath); errors.Is(statErr, fs.ErrNotExist) {
		return rep, fmt.Errorf("cannot rebuild the search index: no manifest (%s) in %s — rebuild reads it, and a copy that skips dotfiles may have dropped it; restore it from a backup (rebuilding the manifest itself is out of scope)", mpath, out)
	}
	manifest, err := state.Load(mpath)
	if err != nil {
		return rep, err
	}
	// Upgrade bookkeeping, logged exactly as reindex/Run do (PC6). The fresh
	// index Open below stamps the current index meta version on its own.
	if manifest.StoresMigrated > 0 {
		logger.Printf("canonicalized %d store path%s (one-time upgrade)",
			manifest.StoresMigrated, plural(manifest.StoresMigrated, "", "s"))
	}
	if manifest.Rekeyed > 0 {
		logger.Printf("re-scoped %d manifest entr%s by store (one-time upgrade; cost scales with archive size)",
			manifest.Rekeyed, plural(manifest.Rekeyed, "y", "ies"))
	}

	idxPath := filepath.Join(out, "search.db")
	tmpPath := idxPath + ".rebuild"
	removeDBFiles(tmpPath) // clear any leftover temp from an interrupted rebuild
	idx, err := index.Open(tmpPath)
	if err != nil {
		return rep, fmt.Errorf("open rebuild index at %s: %w", tmpPath, err)
	}

	// verf reuses verify's exact component-wise inspect (no symlink follow,
	// regular-file-only) so the rebuild trusts a manifest path the identical way
	// verify does (PC2). Only its out field is used.
	verf := &verifier{out: out}

	records := manifest.All()
	keys := make([]string, 0, len(records))
	for k := range records {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order for logs and pruning

	for _, key := range keys {
		rec := records[key]
		segs, ok := validRelPath(rec.Path)
		if !ok {
			logger.Printf("rebuild: skipped %s — recorded path is not a safe in-archive path; pruned", rawPath(rec.Path))
			manifest.Delete(key)
			rep.Pruned++
			continue
		}
		kind, size, htmlFull := verf.inspect(segs)
		switch kind {
		case "ok":
			// proceed
		case "missing":
			manifest.Delete(key)
			rep.Pruned++
			continue
		default: // symlink / irregular — never followed or opened (PC2)
			logger.Printf("rebuild: skipped %s — %s where an archived file should be, not read; pruned", rec.Path, kind)
			manifest.Delete(key)
			rep.Pruned++
			continue
		}
		if size > rebuildMaxFileBytes {
			logger.Printf("rebuild: skipped %s — %d bytes exceeds the %d-byte read cap, not read; pruned", rec.Path, size, rebuildMaxFileBytes)
			manifest.Delete(key)
			rep.Pruned++
			continue
		}

		m, fromEML := deriveMessage(out, rec.Path, htmlFull, verf, &rep, logger)
		if m == nil {
			logger.Printf("rebuild: skipped %s — could not be read; pruned", rec.Path)
			manifest.Delete(key)
			rep.Pruned++
			continue
		}

		store := segs[0]
		folderPath := append([]string{}, segs[1:len(segs)-1]...)
		if addErr := idx.Add(store, folderPath, m, rec.Path, key); addErr != nil {
			// A single record's insert failing must not tear the whole rebuild
			// (PC4). Count it pruned and continue.
			logger.Printf("rebuild: could not index %s: %v; skipped", rec.Path, addErr)
			manifest.Delete(key)
			rep.Pruned++
			continue
		}
		rep.Rebuilt++
		if fromEML {
			rep.FromEML++
		} else {
			rep.FromHTML++
		}
	}

	if err := idx.Flush(); err != nil {
		idx.Close()
		removeDBFiles(tmpPath)
		return rep, fmt.Errorf("flush rebuild index: %w", err)
	}
	// Regenerate folder + root pages FROM THE TEMP INDEX (still open) so the
	// pages match exactly what the rebuild indexed (G1, PC5).
	if err := pages.Generate(out, idx, logger); err != nil {
		idx.Close()
		removeDBFiles(tmpPath)
		return rep, fmt.Errorf("regenerate folder pages: %w", err)
	}
	if err := idx.Close(); err != nil {
		removeDBFiles(tmpPath)
		return rep, fmt.Errorf("close rebuild index: %w", err)
	}
	// Close checkpointed the temp's WAL into the file, so it is a complete
	// standalone db. Remove any stale WAL/SHM of the OLD live index (they would
	// be replayed against the freshly renamed file and corrupt it), then rename
	// the temp over search.db — the single moment the new index becomes live
	// (PC1, R5).
	os.Remove(idxPath + "-wal")
	os.Remove(idxPath + "-shm")
	if err := os.Rename(tmpPath, idxPath); err != nil {
		removeDBFiles(tmpPath)
		return rep, fmt.Errorf("replace search index at %s: %w", idxPath, err)
	}
	export.SyncDir(out) // the rename is the moment the new index goes live (INT-4)

	// Keep the rest of the archive internally consistent, exactly as reindex
	// does: regenerate the verification report and README, and persist the
	// manifest (its load-time re-scope and any records pruned above).
	writeReport(out, manifest, manifest.Migrated > 0, logger)
	writeArchiveReadme(out, logger)
	if err := manifest.Save(); err != nil {
		return rep, err
	}
	return rep, nil
}

// deriveMessage builds a model.Message for one manifest record from its on-disk
// files. It prefers the preserved original bytes (<stem>.eml when present and
// past the gate — full fidelity, fromEML=true); otherwise it re-derives the
// index fields from the archived page at htmlFull (fromEML=false) and reads
// attachment names from the sibling zip. It returns (nil,false) only when even
// the page cannot be read. rep.Unrecovered accrues the from-HTML honesty count.
func deriveMessage(out, relPath, htmlFull string, verf *verifier, rep *RebuildReport, logger *log.Logger) (*model.Message, bool) {
	stem := strings.TrimSuffix(relPath, ".html")

	// Preserved original bytes, gated exactly like the page (PC2).
	if emlSegs, ok := validRelPath(stem + ".eml"); ok {
		if kind, size, emlFull := verf.inspect(emlSegs); kind == "ok" && size <= rebuildMaxFileBytes {
			if data, rerr := os.ReadFile(emlFull); rerr == nil {
				if m := source.ParseRFC822(data); m != nil {
					return m, true
				}
			}
			// A torn/failed .eml read falls through to the page (PC4).
		}
	}

	data, rerr := os.ReadFile(htmlFull)
	if rerr != nil {
		return nil, false
	}
	m, unrecovered := readArchivedHTML(data)
	rep.Unrecovered += unrecovered
	m.Attachments = zipAttachments(out, stem, verf, logger)
	return m, false
}

// zipAttachments reads the entry names from a record's sibling attachment zip
// (<stem>-attachments.zip) via the gate, so the rebuilt index lists what came
// with the message. A missing/unsafe/oversized zip yields nil; a corrupt zip is
// reported and yields nil, never aborting the rebuild (PC3, PC4).
func zipAttachments(out, stem string, verf *verifier, logger *log.Logger) []model.Attachment {
	segs, ok := validRelPath(stem + "-attachments.zip")
	if !ok {
		return nil
	}
	kind, size, full := verf.inspect(segs)
	if kind != "ok" || size > rebuildMaxFileBytes {
		return nil
	}
	zr, err := zip.OpenReader(full)
	if err != nil {
		logger.Printf("rebuild: attachments zip for %s could not be read (%v); attachment names omitted", stem, err)
		return nil
	}
	defer zr.Close()
	var atts []model.Attachment
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		atts = append(atts, model.Attachment{Filename: f.Name})
	}
	return atts
}

// removeDBFiles removes a search-db file and its WAL/SHM siblings (best effort).
func removeDBFiles(path string) {
	os.Remove(path)
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")
}
