// Package app orchestrates a full export run: input discovery, per-file reading
// (with optional snapshotting), and manifest-tracked writing. Both the CLI and
// the GUI drive the exporter through Run so behaviour stays identical.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// Options describes a single export run.
type Options struct {
	Inputs    []string    // files and/or directories to read
	Auto      bool        // also auto-discover default Outlook locations
	Out       string      // output root directory
	Mode      export.Mode // incremental or full
	Since     time.Time   // zero means no date filter
	CopyFirst bool        // snapshot each file before reading (avoids locks)
	Manifest  string      // manifest path override (default <Out>/.mailarchive-manifest.json)
	Index     bool        // build/update the search index (search.db)
	Pages     bool        // generate browsable folder index.html pages

	// KeepRaw also preserves each message's original RFC 822 bytes as
	// <stem>.eml (mbox/maildir/Graph sources; a PST item has none).
	KeepRaw bool

	// CheckpointEvery saves the manifest and flushes the index every N
	// exported messages inside a store walk (R5: a hard crash keeps the
	// progress made). Zero means the default (1000).
	CheckpointEvery int

	// Unattended marks a scheduled run (the CLI's -unattended, the GUI's
	// headless job). A discovery refusal (a vanished -input, no sources found)
	// on an archive that already exists is then recorded as a failed run, so
	// `status` reports it (N13, R18); see RecordRefusedRun.
	Unattended bool
}

// Result summarizes a completed (or cancelled) run.
type Result struct {
	Stats        export.Stats
	Files        int
	ManifestSize int
	// ManifestBefore is the manifest's size when the run began, so the summary
	// can tell how many of the run's exports were new messages and how many
	// replaced an earlier copy of the same key (F13).
	ManifestBefore int
	Indexed        int    // messages in the search index (0 if indexing disabled)
	IndexErrors    int    // messages the index refused (R8 parity broken; surfaced, not swallowed)
	Issues         int    // rows in the regenerated verification report
	ReportPath     string // path to the verification report ("" when nothing to report)
	Fillable       int    // manifest records still missing content an on-demand source may deliver
	Terminal       int    // records missing content the source can never deliver (recorded, never retried)
	Unknown        int    // legacy records not yet re-examined (migrated from a version-1 manifest)
}

// finish fills the manifest-derived fields of a Result and regenerates the
// verification report (R1). When scaffold is true it also (re)writes the archive
// README; a Graph run passes false when no mailbox succeeded, so a run that
// captured nothing leaves no empty browsable scaffold behind. Shared by the
// local and Graph runners. The single operator-facing "Verification:" line is
// printed by the caller's summary (cmd/mailarchive printSummary), not here, so
// the run log carries it exactly once.
func finish(out string, r *Result, exp *export.Exporter, manifest *state.Manifest, idx *index.Index, indexErrors int, scaffold bool, logger *log.Logger) {
	if scaffold {
		writeArchiveReadme(out, logger)
	}
	r.Stats = exp.Stats
	r.ManifestSize = manifest.Len()
	r.Indexed = indexCount(idx) // commits any pending batch first
	// The failed Adds the caller counted, plus every row whose Add succeeded but
	// whose batch was lost afterwards (F49), so every message this run exported
	// has its row added or replaced in the index, or is counted. Indexed is the
	// index's total, so the index's growth plus IndexErrors == exported (on a
	// fresh archive, Indexed + IndexErrors == exported) only when the run
	// exports each key once and none is already indexed: a fill, a re-capture
	// of a legacy "unknown" record, a full-mode re-export, and — on a fresh
	// archive too — a key the same run exports twice (full mode over a source
	// holding a message twice, or an in-run fill of an earlier copy's gap) all
	// export a message again under the same key, and idx.Add replaces that
	// key's row instead of adding one.
	r.IndexErrors = indexErrors
	if idx != nil {
		r.IndexErrors += idx.Lost()
	}
	r.Fillable, r.Terminal, r.Unknown = manifest.Counts()
	r.ReportPath, r.Issues = writeReport(out, manifest, manifest.Migrated > 0, logger)
}

