// Package app orchestrates a full export run: input discovery, per-file reading
// (with optional snapshotting), and manifest-tracked writing. Both the CLI and
// the GUI drive the exporter through Run so behaviour stays identical.
package app

import (
	"bufio"
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
}

// Result summarizes a completed (or cancelled) run.
type Result struct {
	Stats        export.Stats
	Files        int
	ManifestSize int
	Indexed      int    // messages in the search index (0 if indexing disabled)
	Issues       int    // verification findings (referenced-but-not-exported)
	ReportPath   string // path to the verification report, if any issues
}

// ProgressFunc, if provided, is called after each processed message with the
// running stats. Keep it cheap; it runs inline with the export loop.
type ProgressFunc func(stats export.Stats)

// Run performs the export described by opts. If ctx is cancelled it stops
// promptly, saves the manifest, and returns the partial Result with
// context.Canceled.
func Run(ctx context.Context, opts Options, logger *log.Logger, onProgress ProgressFunc) (Result, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

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
	}

	// Optional search index, fed as each message is written.
	var idx *index.Index
	if opts.Index {
		idxPath := filepath.Join(opts.Out, "search.db")
		idx, err = index.Open(idxPath)
		if err != nil {
			return Result{}, fmt.Errorf("open search index: %w", err)
		}
		defer idx.Close()
		exp.OnExported = func(store string, folderPath []string, m *model.Message, relPath, key string) {
			if addErr := idx.Add(store, folderPath, m, relPath, key); addErr != nil {
				logger.Printf("warning: index: %v", addErr)
			}
		}
	}

	result := Result{Files: len(files)}
	var failures int
	for _, f := range files {
		runErr := runFile(ctx, exp, f, opts.CopyFirst, logger, onProgress)

		if saveErr := manifest.Save(); saveErr != nil {
			logger.Printf("warning: could not save manifest: %v", saveErr)
		}
		if idx != nil {
			if flushErr := idx.Flush(); flushErr != nil {
				logger.Printf("warning: index flush: %v", flushErr)
			}
		}

		if errors.Is(runErr, context.Canceled) {
			result.Stats = exp.Stats
			result.ManifestSize = manifest.Len()
			result.Indexed = indexCount(idx)
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

	// Verification report: anything referenced but not fully exported.
	if len(exp.Issues) > 0 {
		reportPath := filepath.Join(opts.Out, "attachments-report.tsv")
		if wErr := writeIssuesReport(reportPath, exp.Issues); wErr != nil {
			logger.Printf("warning: could not write verification report: %v", wErr)
		} else {
			result.ReportPath = reportPath
			logger.Printf("Verification: %d attachment/inline issue(s) recorded in %s", len(exp.Issues), reportPath)
		}
	}

	result.Stats = exp.Stats
	result.ManifestSize = manifest.Len()
	result.Indexed = indexCount(idx)
	result.Issues = len(exp.Issues)
	if failures > 0 {
		return result, fmt.Errorf("%d file(s) failed", failures)
	}
	return result, nil
}

// writeIssuesReport writes the verification findings as a TSV that opens in any
// spreadsheet, one row per referenced-but-not-exported item.
func writeIssuesReport(path string, issues []export.Issue) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "kind\tfolder\tdate\tsubject\tdetail\tpath")
	for _, is := range issues {
		date := ""
		if !is.Date.IsZero() {
			date = is.Date.UTC().Format("2006-01-02")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			is.Kind, tsv(is.Folder), date, tsv(is.Subject), tsv(is.Detail), is.RelPath)
	}
	return w.Flush()
}

func tsv(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\t", " "), "\n", " ")
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
// default Outlook locations into a deduplicated list of data files.
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
// (Windows) and Thunderbird account/store directories (Linux/macOS/Windows).
func autoDiscover() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	// Outlook data files.
	for _, g := range outlookGlobs() {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			add(m)
		}
	}

	// Thunderbird account/store directories (only those that really are stores).
	for _, g := range thunderbirdGlobs() {
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

func isDataFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".pst" || ext == ".ost"
}
