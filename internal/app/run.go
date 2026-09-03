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
}

// Result summarizes a completed (or cancelled) run.
type Result struct {
	Stats        export.Stats
	Files        int
	ManifestSize int
	Indexed      int    // messages in the search index (0 if indexing disabled)
	IndexErrors  int    // messages the index refused (R8 parity broken; surfaced, not swallowed)
	Issues       int    // rows in the regenerated verification report
	ReportPath   string // path to the verification report ("" when nothing to report)
	Fillable     int    // manifest records still missing content an on-demand source may deliver
	Terminal     int    // records missing content the source can never deliver (recorded, never retried)
	Unknown      int    // legacy records not yet re-examined (migrated from a version-1 manifest)
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
	r.Indexed = indexCount(idx)
	r.IndexErrors = indexErrors
	r.Fillable, r.Terminal, r.Unknown = manifest.Counts()
	r.ReportPath, r.Issues = writeReport(out, manifest, manifest.Migrated > 0, logger)
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
	files, err := DiscoverInputs(opts.Inputs, opts.Auto)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, errors.New("no input mail sources found (a .pst/.ost file, an mbox file, or a mail directory; or enable auto-discovery)")
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

	exp := &export.Exporter{
		OutDir:   opts.Out,
		Manifest: manifest,
		Mode:     opts.Mode,
		Since:    opts.Since,
		Log:      logger,
		KeepRaw:  opts.KeepRaw,
	}

	// Optional search index, fed as each message is written.
	var idx *index.Index
	var indexErrors int
	if opts.Index {
		idxPath := filepath.Join(opts.Out, "search.db")
		idx, err = index.Open(idxPath)
		if err != nil {
			return Result{}, fmt.Errorf("open search index: %w", err)
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
	if manifest.Rekeyed > 0 {
		logger.Printf("re-scoped %d manifest entr%s by store (one-time upgrade; cost scales with archive size)",
			manifest.Rekeyed, plural(manifest.Rekeyed, "y", "ies"))
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
		commit(manifest, idx, logger)
	}

	result = Result{Files: len(files)}
	var failures int
	for _, f := range files {
		runErr := runFile(ctx, exp, f, opts.Out, opts.CopyFirst, logger, func(stats export.Stats) {
			checkpoint(stats)
			if onProgress != nil {
				onProgress(stats)
			}
		})
		if lockLost != nil {
			commit(manifest, idx, logger)
			finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
			return result, lockLost
		}
		commit(manifest, idx, logger)

		if errors.Is(runErr, context.Canceled) {
			finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
			return result, context.Canceled
		}
		if runErr != nil {
			failures++
			logger.Printf("error: %s: %v", f, runErr)
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
	if failures > 0 {
		return result, fmt.Errorf("%d file(s) failed", failures)
	}
	return result, nil
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

// commit makes progress durable: the index FIRST, then the manifest. After a
// crash the index is then a superset of the manifest, and the next incremental
// run re-exports the un-manifested tail (idx.Add replaces by key), so search
// and manifest can never permanently disagree (R5/R8).
func commit(manifest *state.Manifest, idx *index.Index, logger *log.Logger) {
	if idx != nil {
		if flushErr := idx.Flush(); flushErr != nil {
			logger.Printf("warning: index flush: %v", flushErr)
		}
	}
	if saveErr := manifest.Save(); saveErr != nil {
		logger.Printf("warning: could not save manifest: %v", saveErr)
	}
}

// beginRun writes the "running" last-run record and returns it for recordRun.
func beginRun(opts Options) state.LastRun {
	exe, _ := os.Executable()
	mode := "incremental"
	if opts.Mode == export.Full {
		mode = "full"
	}
	lr := state.LastRun{Status: state.RunRunning, Started: time.Now().UTC(), PID: os.Getpid(), Exe: exe, Mode: mode, Job: canonicalJob(opts, mode)}
	_ = state.WriteLastRun(opts.Out, lr) // best effort: the run itself must not fail on this
	return lr
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

// runFile opens one data file (optionally via a temp snapshot) and exports every
// message it contains, honouring ctx cancellation between messages.
func runFile(ctx context.Context, exp *export.Exporter, path, out string, copyFirst bool, logger *log.Logger, onProgress ProgressFunc) error {
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
	// The token scopes this source's identity and names its on-disk tree. It is
	// computed once from the ORIGINAL input path (never the -copy-first
	// snapshot, whose name changes every run) so two sources with the same
	// display name get distinct, sticky trees (F1). Injective and persisted.
	token := exp.Manifest.Token(path, store)
	logger.Printf("Reading %s (store: %s)", path, store)

	return reader.Walk(func(folderPath []string, m *model.Message) (err error) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