// indexOpenError is the refusal of an export or a Graph run whose search.db
// at idxPath cannot be opened for writing (index.Open's err). Like reindex's,
// it names the file and the remedy its cause has (R12, X6), never SQLite's
// bare error:
//   - a busy index is in use, not damaged — another program kept it locked
//     past the writable open's wait — so the remedy is to retry, never the
//     rebuild (MA-398);
//   - a newer format's refusal already names the file and the upgrade, and
//     stands: a rebuild would replace a newer index with an older one;
//   - a file or folder SQLite cannot write (index.ReadOnly: a read-only
//     remount, changed permissions, a read-only search.db) or a disk that
//     refuses the write (index.DiskError: full, a file-size limit, a failing
//     drive) is not damage: the rebuild writes a new index to the same place
//     and fails the same way, so the remedy is the writable folder or the
//     free space (F1);
//   - anything else, a damaged file (SQLITE_NOTADB) above all, is rebuilt
//     from the archive's own files, which needs the archive's manifest
//     (Rebuild refuses without one). With no manifest in out there is nothing
//     to rebuild from, and nothing in that file a run needs, so the remedy is
//     to move it aside: the next run builds a fresh index.
func indexOpenError(idxPath, out string, err error) error {
	switch {
	case errors.Is(err, index.ErrNewerFormat):
		return err
	case index.Busy(err):
		return fmt.Errorf("search index %s is busy (%w): another program kept it locked — close any other program that has it open, then run again", idxPath, err)
	case index.ReadOnly(err) || index.DiskError(err):
		return fmt.Errorf("search index %s cannot be written (%w): %s, then run again", idxPath, err, unwritableRemedy(idxPath, out, err))
	}
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, serr := os.Stat(mpath); errors.Is(serr, fs.ErrNotExist) {
		return fmt.Errorf("search index %s cannot be opened (%w), and there is no manifest (%s) to rebuild it from: move it out of %s, then run again", idxPath, err, mpath, out)
	}
	return fmt.Errorf("search index %s cannot be opened (%w): rebuild it from the archive with `mailarchive reindex -rebuild -out %q`, then run again", idxPath, err, out)
}

// unwritableRemedy names what to fix when SQLite cannot write the index at
// dbPath in folder out (err is index.ReadOnly or index.DiskError): the disk
// (full or failing), search.db itself (opened read-only because the file is
// not writable), the folder (SQLite cannot create the -wal/-shm or the file
// there), or, when neither can be told, both.
func unwritableRemedy(dbPath, out string, err error) string {
	if index.DiskError(err) {
		return fmt.Sprintf("the disk holding %s is full or failing — free space on it (or check the drive)", out)
	}
	fileRO := fileReadOnly(dbPath)
	dirRO := index.ReadOnlyDir(err)
	switch {
	case fileRO && dirRO:
		return fmt.Sprintf("%s and the archive folder %s are read-only — make both writable (or remount the drive read-write)", filepath.Base(dbPath), out)
	case fileRO:
		return fmt.Sprintf("%s is read-only — make it writable (on Windows, clear its Read-only attribute)", filepath.Base(dbPath))
	case dirRO:
		return fmt.Sprintf("the archive folder %s is read-only — make it writable (or remount the drive read-write)", out)
	}
	return fmt.Sprintf("%s or the archive folder %s is not writable — make both writable", filepath.Base(dbPath), out)
}

// fileReadOnly reports whether the file at path exists and refuses to be
// opened for writing for lack of permission (a 0444 file, Windows' Read-only
// attribute). Opening it write-only and closing it changes nothing.
func fileReadOnly(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return false
	}
	return errors.Is(err, fs.ErrPermission)
}

// ProgressFunc, if provided, is called after each processed message with the
// running stats. Keep it cheap; it runs inline with the export loop.
type ProgressFunc func(stats export.Stats)

