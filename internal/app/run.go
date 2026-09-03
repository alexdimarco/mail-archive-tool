// Package app orchestrates a full export run: input discovery, per-file reading
// (with optional snapshotting), and manifest-tracked writing. Both the CLI and
// the GUI drive the exporter through Run so behaviour stays identical.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

// finish fills the manifest-derived fields of a Result, regenerates the
// verification report (R1) and the archive README. Shared by the local and
// Graph runners.
func finish(out string, r *Result, exp *export.Exporter, manifest *state.Manifest, idx *index.Index, indexErrors int, logger *log.Logger) {
	writeArchiveReadme(out, logger)
	r.Stats = exp.Stats
	r.ManifestSize = manifest.Len()
	r.Indexed = indexCount(idx)
	r.IndexErrors = indexErrors
	r.Fillable, r.Terminal, r.Unknown = manifest.Counts()
	r.ReportPath, r.Issues = writeReport(out, manifest, manifest.Migrated > 0, logger)
	if r.Issues > 0 {
		logger.Printf("Verification: %d finding(s) recorded in %s (fillable=%d terminal=%d unknown=%d)",
			r.Issues, r.ReportPath, r.Fillable, r.Terminal, r.Unknown)
	}
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

	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output dir: %w", err)
	}
	// One run per archive at a time (R5): a scheduled run overlapping a manual
	// one would otherwise interleave manifest/index/file writes.
	lock, err := lockfile.Acquire(filepath.Join(opts.Out, lockfile.Name))
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	// Every run that begins is recorded (R18): "running" now, before any early
	// return, finalized on the way out — so a run that never finishes is
	// visible as such to `status`.
	defer recordRun(opts, beginRun(opts), &result, &err)

	files, err := DiscoverInputs(opts.Inputs, opts.Auto)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, errors.New("no input mail sources found (a .pst/.ost file, an mbox file, or a mail directory; or enable auto-discovery)")
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
	// otherwise leave hours of work unrecorded.
	every := opts.CheckpointEvery
	if every <= 0 {
		every = defaultCheckpointEvery
	}
	lastCheckpoint := 0
	checkpoint := func(stats export.Stats) {
		if stats.Exported-lastCheckpoint < every {
			return
		}
		lastCheckpoint = stats.Exported
		if saveErr := manifest.Save(); saveErr != nil {
			logger.Printf("warning: could not checkpoint manifest: %v", saveErr)
		}
		if idx != nil {
			if flushErr := idx.Flush(); flushErr != nil {
				logger.Printf("warning: index checkpoint: %v", flushErr)
			}
		}
	}

	result = Result{Files: len(files)}
	var failures int
	for _, f := range files {
		runErr := runFile(ctx, exp, f, opts.CopyFirst, logger, func(stats export.Stats) {
			checkpoint(stats)
			if onProgress != nil {
				onProgress(stats)
			}
		})

		if saveErr := manifest.Save(); saveErr != nil {
			logger.Printf("warning: could not save manifest: %v", saveErr)
		}
		if idx != nil {
			if flushErr := idx.Flush(); flushErr != nil {
				logger.Printf("warning: index flush: %v", flushErr)
			}
		}

		if errors.Is(runErr, context.Canceled) {
			finish(opts.Out, &result, exp, manifest, idx, indexErrors, logger)
			return result, context.Canceled
		}
		if runErr != nil {
			failures++
			logger.Printf("error: %s: %v", f, runErr)
		}
	}

	// Browsable folder index pages, built from the finished index.
	if idx != nil && opts.Pages {
		if pErr := pages.Generate(opts.Out, idx, logger); pErr != nil {
			logger.Printf("warning: folder pages: %v", pErr)
		}
	}

	finish(opts.Out, &result, exp, manifest, idx, indexErrors, logger)
	if failures > 0 {
		return result, fmt.Errorf("%d file(s) failed", failures)
	}
	return result, nil
}

// defaultCheckpointEvery aligns with the index's own batch size.
const defaultCheckpointEvery = 1000

// beginRun writes the "running" last-run record and returns it for recordRun.
func beginRun(opts Options) state.LastRun {
	exe, _ := os.Executable()
	mode := "incremental"
	if opts.Mode == export.Full {
		mode = "full"
	}
	lr := state.LastRun{Status: state.RunRunning, Started: time.Now().UTC(), PID: os.Getpid(), Exe: exe, Mode: mode}
	_ = state.WriteLastRun(opts.Out, lr) // best effort: the run itself must not fail on this
	return lr
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
func runFile(ctx context.Context, exp *export.Exporter, path string, copyFirst bool, logger *log.Logger, onProgress ProgressFunc) error {
	openPath := path
	if copyFirst {
		// Snapshotting only applies to single files (e.g. a locked .ost);
		// mail-store directories are read in place.
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			snap, cleanup, err := snapshot(path)
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
		if _, exportErr := exp.Export(store, folderPath, m); exportErr != nil {
			return exportErr
		}
		if onProgress != nil {
			onProgress(exp.Stats)
		}
		return nil
	})
}

// snapshot copies src to a temporary file, returning its path and a cleanup func.
func snapshot(src string) (string, func(), error) {
	in, err := os.Open(src)
	if err != nil {
		return "", nil, fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp("", "mailarchive-*"+filepath.Ext(src))
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