// Run performs the export described by opts. If ctx is cancelled it stops
// promptly, saves the manifest, and returns the partial Result with
// context.Canceled.
func Run(ctx context.Context, opts Options, logger *log.Logger, onProgress ProgressFunc) (result Result, err error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	// Validate the inputs BEFORE creating anything (friction #2). A missing
	// -input path, or no sources at all, must refuse naming the problem while
	// creating no output dir, no lock, and no last-run record — a guaranteed
	// failure must not first leave a half-built archive and a failed run behind.
	// A scheduled run's refusal is still a run (N13): recorded failed when the
	// archive already exists, so it never leaves the previous success standing.
	files, err := DiscoverInputs(opts.Inputs, opts.Auto)
	if err == nil && len(files) == 0 {
		err = errors.New("no input mail sources found (a .pst/.ost file, an mbox file, or a mail directory; or enable auto-discovery)")
	}
	if err != nil {
		if opts.Unattended {
			RecordRefusedRun(opts, err)
		}
		return Result{}, err
	}

	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output dir: %w", err)
	}
	// One run per archive at a time (R5): a scheduled run overlapping a manual
	// one would otherwise interleave manifest/index/file writes.
	lock, err := lockfile.AcquireAs(filepath.Join(opts.Out, lockfile.Name), "export")
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	// Every run that begins is recorded (R18): "running" now, before any early
	// return, finalized on the way out — so a run that never finishes is
	// visible as such to `status`.
	defer recordRun(opts, beginRun(opts), &result, &err)

	// On -auto, warn up front when a live Exchange/IMAP .ost is about to be read
	// by the direct go-pst reader, which cannot read every .ost — the -outlook
	// remedy should not wait for a failure to appear (friction #1).
	if opts.Auto {
		_, classicOutlook := outlookcom.Detect()
		if adv := ostAdvisory(runtime.GOOS, files, classicOutlook); adv != "" {
			logger.Printf("%s", adv)
		}
	}

	// What a crashed earlier run may have left behind (R5): stale temps and
	// orphan zips. Only files older than this run's start are touched.
	if n := export.SweepOrphans(opts.Out, time.Now(), logger); n > 0 {
		logger.Printf("swept %d orphaned temp/zip file(s) from an interrupted run", n)
	}

	mpath := opts.Manifest
	if mpath == "" {
		mpath = filepath.Join(opts.Out, ".mailarchive-manifest.json")
	}
	manifest, err := state.Load(mpath)
	if err != nil {
		return Result{}, err
	}
	manifestBefore := manifest.Len()

	exp := &export.Exporter{
		OutDir:   opts.Out,
		Manifest: manifest,
		Mode:     opts.Mode,
		Since:    opts.Since,
		Log:      logger,
		KeepRaw:  opts.KeepRaw,
	}

	// One-time upgrade logs BEFORE the index repair, so cause (the manifest
	// re-scope) precedes effect (the index-key migration) — matching reindex and
	// the README sample (friction #17).
	if manifest.StoresMigrated > 0 {
		logger.Printf("canonicalized %d store path%s (one-time upgrade)",
			manifest.StoresMigrated, plural(manifest.StoresMigrated, "", "s"))
	}
	if manifest.Rekeyed > 0 {
		logger.Printf("re-scoped %d manifest entr%s by store (one-time upgrade; cost scales with archive size)",
			manifest.Rekeyed, plural(manifest.Rekeyed, "y", "ies"))
	}

	// Optional search index, fed as each message is written.
	var idx *index.Index
	var indexErrors int
	if opts.Index {
		idxPath := filepath.Join(opts.Out, "search.db")
		idx, err = index.Open(idxPath)
		if err != nil {
			return Result{}, indexOpenError(idxPath, opts.Out, err)
		}
		defer idx.Close()
		// Repair legacy index keys to match the re-scoped manifest, driven by
		// the manifest's own re-key signal (force) — the only trigger that
		// survives an old-binary excursion — or the index's own version (F2).
		if _, rkErr := idx.RepairKeys(manifest.Rekeyed > 0, state.MigrateKey, logger); rkErr != nil {
			return Result{}, fmt.Errorf("migrate search index keys: %w", rkErr)
		}
		exp.OnExported = func(store string, folderPath []string, m *model.Message, relPath, key string) {
			if addErr := idx.Add(store, folderPath, m, relPath, key); addErr != nil {
				indexErrors++
				logger.Printf("warning: index: %v", addErr)
			}
		}
	}
	if manifest.Migrated > 0 {
		logger.Printf("%d manifest entr%s predate completeness tracking; they will be re-examined by this and following incremental runs",
			manifest.Migrated, plural(manifest.Migrated, "y", "ies"))
	}

	// Checkpoint inside a store walk so a hard crash keeps the progress made
	// (R5) — not only at store boundaries, where a single huge .pst would
	// otherwise leave hours of work unrecorded. The cadence scales with the
	// archive size (effectiveEvery) so a small archive checkpoints often and a
	// huge one does not pay a full save every configured-N messages.
	floor := opts.CheckpointEvery
	if floor <= 0 {
		floor = defaultCheckpointEvery
	}
	lastCheckpoint := 0
	var lockLost error
	// saveErr is the outcome of the most recent manifest save: a failed
	// mid-walk checkpoint is healed by a later save that succeeds, but a run
	// whose LAST save failed has kept no progress and must fail (F53).
	var saveErr error
	checkpoint := func(stats export.Stats) {
		if stats.Exported-lastCheckpoint < effectiveEvery(floor, manifest.Len()) {
			return
		}
		lastCheckpoint = stats.Exported
		// The lock file is the run's guarantee of exclusivity; if it was removed
		// or replaced under us, another run may now hold a fresh one. Stop
		// rather than race it (R5).
		if err := lock.StillHeld(); err != nil && lockLost == nil {
			lockLost = err
			return
		}
		// A one-shot local import keeps no go-back timeline (its dedup is
		// folder-scoped, §3.6), so there is no history writer to sync here.
		saveErr = commit(nil, idx, manifest, logger)
	}

	result = Result{Files: len(files), ManifestBefore: manifestBefore}
	var failures int
	for _, f := range files {
		runErr := runFile(ctx, exp, f, opts.Out, opts.CopyFirst, logger, func(stats export.Stats) {
			checkpoint(stats)
			if onProgress != nil {
				onProgress(stats)
			}
		}, func() error { return lockLost })
		if lockLost != nil {
			// The lock was removed or replaced mid-run: another run may now own
			// this archive. Do NOT write shared state (manifest/index/README) into
			// it — the deferred recordRun marks this run failed, and the message
			// files already written are reconciled by the next incremental run
			// (index-before-manifest ordering leaves no orphaned manifest rows).
			// (INT-CC-2/R5.)
			return result, lockLost
		}
		saveErr = commit(nil, idx, manifest, logger)

		if errors.Is(runErr, context.Canceled) {
			finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
			if saveErr != nil {
				// "progress was kept" would be false: record it failed.
				return result, manifestSaveFailure(mpath, saveErr)
			}
			return result, context.Canceled
		}
		if runErr != nil {
			failures++
			logger.Printf("error: %s: %v%s", f, runErr, ostOpenRemedy(f))
		}
	}

	// Browsable folder index pages, built from the finished index. Regenerate
	// them only when this run exported something or the root index.html is not
	// yet there (nas-02): a zero-change re-run must not rewrite every page.
	if idx != nil && opts.Pages && pagesNeeded(opts.Out, exp.Stats.Exported) {
		if pErr := pages.Generate(opts.Out, idx, logger); pErr != nil {
			logger.Printf("warning: folder pages: %v", pErr)
		}
	}

	finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
	if saveErr != nil {
		if failures > 0 {
			return result, fmt.Errorf("%d file(s) failed; %w", failures, manifestSaveFailure(mpath, saveErr))
		}
		return result, manifestSaveFailure(mpath, saveErr)
	}
	if failures > 0 {
		return result, fmt.Errorf("%d file(s) failed", failures)
	}
	return result, nil
}

// manifestSaveFailure is the run error when the run's final manifest save
// failed (F53): the manifest is the run's whole persisted result, so nothing
// this run did is recorded and the next run repeats it. It names the file and
// the remedy; recordRun finalizes the last-run record failed with it, so
// `status` reports RED instead of a green "ok".
func manifestSaveFailure(mpath string, err error) error {
	return fmt.Errorf("could not save the manifest %s (%w): this run's progress was not recorded and the next run will repeat it — make sure the manifest file and its folder are writable (on Windows, that no other program holds the manifest open), then run again", mpath, err)
}

// defaultCheckpointEvery aligns with the index's own batch size.
const defaultCheckpointEvery = 1000

// effectiveEvery is the checkpoint cadence for an archive currently holding n
// records: roughly an eighth of the archive, clamped to at least floor (the
// configured CheckpointEvery, default 1000) and at most 25000. A small archive
// checkpoints every floor messages; a very large one caps the save frequency so
// the durability writes never dominate a long run (nas-03).
func effectiveEvery(floor, n int) int {
	e := n / 8
	if e < floor {
		e = floor
	}
	if e > 25000 {
		e = 25000
	}
	return e
}

// pagesNeeded reports whether the browsable folder pages should be regenerated:
// only when this run exported at least one message, or the root index.html does
// not yet exist. A zero-change re-run then leaves the pages (and their mtimes)
// untouched (nas-02). reindex regenerates unconditionally — it reconciles the
// pages to what survives on disk.
func pagesNeeded(out string, exported int) bool {
	if exported > 0 {
		return true
	}
	_, err := os.Stat(filepath.Join(out, "index.html"))
	return errors.Is(err, fs.ErrNotExist)
}

// ostAdvisory returns the up-front guidance to print before an -auto run reads
// its inputs, when a live Exchange/IMAP Outlook .ost is about to be routed into
// the direct go-pst reader (which cannot read every .ost) and the classic
// Outlook that could export a clean PST is present. It is a pure function of the
// OS, the discovered paths, and whether classic Outlook was detected, so the
// decision is unit-tested without a Windows host. Empty when it does not apply
// (not Windows, no .ost among the inputs, or no classic Outlook to run -outlook).
func ostAdvisory(goos string, paths []string, classicOutlook bool) string {
	if goos != "windows" || !classicOutlook {
		return ""
	}
	hasOST := false
	for _, p := range paths {
		if strings.EqualFold(filepath.Ext(p), ".ost") {
			hasOST = true
			break
		}
	}
	if !hasOST {
		return ""
	}
	return "Note: a live Exchange/IMAP Outlook .ost was auto-discovered. Some .ost caches " +
		"cannot be read directly; if this run reports read errors or missing mail, re-run " +
		"with -outlook to have classic Outlook export a clean PST first."
}

// commit makes progress durable in the crash-safe order (§3.5): the history log
// is appended+fsync'd FIRST, THEN the index is flushed, THEN the manifest is
// saved as the trailing anchor. So the manifest — the fold-to-now projection —
// never advances past the history events that explain it: after a crash the
// history is a superset of the manifest and the index is a superset too, and the
// next run re-observes the tail idempotently (a duplicate history event folds
// away; idx.Add replaces by key). A move is therefore never lost. hist and idx
// may be nil (a one-shot local run writes no timeline; indexing may be off). It
// returns the manifest save's error, so a run whose final save failed fails.
func commit(hist *state.HistoryWriter, idx *index.Index, manifest *state.Manifest, logger *log.Logger) error {
	var syncHistory, flushIndex, saveManifest func() error
	if hist != nil {
		syncHistory = hist.Sync
	}
	if idx != nil {
		flushIndex = idx.Flush
	}
	if manifest != nil {
		saveManifest = manifest.Save
	}
	return commitInOrder(syncHistory, flushIndex, saveManifest, logger)
}

// commitInOrder is the single definition of the crash-safe durability order
// (history → index → manifest, §3.5); commit wires the real stores to it and a
// test drives it with recording doubles to prove the order holds. A nil step is
// skipped. Each error is logged and does not abort the later steps: the
// invariant here is only about ORDER (the manifest is written last), never that
// every step succeeds. The manifest save's error is also returned: a mid-walk
// checkpoint may fail and be healed by a later save, but the caller fails a run
// whose last save failed (F53).
func commitInOrder(syncHistory, flushIndex, saveManifest func() error, logger *log.Logger) error {
	if syncHistory != nil {
		if err := syncHistory(); err != nil {
			logger.Printf("warning: history sync: %v", err)
		}
	}
	if flushIndex != nil {
		if err := flushIndex(); err != nil {
			logger.Printf("warning: index flush: %v", err)
		}
	}
	if saveManifest != nil {
		if err := saveManifest(); err != nil {
			logger.Printf("warning: could not save manifest: %v", err)
			return err
		}
	}
	return nil
}

// beginRun writes the "running" last-run record and returns it for recordRun.
func beginRun(opts Options) state.LastRun {
	lr := newRunRecord(opts)
	_ = state.WriteLastRun(opts.Out, lr) // best effort: the run itself must not fail on this
	return lr
}

// newRunRecord is the "running" last-run record for a run of opts, starting now.
func newRunRecord(opts Options) state.LastRun {
	exe, _ := os.Executable()
	mode := "incremental"
	if opts.Mode == export.Full {
		mode = "full"
	}
	return state.LastRun{Status: state.RunRunning, Started: time.Now().UTC(), PID: os.Getpid(), Exe: exe, Mode: mode, Job: canonicalJob(opts, mode), NoIndex: !opts.Index}
}

// RecordRefusedRun records a scheduled run that refused before it began — its
// -input vanished, or discovery found no sources — as a FAILED run of the
// archive at opts.Out (N13, R18): the last-run record names the cause and the
// attention sidecar is written, so `status` reports RED run_failed instead of
// the previous success. It writes only into an archive that already exists (a
// manifest or a last-run record is present): a first run that refuses still
// creates nothing (friction #2, MA-103). It takes the archive lock for the
// write and skips it when another run holds the lock — that run's own record
// is authoritative. Best effort, like every last-run write.
func RecordRefusedRun(opts Options, cause error) {
	mpath := opts.Manifest
	if mpath == "" {
		mpath = filepath.Join(opts.Out, ".mailarchive-manifest.json")
	}
	_, mErr := os.Stat(mpath)
	_, lrErr := os.Stat(filepath.Join(opts.Out, state.LastRunName))
	if mErr != nil && lrErr != nil {
		return
	}
	lock, err := lockfile.AcquireAs(filepath.Join(opts.Out, lockfile.Name), "export")
	if err != nil {
		return
	}
	defer lock.Release()
	lr := newRunRecord(opts)
	lr.Status, lr.Finished, lr.Error = state.RunFailed, lr.Started, cause.Error()
	_ = state.WriteLastRun(opts.Out, lr)
}

// canonicalJob reconstructs the run's export flags in the same shape the
// scheduler stores them, so `status` can offer a "schedule the same job"
// remedy. It returns nil when there is no determinable local export job — a
// Graph run (shared beginRun, no inputs) or a bare run — so status falls back
// to a generic phrase rather than inventing a wrong command.
func canonicalJob(opts Options, mode string) []string {
	if len(opts.Inputs) == 0 && !opts.Auto {
		return nil
	}
	job := []string{"-out", absOrSame(opts.Out), "-mode", mode}
	if opts.Auto {
		job = append(job, "-auto")
	}
	for _, in := range opts.Inputs {
		job = append(job, "-input", absOrSame(in))
	}
	if opts.CopyFirst {
		job = append(job, "-copy-first")
	}
	if opts.KeepRaw {
		job = append(job, "-raw")
	}
	return job
}

// absOrSame returns p made absolute (so a recorded job is pasteable from any
// directory), or p itself when it cannot be resolved.
func absOrSame(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// recordRun finalizes the last-run record from the run's outcome.
func recordRun(opts Options, lr state.LastRun, result *Result, err *error) {
	lr.Finished = time.Now().UTC()
	switch {
	case *err == nil:
		lr.Status = state.RunOK
	case errors.Is(*err, context.Canceled):
		lr.Status = state.RunCancelled
	default:
		lr.Status = state.RunFailed
		lr.Error = (*err).Error()
	}
	lr.Exported, lr.Filled = result.Stats.Exported, result.Stats.Filled
	lr.Fillable, lr.Terminal, lr.Unknown, lr.IndexErrors = result.Fillable, result.Terminal, result.Unknown, result.IndexErrors
	_ = state.WriteLastRun(opts.Out, lr)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func indexCount(idx *index.Index) int {
	if idx == nil {
		return 0
	}
	n, err := idx.Count()
	if err != nil {
		return 0
	}
	return n
}

// ostOpenRemedy returns guidance to append when a source fails to open/parse and
// the input is an Outlook `.ost`: go-pst cannot read every live Cached-Exchange
// `.ost` (invariant R16 / scenario S19), so a bare "corrupt or unsupported" error
// strands a CLI user with no next step (the GUI already steers here). Point the
// operator at the reliable paths — Outlook's own export on Windows, or the Graph
// device path for a Microsoft 365 mailbox. Pure function of the path so a test can
// assert the wording without a real `.ost`; empty for any non-`.ost` source.
func ostOpenRemedy(path string) string {
	if !strings.EqualFold(filepath.Ext(path), ".ost") {
		return ""
	}
	return " — a live Exchange / Microsoft 365 .ost often cannot be read directly;" +
		" on Windows run `mailarchive -outlook` (Outlook exports a clean .pst first)," +
		" or archive a Microsoft 365 mailbox server-side with `mailarchive graph -auth device`"
}

// runFile opens one data file (optionally via a temp snapshot) and exports every
// message it contains, honouring ctx cancellation between messages.
func runFile(ctx context.Context, exp *export.Exporter, path, out string, copyFirst bool, logger *log.Logger, onProgress ProgressFunc, abort func() error) error {
	openPath := path
	if copyFirst {
		// Snapshotting only applies to single files (e.g. a locked .ost);
		// mail-store directories are read in place.
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			snap, cleanup, err := snapshot(path, out)
			if err != nil {
				return err
			}
			defer cleanup()
			openPath = snap
		}
	}

	reader, err := source.Open(openPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	store := reader.StoreName()
	// The token scopes this source's identity and names its on-disk tree. Its
	// seed is the CANONICAL source id (state.PathSourceID) of the original input
	// path (never the -copy-first snapshot, whose name changes every run), so a
	// different spelling of the same file — relative vs absolute, a symlink, a
	// trailing slash — hits the same token instead of minting a duplicate tree
	// (INT-CC-1/R2), while two distinct sources with the same display name still
	// get distinct, sticky trees (F1). The source is still READ through the
	// original path.
	token := exp.Manifest.Token(state.PathSourceID(path), store)
	logger.Printf("Reading %s (store: %s)", path, store)

	return reader.Walk(func(folderPath []string, m *model.Message) (err error) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// Stop promptly once the run lost its lock: bound continued writing to a
		// contended archive to the next message boundary (INT-CC-2/R5).
		if abort != nil {
			if e := abort(); e != nil {
				return e
			}
		}
		// A panic exporting one crafted message must not abort the whole run
		// (R10); log it and skip that message.
		defer func() {
			if r := recover(); r != nil {
				logger.Printf("warning: recovered from panic on a message in %s (skipped): %v",
					strings.Join(folderPath, "/"), r)
				err = nil
			}
		}()
		if _, exportErr := exp.Export(token, folderPath, m); exportErr != nil {
			return exportErr
		}
		if onProgress != nil {
			onProgress(exp.Stats)
		}
		return nil
	})
}

// snapshot copies src to a temporary file ON THE ARCHIVE'S VOLUME (the drive
// the operator chose and sized — a multi-gigabyte .ost must not land in a
// small system temp directory at 02:00), returning its path and a cleanup func.
func snapshot(src, out string) (string, func(), error) {
	in, err := os.Open(src)
	if err != nil {
		return "", nil, fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(out, ".mailarchive-snapshot-*"+filepath.Ext(src))
	if err != nil {
		return "", nil, fmt.Errorf("create snapshot: %w", err)
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("copy snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, err
	}
	name := tmp.Name()
	return name, func() { os.Remove(name) }, nil
}

// DiscoverInputs expands the given files/directories and, if auto is set, the
// default Outlook, Thunderbird, and Evolution locations into a deduplicated list
// of data files/stores.
func DiscoverInputs(inputs []string, auto bool) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			files = append(files, p)
		}
	}

	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("input %s does not exist (check the path, or that the drive holding it is mounted)", in)
			}
			return nil, fmt.Errorf("input %s: %w", in, err)
		}
		if info.IsDir() {
			if source.IsMailStoreDir(in) {
				add(in) // Thunderbird/mbox store: one source, don't expand
			} else {
				found, err := scanDir(in)
				if err != nil {
					return nil, err
				}
				for _, f := range found {
					add(f)
				}
			}
		} else {
			add(in)
		}
	}

	if auto {
		for _, f := range autoDiscover() {
			add(f)
		}
	}
	return files, nil
}

func scanDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isDataFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// autoDiscover returns default mail-source locations: Outlook data files
// (Windows), and Thunderbird and Evolution account/store directories
// (Linux/macOS/Windows).
func autoDiscover() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	// Outlook data files. Skip any that can't be parsed — Outlook leaves orphaned
	// or stub .ost files behind (e.g. from removed accounts) that would otherwise
	// pad the picker with unreadable entries. A file that is merely locked by a
	// running Outlook is kept (DataFileReadable is lock-tolerant).
	for _, g := range outlookGlobs() {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if source.DataFileReadable(m) {
				add(m)
			}
		}
	}

	// Thunderbird and Evolution account/store directories (only real stores).
	for _, g := range append(thunderbirdGlobs(), evolutionGlobs()...) {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil && fi.IsDir() && source.IsMailStoreDir(m) {
				add(m)
			}
		}
	}
	return out
}

func outlookGlobs() []string {
	var globs []string
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		globs = append(globs, filepath.Join(la, "Microsoft", "Outlook", "*.ost"))
		globs = append(globs, filepath.Join(la, "Microsoft", "Outlook", "*.pst"))
	}
	if up := os.Getenv("USERPROFILE"); up != "" {
		globs = append(globs, filepath.Join(up, "Documents", "Outlook Files", "*.pst"))
	}
	return globs
}

func thunderbirdGlobs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	roots := []string{
		filepath.Join(home, ".thunderbird"),
		filepath.Join(home, "snap", "thunderbird", "common", ".thunderbird"),
		filepath.Join(home, ".mozilla-thunderbird"),
		filepath.Join(home, "Library", "Thunderbird", "Profiles"),
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		roots = append(roots, filepath.Join(ad, "Thunderbird", "Profiles"))
	}
	var globs []string
	for _, r := range roots {
		// Each account under ImapMail/ and Mail/ (incl. "Local Folders") is a store.
		globs = append(globs, filepath.Join(r, "*", "ImapMail", "*"))
		globs = append(globs, filepath.Join(r, "*", "Mail", "*"))
	}
	return globs
}

// evolutionGlobs returns the default Evolution store locations: the local
// Maildir++ store and each IMAP account's disk cache (native and Flatpak paths).
func evolutionGlobs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "share", "evolution", "mail", "local"),
		filepath.Join(home, ".var", "app", "org.gnome.Evolution", "data", "evolution", "mail", "local"),
		filepath.Join(home, ".cache", "evolution", "mail", "*"),
		filepath.Join(home, ".var", "app", "org.gnome.Evolution", "cache", "evolution", "mail", "*"),
	}
}

func isDataFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".pst" || ext == ".ost"
}
