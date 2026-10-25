// Command mailarchive archives mail from many sources — Outlook .pst/.ost,
// Thunderbird mbox/maildir, Evolution (Maildir++/IMAP cache), and Microsoft 365
// via Graph — into a directory of self-contained HTML files with per-email
// attachment archives, mirroring the source folder tree, and builds a full-text
// search index for discovery.
//
// Subcommands:
//
//	mailarchive [flags]         export local mail sources (default)
//	mailarchive serve  [flags]  start the local search + reader web UI
//	mailarchive search [flags] QUERY...   full-text search from the terminal
//	mailarchive reindex  [flags] reconcile the archive with what is on disk
//	mailarchive schedule [flags] print/install a recurring-backup entry
//	mailarchive graph  [flags]  archive Microsoft 365 mailboxes via Graph (app-only)
//	mailarchive status [flags]  report an archive's completeness, last run and schedule
//	mailarchive verify [flags]  check archived files against their recorded fixity
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/runlog"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

func main() {
	args := os.Args[1:]
	// R33 — one program: a GUI surface (desktop/wizard/run-job), a legacy
	// schedule argv, or a start under a former name routes in-process here and
	// never falls to the export dispatch (MA-610/MA-611); anything else is a CLI
	// verb or an export and falls through.
	if code, handled := runResolvedVerb(invokedBase(), args); handled {
		os.Exit(code)
	}
	var err error
	switch {
	case len(args) > 0 && args[0] == "serve":
		err = runServe(args[1:])
	case len(args) > 0 && args[0] == "search":
		err = runSearch(args[1:])
	case len(args) > 0 && args[0] == "reindex":
		err = runReindex(args[1:])
	case len(args) > 0 && args[0] == "schedule":
		err = runSchedule(args[1:])
	case len(args) > 0 && args[0] == "graph":
		err = runGraph(args[1:])
	case len(args) > 0 && args[0] == "setup":
		err = runSetup(args[1:])
	case len(args) > 0 && args[0] == "status":
		err = runStatus(args[1:])
	case len(args) > 0 && args[0] == "verify":
		err = runVerify(args[1:])
	case len(args) > 0 && args[0] == "extract":
		err = runExtract(args[1:])
	case len(args) > 0 && args[0] == "pause":
		err = runPause(args[1:])
	case len(args) > 0 && args[0] == "resume":
		err = runResume(args[1:])
	case len(args) > 0 && args[0] == "uninstall":
		err = runUninstall(args[1:])
	default:
		err = runExport(args)
	}
	if err != nil {
		// verify's "not attested" verdict carries its own exit code and has
		// already printed its report; it is not a refusal, so it gets no
		// "mailarchive:" prefix. Everything else is a refusal/error (exit 1).
		var ee *exitError
		if !errors.As(err, &ee) {
			fmt.Fprintln(os.Stderr, "mailarchive: "+err.Error())
		}
		os.Exit(exitCode(err))
	}
}

// exitError carries a specific, non-1 process exit code for a command whose
// result (not a refusal) is machine-read from the exit status — `verify`'s
// "not attested" is exit 2 (docs/ux-contract.md X1). The report is printed
// before it is returned, so main only needs the code.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// exitCode is the process exit status for a verb's result (docs/ux-contract.md
// X1): 0 for success, an exitError's own code, and 1 for any other error, a
// refusal or a failed run.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

// stringSlice is a repeatable / comma-separated string flag.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

func (s *stringSlice) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*s = append(*s, p)
		}
	}
	return nil
}

func runExport(args []string) error {
	fs := flag.NewFlagSet("mailarchive", flag.ContinueOnError)
	fs.Usage = exportUsage(fs)
	o := exportFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs := append(o.inputs, fs.Args()...)

	if *o.out == "" {
		return errors.New("-out is required (or use a subcommand: serve, search, reindex, schedule, graph, status, verify, extract, pause, resume, uninstall)")
	}
	if *o.unattended {
		if err := requireExistingOut(*o.out); err != nil {
			return err
		}
	}
	mode, err := parseMode(*o.mode)
	if err != nil {
		return err
	}
	var since time.Time
	if *o.since != "" {
		since, err = util.ParseSince(*o.since, time.Now())
		if err != nil {
			return err
		}
	}

	// An encrypted archive refuses a keyless run first — before discovery, whose own
	// refusal writes the -log and a last run naming the sources, and before the run
	// log and the -outlook PST export write into it in plaintext (phase2a §5,
	// P2a-C9). A scheduled run's refusal is still recorded (A14). -list only reads
	// the sources.
	if !*o.list {
		if kerr := app.RefuseKeyless("archive into it", *o.out, manifestFolder(*o.manifest)); kerr != nil {
			if *o.unattended {
				app.RecordRefusedRun(app.Options{Out: *o.out, Unattended: true}, kerr)
			}
			return kerr
		}
	}

	// Discover the inputs up front (friction #2), BEFORE any output dir, run log,
	// lock or last-run record exists: a missing -input path (or no sources at
	// all) refuses here, naming the problem, having created nothing. -list
	// previews the discovered stores and stops. -outlook supplies its own inputs
	// later, so an empty set is not yet a refusal in that case.
	discovered, err := app.DiscoverInputs(inputs, *o.auto)
	if err == nil {
		// An opt-in that names no input would be read as no opt-in, so it is refused here.
		err = app.FollowLinksRefusal(o.followLinks, discovered)
	}
	if err == nil && *o.list {
		return listStores(discovered)
	}
	if err == nil && len(discovered) == 0 && !*o.outlook {
		err = errors.New("no input mail sources found: pass -input PATH (a .pst/.ost file, an mbox file, or a mail directory), or -auto to discover Outlook/Thunderbird/Evolution stores")
	}
	if err != nil {
		// A scheduled run's refusal is still a run (N13, R18): on an archive
		// that already exists it is recorded failed (status, attention
		// sidecar) and logged to its own -log, instead of leaving the previous
		// success as the archive's last word. A first run still creates
		// nothing (RecordRefusedRun writes only into an existing archive).
		if *o.unattended && !*o.list {
			app.RecordRefusedRun(exportOptions(o, inputs, mode, since), err)
			if *o.log != "" {
				if l, closeLog, lerr := newRunLogger(*o.log); lerr == nil {
					l.Printf("FAILED: %v", err)
					closeLog()
				}
			}
		}
		return err
	}

	// A paused (sealed) archive is hard read-only: refuse at entry — BEFORE the run log is
	// opened and BEFORE the -outlook COM export recreates <out>/_outlook-pst inside the
	// archive, both of which precede the engine's own seal check in app.Run (too late for
	// these pre-lock writes). Mirrors the scheduled path (wizard.RunHeadless) and
	// HeadlessCapture (S2-C1/C2).
	if serr := seal.RefuseWrite(*o.out); serr != nil {
		return serr
	}

	logger, closeLog, err := newRunLogger(*o.log)
	if err != nil {
		return err
	}
	defer closeLog()
	defer func() { logFailure(logger, err) }() // a failed unattended run leaves its reason in its own log too

	outlook, outlookSyncWait, auto := o.outlook, o.outlookSyncWait, o.auto
	doIndex, keepRaw := o.index, o.keepRaw
	enableOffline, syncWait, modeStr := o.enableOffline, o.syncWait, o.mode
	out := o.out

	// Optionally have Outlook itself export each account to a fresh .pst, then
	// archive those. This is the reliable path for a live Exchange/IMAP .ost cache
	// go-pst can't read directly. Windows + classic Outlook only; elsewhere it
	// refuses with a legible message.
	pstDir := ""
	var outlookSkips []outlookcom.Skip
	var outlookExported []string // the PSTs this run's export wrote: a fresh image of the mailbox (NF1)
	if *outlook {
		// Refuse up front on a platform that can't run Outlook automation, before
		// printing the completeness note or any "exporting…" progress — a guaranteed
		// refusal must not first look like it started work.
		if runtime.GOOS != "windows" {
			return fmt.Errorf("%w", outlookcom.ErrUnsupported)
		}
		logger.Printf("%s", outlookcom.CompletenessNote)
		pstDir = filepath.Join(*out, "_outlook-pst")
		logger.Printf("Outlook: exporting each account to a PST under %s ...", pstDir)
		stores, skips, cerr := outlookcom.CreatePSTs(pstDir,
			outlookcom.Options{Sync: *outlookSyncWait > 0, SyncWait: *outlookSyncWait}, logger)
		if cerr != nil {
			return cerr
		}
		for _, s := range stores {
			inputs = append(inputs, s.Path)
			outlookExported = append(outlookExported, s.Path)
		}
		outlookSkips = skips
		logger.Printf("Outlook: %d PST(s) ready; archiving them now.", len(stores))
	}

	opts := exportOptions(o, inputs, mode, since)
	opts.OutlookSkips = outlookSkips // what Outlook's export left out is recorded as a gap, never only logged (FC28)
	opts.OutlookExported = outlookExported

	// Optional Thunderbird pre-sync (macOS and Windows): start Thunderbird (hidden on
	// macOS; briefly visible then minimized on Windows), wait for its mail files to go
	// quiet, ask it to quit, THEN read. It runs as app.Run's BeforeRead hook (after the
	// lock + running record, before any source is read) and never gates the archive.
	// On a platform without an adapter it refuses here (setPresyncHook).
	if *o.tbSync {
		// R32 (adversarial finding A5): a pre-sync SETUP problem never gates the
		// archive — log it and run without the sync, the same swallow the GUI's
		// optsForJob does, so R32 cannot drift between the CLI and GUI surfaces. The
		// unsupported-platform refusal is enforced earlier, at flag-parse time (job.go).
		if err := setPresyncHook(&opts, *o.tbProfile); err != nil {
			fmt.Fprintf(os.Stderr, "warning: Thunderbird pre-sync not set up (%v); archiving without it\n", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Thunderbird IMAP prep: enable offline storage and/or wait for a sync so
	// on-demand mail is actually present locally before we read it.
	if *enableOffline || *syncWait {
		if err := prepareThunderbird(ctx, inputs, *auto, *enableOffline, *syncWait, logger); err != nil {
			return err
		}
	}

	logger.Printf("Exporting to %s (mode=%s)", *out, *modeStr)
	if !since.IsZero() {
		logger.Printf("Date filter: items on or after %s", since.Format(time.RFC3339))
	}

	result, runErr := app.Run(ctx, opts, logger, nil)
	// The "Done. …" summary (and its -raw/verification lines) belongs to a run
	// that reached completion — a clean finish, a saved interrupt, or a partial
	// run (its walk finished; the "Not captured" block says what it could not
	// capture). A run that FAILED (refused before the lock, or aborted mid-walk)
	// prints only the "FAILED:" line, never a misleading "Done. exported=0 …
	// manifest=0" first (friction #7b/#22).
	if printsSummary(runErr) {
		printSummary(logger, result, *doIndex, *keepRaw, *out)
	}

	// The -outlook COM path builds a full, mailbox-sized PST copy under
	// <out>/_outlook-pst purely to feed the pipeline. On a clean run reclaim it
	// (it rebuilds next run); on a failure leave it for inspection/retry (P2).
	if pstDir != "" {
		if n, ok := reclaimPSTDir(pstDir, runErr); ok {
			logger.Printf("Reclaimed %s by removing the temporary Outlook PST copy (%s).", thunderbird.HumanBytes(n), pstDir)
		}
	}

	if errors.Is(runErr, context.Canceled) {
		logger.Printf("interrupted; progress saved to the manifest")
		return nil
	}
	err = runErr
	return err
}

// exportOptions builds the app.Options an export run (or its recorded refusal)
// uses from the parsed flags and the final input list.
func exportOptions(o *exportOpts, inputs []string, mode export.Mode, since time.Time) app.Options {
	return app.Options{
		Inputs:      inputs,
		Auto:        *o.auto,
		Out:         *o.out,
		Mode:        mode,
		Since:       since,
		CopyFirst:   *o.copyFirst,
		FollowLinks: o.followLinks,
		Manifest:    *o.manifest,
		Index:       *o.index,
		Pages:       *o.pages,
		KeepRaw:     *o.keepRaw,
		Unattended:  *o.unattended,
	}
}

// reclaimPSTDir removes the temporary per-account PST directory that -outlook's
// COM export built under the archive, once the run succeeded, and returns the
// bytes reclaimed. A failed or cancelled run (runErr != nil) leaves it in place
// so a retry can reuse or inspect it. The bool reports whether it was removed.
func reclaimPSTDir(pstDir string, runErr error) (int64, bool) {
	if runErr != nil {
		return 0, false
	}
	n := dirSize(pstDir)
	if err := os.RemoveAll(pstDir); err != nil {
		return 0, false
	}
	return n, true
}

// dirSize sums the sizes of the regular files under dir (0 if it does not exist).
func dirSize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// listStores prints, one per line, the mail stores an export would archive with
// a rough on-disk size — the -list preview. It creates nothing and exits 0.
func listStores(files []string) error {
	if len(files) == 0 {
		fmt.Println("No mail stores found to archive.")
		return nil
	}
	for _, f := range files {
		line := fmt.Sprintf("%8s  %s", thunderbird.HumanBytes(dirSize(f)), f)
		// Show the friendly store label beside an opaque path when discovery can
		// derive one — most usefully an Evolution IMAP cache dir, whose on-disk
		// name is an account-UID hash, not the account it belongs to (friction #21).
		if label := storeLabel(f); label != "" && label != filepath.Base(f) {
			line += "  (" + label + ")"
		}
		fmt.Println(line)
	}
	return nil
}

// storeLabel returns the human-readable store name a discovered directory source
// would archive under (Evolution resolves an IMAP cache UID to its account name),
// or "" when the path is not a directory store or cannot be opened. It is
// best-effort and side-effect-free: it opens only directory sources (never a
// heavyweight .pst/.ost parse) and creates nothing, so -list still writes nothing.
func storeLabel(path string) string {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return ""
	}
	s, err := source.Open(path)
	if err != nil {
		return ""
	}
	defer s.Close()
	return s.StoreName()
}

// manifestFolder is the folder of a -manifest override, or "" for none: a
// relocated manifest's folder must not be an encrypted archive either.
func manifestFolder(manifest string) string {
	if manifest == "" {
		return ""
	}
	return filepath.Dir(manifest)
}

// newRunLogger returns the operator logger: stderr by default, or the
// size-capped run log at path when -log was given (what scheduled jobs use).
func newRunLogger(path string) (*log.Logger, func(), error) {
	if path == "" {
		return log.New(os.Stderr, "", 0), func() {}, nil
	}
	f, err := runlog.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return log.New(f, "", log.LstdFlags), func() { f.Close() }, nil
}

var markTags = regexp.MustCompile(`</?mark>`)

// scrubTTY maps every C0 control, DEL and C1 control in mail-derived text to a
// space before it reaches the terminal, so a hostile Subject, sender name or
// body snippet cannot inject ANSI/OSC escape sequences into the operator's
// terminal when they run `search`. This mirrors the control-strip the archive
// already applies wherever it echoes untrusted mail-derived text
// (internal/app/report.go, internal/schedule/descriptor.go, internal/lockfile).
// The machine formats are unaffected: -json escapes control bytes and -paths
// prints only tool-made paths.
func scrubTTY(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}

func runSearch(args []string) error {
	fs := flag.NewFlagSet("mailarchive search", flag.ContinueOnError)
	out := fs.String("out", ".", "export directory to search (contains search.db)")
	limit := fs.Int("limit", 20, "maximum results")
	folder := fs.String("folder", "", "restrict to a folder (and its subfolders); a folder: token in the query overrides this")
	sender := fs.String("sender", "", "restrict to a sender (substring); a from: token in the query overrides this")
	afterStr := fs.String("after", "", "only items on/after this date (YYYY, YYYY-MM or YYYY-MM-DD); an after: token overrides this")
	beforeStr := fs.String("before", "", "only items before this date (YYYY, YYYY-MM or YYYY-MM-DD); a before: token overrides this")
	attach := fs.Bool("attach", false, "only items with attachments")
	asJSON := fs.Bool("json", false, "print matches as a JSON array on stdout (the N-match line goes to stderr; snippets carry no <mark>)")
	asPaths := fs.Bool("paths", false, "print one archive-relative path per match on stdout (the N-match line goes to stderr)")
	nul := fs.Bool("0", false, "with -paths, separate paths with NUL instead of newline (for xargs -0)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *asJSON && *asPaths {
		return errors.New("-json and -paths cannot be combined; pick one machine format")
	}

	base := index.Query{
		Folder:    *folder,
		Sender:    *sender,
		HasAttach: *attach,
		Limit:     *limit,
	}
	if t, ok := index.ParseDate(*afterStr); ok {
		base.After = t
	}
	if t, ok := index.ParseDate(*beforeStr); ok {
		base.Before = t
	}
	// The positional args carry the same inline grammar as the serve box: a
	// from:/folder:/after:/before:/has: token here means exactly what it means
	// there, and overrides the matching flag.
	q := index.ParseQuery(strings.Join(fs.Args(), " "), base)

	// -paths would print archive paths that do not exist on an encrypted archive's
	// disk, and its index is not readable keyless: refuse (phase2a §5).
	if kerr := app.RefuseKeyless("search it", *out); kerr != nil {
		return kerr
	}
	ix, err := index.OpenReadonly(filepath.Join(*out, "search.db"))
	if err != nil {
		return err
	}
	defer ix.Close()

	results, total, err := ix.Search(q)
	if err != nil {
		return err
	}

	switch {
	case *asJSON:
		return emitSearchJSON(results, total, len(results))
	case *asPaths:
		return emitSearchPaths(results, total, len(results), *nul)
	}

	fmt.Printf("%d match(es)%s:\n\n", total, moreNote(total, len(results)))
	for _, r := range results {
		date := "          "
		if !r.Date.IsZero() {
			date = r.Date.Format("2006-01-02")
		}
		// Every mail-derived field is control-scrubbed before it reaches the
		// terminal (a hostile Subject/body/sender must not inject ANSI/OSC).
		from := scrubTTY(r.SenderName)
		if from == "" {
			from = scrubTTY(r.SenderEmail)
		}
		subject := scrubTTY(r.Subject)
		if subject == "" {
			subject = "(no subject)"
		}
		fmt.Printf("%s  %-28.28s  %s\n", date, from, scrubTTY(r.Folder))
		fmt.Printf("    %s\n", subject)
		if r.Snippet != "" {
			fmt.Printf("    %s\n", scrubTTY(markTags.ReplaceAllString(r.Snippet, "")))
		}
		fmt.Printf("    -> %s\n\n", r.Path)
	}
	return nil
}

// emitSearchJSON writes the matches as a JSON array on stdout (only data), the
// N-match line on stderr (X8). Each Snippet keeps its HTML-escaped text but
// loses the index's <mark> highlight tags, which are a UI concern.
func emitSearchJSON(results []index.Result, total, shown int) error {
	out := make([]index.Result, len(results))
	copy(out, results)
	for i := range out {
		out[i].Snippet = markTags.ReplaceAllString(out[i].Snippet, "")
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d match(es)%s\n", total, moreNote(total, shown))
	return nil
}

// emitSearchPaths writes one archive-relative path per match on stdout (only
// data), separated by newline or NUL, the N-match line on stderr (X8).
func emitSearchPaths(results []index.Result, total, shown int, nul bool) error {
	w := bufio.NewWriter(os.Stdout)
	sep := byte('\n')
	if nul {
		sep = 0
	}
	for _, r := range results {
		if _, err := w.WriteString(r.Path); err != nil {
			return err
		}
		if err := w.WriteByte(sep); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d match(es)%s\n", total, moreNote(total, shown))
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func moreNote(total, shown int) string {
	if total > shown {
		return fmt.Sprintf(" (showing %d)", shown)
	}
	return ""
}

// runReindex reconciles the archive at -out with what is on disk: rows whose
// exported file was deleted/moved are pruned from the index and manifest, and
// the folder pages are regenerated.
func runReindex(args []string) (err error) {
	fs := flag.NewFlagSet("mailarchive reindex", flag.ContinueOnError)
	o := reindexFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *o.out == "" {
		return errors.New("-out is required (the export directory to reconcile)")
	}
	// Before the run log opens: on an encrypted archive reindex would prune every
	// record (phase2a §5).
	if kerr := app.RefuseKeyless("reindex it", *o.out); kerr != nil {
		return kerr
	}

	logger, closeLog, err := newRunLogger(*o.log)
	if err != nil {
		return err
	}
	defer closeLog()
	if *o.rebuild {
		rep, err := app.Rebuild(*o.out, logger)
		if err != nil {
			logger.Printf("FAILED: %v", err)
			return err
		}
		logger.Printf("rebuilt=%d (from-eml=%d re-derived=%d unrecovered-fields=%d) pruned=%d",
			rep.Rebuilt, rep.FromEML, rep.FromHTML, rep.Unrecovered, rep.Pruned)
		return nil
	}
	kept, pruned, err := app.Reindex(*o.out, logger)
	if err != nil {
		logger.Printf("FAILED: %v", err)
		return err
	}
	logger.Printf("reindexed: kept=%d pruned=%d", kept, pruned)
	return nil
}

// runVerify checks the archive at -out against its recorded fixity and prints a
// report. It exits 0 when the archive is attested (every recorded file checked
// and intact, none unrecorded), 2 when it is not attested (any modified,
// missing or unrecorded file — each named), and 1 on refusal or error (no
// manifest, a locked archive, an unreadable manifest). `unexpected` files are
// reported, not an exit condition (docs/ux-contract.md X1). Verify holds the
// archive lock for its whole duration, so run it outside the backup window.
func runVerify(args []string) error {
	fs := flag.NewFlagSet("mailarchive verify", flag.ContinueOnError)
	fs.Usage = verifyUsage(fs)
	o := verifyFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *o.out == "" {
		return errors.New("-out is required (the archive directory to verify)")
	}
	if *o.unattended {
		if err := requireExistingOut(*o.out); err != nil {
			return err
		}
	}
	if kerr := app.RefuseKeyless("verify it", *o.out); kerr != nil {
		return kerr
	}
	logger, closeLog, err := newRunLogger(*o.log)
	if err != nil {
		return err
	}
	defer closeLog()

	// Ctrl-C or a terminate says what happened (F10); the running record is
	// left as a kill leaves it, so status reports the verify as never finished.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	stopWatch := watchVerifyInterrupt(sigs, os.Stderr, abspath(*o.out), os.Exit)
	defer func() { signal.Stop(sigs); stopWatch() }()

	rep, verr := app.Verify(abspath(*o.out), app.VerifyOptions{Record: *o.record}, logger, nil)
	if verr != nil {
		if *o.log != "" { // a scheduled run leaves its reason in its own log
			logger.Printf("FAILED: %v", verr)
		}
		return verr
	}
	if *o.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		for _, line := range app.VerifySummary(rep) {
			fmt.Println(line)
		}
	}
	if *o.log != "" {
		logger.Printf("verify: %s", app.VerifyResultLine(rep))
	}
	if code := rep.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

// watchVerifyInterrupt waits for a signal on sigs and, when one arrives, prints
// one line to w saying the verify did not finish and what still stands, then
// calls exit with the shell's code for that signal (130 for Ctrl-C, 143 for
// terminate). The returned func stops the watcher and waits for it, so a verify
// that finished normally prints nothing.
func watchVerifyInterrupt(sigs <-chan os.Signal, w io.Writer, out string, exit func(int)) (stop func()) {
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case s := <-sigs:
			fmt.Fprintf(w, "mailarchive: verify interrupted: the archive was not fully checked and no new verdict was recorded; the last finished verdict still stands (see `mailarchive status`). Run `mailarchive verify -out %q` again to finish.\n", out)
			if s == syscall.SIGTERM {
				exit(143)
			} else {
				exit(130)
			}
		case <-quit:
		}
	}()
	return func() { close(quit); <-done }
}

func verifyUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive verify - check archived files against their recorded fixity

Usage:
  mailarchive verify -out DIR [-json] [-record]

Re-hashes every archived file the manifest records and reports each as ok,
modified, missing or unrecorded, plus any stray file as unexpected. Fixity is
detected for files written by this version or later, or baselined with -record.

Exit: 0 attested (every recorded file checked and intact, none unrecorded);
2 not attested (any modified/missing/unrecorded, each named); 1 refusal/error.

verify holds the archive's exclusive lock for its whole run, so a scheduled
backup that fires meanwhile refuses — run verify OUTSIDE the backup window.

Flags:
`)
		fs.PrintDefaults()
	}
}

// runExtract migrates the archive out: it copies every record's preserved
// original bytes (`<stem>.eml`) into -dest as standard interchange (mboxrd, one
// file per folder; or byte-exact .eml, one per message), faithfully or not at
// all. It exits 0 when the whole set was emitted, the partial code (3) when any
// record had no preserved bytes to emit (including a PST-only archive, where
// nothing is extractable), and 1 on refusal/error. extract holds the archive's
// exclusive lock for its whole run, so run it OUTSIDE the backup window.
func runExtract(args []string) error {
	fs := flag.NewFlagSet("mailarchive extract", flag.ContinueOnError)
	fs.Usage = extractUsage(fs)
	o := extractFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *o.out == "" {
		return errors.New("-out is required (the archive directory to extract from)")
	}
	format := app.ExtractFormat(strings.ToLower(strings.TrimSpace(*o.format)))
	if format != app.FormatMbox && format != app.FormatEML {
		return fmt.Errorf("-format is required and must be mbox or eml (got %q)", *o.format)
	}
	if *o.dest == "" {
		return errors.New("-dest is required (an empty directory to write the extracted mail into; it must not overlap -out)")
	}
	if kerr := app.RefuseKeyless("extract from it", *o.out, *o.dest); kerr != nil {
		return kerr
	}

	logger, closeLog, err := newRunLogger(*o.log)
	if err != nil {
		return err
	}
	defer closeLog()

	rep, xerr := app.Extract(abspath(*o.out), format, *o.dest, *o.overwrite, logger, nil)
	if xerr != nil {
		if *o.log != "" { // a piped run leaves its reason in its own log
			logger.Printf("FAILED: %v", xerr)
		}
		return xerr
	}
	for _, line := range app.ExtractSummary(rep) {
		fmt.Println(line)
	}
	if code := rep.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

func extractUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive extract - migrate the archive out as standard mail (mbox/eml)

Usage:
  mailarchive extract -out DIR -format mbox|eml -dest OUTDIR [--overwrite]

Copies each message's PRESERVED original bytes (the <stem>.eml kept with -raw)
into -dest, faithfully or not at all: a record with no preserved bytes (every
PST/OST item; any archive captured without -raw) is counted and listed, never
synthesized. -format mbox writes one mboxrd file per folder (a reader that
unquotes ">From " recovers the exact bytes); -format eml writes one byte-exact
.eml per message, mirroring the folder tree — the format for a consumer of
unknown flavour. -dest must be empty (or pass --overwrite), must not overlap
-out, and needs room for the full .eml volume this copies.

Exit: 0 the whole set emitted; 3 partial (some records had no preserved bytes,
including a PST-only archive where nothing is extractable); 1 refusal/error.

extract holds the archive's exclusive lock for its whole run, so a scheduled
backup that fires meanwhile refuses — run extract OUTSIDE the backup window. It
is an operator-driven migration, not a backup, and cannot be scheduled.

Flags:
`)
		fs.PrintDefaults()
	}
}

// runSchedule prints (default) or installs/removes a recurring-backup entry for
// the host OS's scheduler. Two forms: the flat form (`schedule -out DIR -auto
// …` — the export job's flags on the schedule command) and the job form
// (`schedule [-interval …] -- <mailarchive job>` for an export, graph or reindex
// job). Either way the job is validated NOW through the real flag definitions
// (parseJob), so what cannot run unattended is refused at schedule time.
func runSchedule(args []string) error {
	// Split at "--": schedule flags before, the job after.
	var jobArgs []string
	hasJob := false
	for i, a := range args {
		if a == "--" {
			jobArgs, args, hasJob = args[i+1:], args[:i], true
			break
		}
	}

	fs := flag.NewFlagSet("mailarchive schedule", flag.ContinueOnError)
	fs.Usage = scheduleUsage(fs)
	interval := fs.String("interval", "daily", "backup cadence: hourly|daily|weekly")
	at := fs.String("at", "02:00", "time of day HH:MM (hourly uses only the minute)")
	name := fs.String("name", "", "scheduler entry name (default: mailarchive-<hash of the archive path>, one schedule per archive)")
	install := fs.Bool("install", false, "install the schedule (default: print it without applying)")
	remove := fs.Bool("remove", false, "remove a previously installed schedule (by -name, or by the -out archive's descriptor)")
	// Flat form: the export job's flags, validated through the same definitions.
	eo := exportFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *install && *remove {
		return errors.New("choose either -install or -remove, not both")
	}
	iv, err := schedule.ParseInterval(*interval)
	if err != nil {
		return err
	}

	// The two forms are mutually exclusive: with "--" the job carries every
	// job flag, so a job flag before it is a mistake, named.
	if hasJob {
		var stray []string
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "interval", "at", "name", "install", "remove":
			default:
				stray = append(stray, "-"+f.Name)
			}
		})
		if len(stray) > 0 {
			return errors.New(strayFlagRefusal(stray, jobArgs))
		}
	} else {
		jobArgs = flatJobArgs(eo, fs.Args())
	}

	// §4.6.1 (MA-615): the program a schedule runs comes from ProgramForSchedule,
	// not os.Executable(); the command line's schedule always wraps on Windows
	// (non-goal 7, D2). A translocated program is refused here, before any job
	// file or scheduler entry is written.
	exe, progReason, err := schedule.ProgramForSchedule(true)
	if err != nil {
		return err
	}

	// Removal keys off the name, found via -name, the job's own entry name, or
	// the archive's descriptor.
	if *remove {
		out := abspath(*eo.out)
		var rj job
		if len(jobArgs) > 0 {
			if j, jerr := parseJob(jobArgs); jerr == nil {
				rj = j
				if out == "" {
					out = j.out
				}
			}
		}
		n := *name
		if n == "" && out != "" {
			switch {
			case rj.verb == "verify" && rj.out == out:
				// A verify job's entry is the archive's distinct "-verify" name,
				// exactly as -install derived it; the descriptor records the
				// archive's BACKUP schedule and must not be what this removes.
				n = defaultScheduleName(rj)
			default:
				if rn, rerr := schedule.RecordedBackupName(out); rerr == nil {
					n = rn
				} else if _, derr := schedule.ReadDescriptor(out); derr == nil {
					// The descriptor reads, but the entry it names is not this
					// archive's (a handed-over or copied folder): never act on it.
					return fmt.Errorf("-remove will not act on the schedule name %s records: %w", filepath.Join(out, schedule.DescriptorName), rerr)
				} else {
					n = schedule.DefaultNameFor(out)
				}
			}
		}
		if n == "" {
			return errors.New("-remove needs -name NAME or -out DIR (the archive whose schedule to remove)")
		}
		if n, err = schedule.SanitizeName(n); err != nil {
			return err
		}
		spec := schedule.Spec{Name: n, Interval: iv, At: *at, Exe: exe, Out: out, WrapperPath: schedule.DefaultWrapperPath(n)}
		if err := spec.Validate(); err != nil {
			return err
		}
		// Which of the archive's entries this is: a verify job after --, or the
		// name the archive's verify descriptor records (read before the removal
		// deletes it).
		kind := "backup"
		if rj.verb == "verify" || schedule.VerifyRecorded(out, n) {
			kind = "verify"
		}
		removed, err := schedule.RemoveIfInstalled(spec)
		if err != nil {
			return err
		}
		if !removed {
			if out == "" {
				fmt.Printf("no schedule was installed for %s — nothing to remove\n", n)
				return nil
			}
			// The archive may well have another schedule (its backup, or a verify
			// under another name): say which entry is absent, never that the
			// archive has none.
			fmt.Printf("no schedule %q was installed for %s — nothing to remove\n", n, out)
			if rj.verb == "verify" && *name == "" {
				fmt.Println(verifyRemoveHint(out))
			}
			return nil
		}
		fmt.Printf("Removed scheduled %s %q.\n", kind, n)
		// A descriptor that cannot be read is never deleted, whatever the entry's
		// name (nothing in it says whose it was; rulings §13.6, ESC-10), so a
		// removal names the one of its own kind it left — the backup's for a
		// backup, the verify's for a verify — with the remedy; `status` keeps
		// reporting it until the operator acts.
		left, remedy := schedule.UnreadableDescriptor(out), schedule.UnreadableDescriptorRemedy
		if kind == "verify" {
			left, remedy = schedule.UnreadableVerifyDescriptor(out), schedule.UnreadableVerifyDescriptorRemedy
		}
		if left != "" {
			fmt.Printf("Left in place: %s cannot be read, so -remove cannot tell which schedule it records — %s.\n", left, remedy)
		}
		return nil
	}

	j, err := parseJob(jobArgs)
	if err != nil {
		return err
	}
	n := *name
	if n == "" {
		// A verify job derives a distinct "-verify" name so it coexists with the
		// archive's backup schedule instead of overwriting it (friction #4).
		n = defaultScheduleName(j)
	}
	if n, err = schedule.SanitizeName(n); err != nil {
		return err
	}
	logPath := schedule.DefaultLogPath(j.out, n)
	spec := schedule.Spec{
		Name:        n,
		Interval:    iv,
		At:          *at,
		Exe:         exe,
		Args:        append(j.command(), "-log", logPath),
		Log:         logPath,
		WrapperPath: schedule.DefaultWrapperPath(n),
		Out:         j.out,
	}
	// A wrapper (and its cmd.exe window) only when the direct run string is too
	// long; the common case runs direct with no console window (MA-641).
	spec.Wrapper = schedule.WrapperNeeded(spec)
	if err := spec.Validate(); err != nil {
		return err
	}

	if *install {
		// A verify installed under a new -name leaves the entry the archive's
		// records name running (Install never removes it); read it before the
		// install rewrites the verify descriptor, so it can be named below.
		superseded := schedule.SupersededVerify(spec)
		if err := schedule.Install(spec); err != nil {
			return err
		}
		kind := "backup"
		if j.verb == "verify" {
			kind = "verify"
		}
		fmt.Printf("Installed scheduled %s %q (%s at %s).\n", kind, n, iv, *at)
		fmt.Printf("It runs: %s %s\n", exe, strings.Join(spec.Args, " "))
		if note := schedule.ProgramNote(exe, progReason); note != "" {
			fmt.Println(note)
		}
		if note := graphDeletedJunkNote(j); note != "" {
			fmt.Println(note)
		}
		desc := filepath.Join(j.out, schedule.DescriptorName)
		if j.verb == "verify" {
			desc = filepath.Join(j.out, schedule.VerifyDescriptorName) // a verify records its own
		}
		fmt.Printf("Log: %s · descriptor: %s · check with: mailarchive status -out %q\n", logPath, desc, j.out)
		if j.presync {
			fmt.Println(presyncInstallNote(n, false))
		}
		if superseded != "" {
			fmt.Printf("Note: this archive's verify was installed as %q, and that entry is still installed: it keeps running beside %q. If it is still this archive's verify, remove it with `mailarchive schedule -name %s -remove -- verify -out %q`.\n", superseded, n, superseded, j.out)
		}
		if note, _ := schedule.VerifyScheduleNote(spec); note != "" {
			fmt.Println(note)
		}
		if jobUsesOutlook(spec.Args) {
			fmt.Println(outlookReminderText)
		}
		if svc, ok := util.UnderCloudSync(j.out); ok {
			fmt.Printf("Note: %s is inside %s's synced folder — a local, non-synced folder avoids re-uploads and sync conflicts.\n", j.out, svc)
		}
		return nil
	}
	text, err := schedule.Preview(spec)
	if err != nil {
		return err
	}
	fmt.Print(text)
	if j.presync {
		fmt.Println(presyncInstallNote(n, true))
	}
	if note := graphDeletedJunkNote(j); note != "" {
		fmt.Println(note)
	}
	fmt.Printf("\nThe job's log: %s\nThis was NOT applied. Re-run with -install to schedule it, or -remove to uninstall.\n", logPath)
	return nil
}

// flatJobArgs re-assembles the export job from the flat form's flags so it can
// be validated through parseJob exactly like a -- job.
func flatJobArgs(o *exportOpts, positional []string) []string {
	var a []string
	if *o.out != "" {
		a = append(a, "-out", *o.out)
	}
	a = append(a, "-mode", *o.mode)
	if *o.since != "" {
		a = append(a, "-since", *o.since)
	}
	if *o.auto {
		a = append(a, "-auto")
	}
	for _, in := range append(o.inputs, positional...) {
		a = append(a, "-input", in)
	}
	if *o.copyFirst {
		a = append(a, "-copy-first")
	}
	for _, in := range o.followLinks {
		a = append(a, "-follow-links", in)
	}
	if *o.outlook {
		a = append(a, "-outlook", "-outlook-sync-wait", o.outlookSyncWait.String())
	}
	if !*o.index {
		a = append(a, "-index=false")
	}
	if !*o.pages {
		a = append(a, "-pages=false")
	}
	if *o.keepRaw {
		a = append(a, "-raw")
	}
	if *o.manifest != "" {
		a = append(a, "-manifest", *o.manifest)
	}
	if *o.enableOffline {
		a = append(a, "-enable-offline")
	}
	if *o.syncWait {
		a = append(a, "-sync-wait")
	}
	if *o.tbSync {
		a = append(a, "-thunderbird-sync")
		if *o.tbProfile != "" {
			a = append(a, "-thunderbird-profile", *o.tbProfile)
		}
	}
	return a
}

func abspath(p string) string {
	if p == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// parseSinceAt validates a -since value against now.
func parseSinceAt(s string, now time.Time) (time.Time, error) { return util.ParseSince(s, now) }

func scheduleUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive schedule - schedule a recurring backup with the host OS scheduler

Usage:
  mailarchive schedule -out DIR [-input ...|-auto] [-interval daily|weekly|hourly] [-at HH:MM] [-name NAME]
  mailarchive schedule -out DIR -auto -install                    apply the schedule (export job)
  mailarchive schedule [-interval ...] -install -- graph -out DIR -tenant T -client-id ID \
      -mailbox u@dom -client-secret-file FILE                     apply a Graph job
  mailarchive schedule -out DIR -remove                            uninstall this archive's schedule
  mailarchive schedule -remove -- verify -out DIR                  uninstall this archive's scheduled verify
  mailarchive schedule -name NAME -remove                          uninstall by name

The job after -- may be an export (no verb), graph, reindex, or verify job; it
is validated now, and what cannot run unattended is refused now. By default the
exact scheduler entry is printed and NOT applied. The job writes its log to
<out>/<name>.log; one schedule per archive by default.

Flags:
`)
		fs.PrintDefaults()
	}
}

// graphFake points runGraph at a fake Microsoft 365 — its Graph, token and
// device-code endpoints — and keeps a sign-in out of the OS vault. Only tests
// set it, to run `graph` in-process with no network; no flag or environment
// variable reaches it. Empty, runGraph talks to Microsoft.
var graphFake struct{ base, token, deviceAuth string }

// runGraph archives mailboxes server-side via Microsoft Graph (app-only). The
// app client secret comes from an environment variable, never the command line,
// so it can't leak into shell history or the process list.
func runGraph(args []string) (err error) {
	fs := flag.NewFlagSet("mailarchive graph", flag.ContinueOnError)
	fs.Usage = graphUsage(fs)
	o := graphFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	mailboxes := append(o.mailboxes, fs.Args()...)

	// Fill unset values from the saved setup config (`mailarchive setup`); an
	// explicit flag always wins (coexistence, MA-269). A missing config is fine.
	if cfgPath, perr := graphconfig.DefaultConfigPath(); perr == nil {
		if cfg, cerr := graphconfig.Load(cfgPath); cerr == nil {
			if !flagWasSet(fs, "tenant") && *o.tenant == "" {
				*o.tenant = cfg.Tenant
			}
			if !flagWasSet(fs, "client-id") && *o.clientID == "" {
				*o.clientID = cfg.ClientID
			}
			if !flagWasSet(fs, "auth") && cfg.Auth != "" {
				*o.auth = cfg.Auth
			}
		}
	}

	authMode := strings.ToLower(strings.TrimSpace(*o.auth))
	if authMode != "app" && authMode != "device" {
		return fmt.Errorf("-auth must be app or device (got %q)", *o.auth)
	}
	if *o.out == "" {
		return errors.New("-out is required (the output directory)")
	}
	if *o.tenant == "" {
		return errors.New("-tenant is required (the Microsoft 365 tenant id or domain)")
	}
	if *o.clientID == "" {
		return errors.New("-client-id is required (the Entra app id)")
	}
	if *o.unattended {
		if err := requireExistingOut(*o.out); err != nil {
			return err
		}
	}
	// Before any sign-in, log or write: an encrypted archive is captured into only
	// with its key (phase2a §5); a scheduled capture's refusal is recorded (A14).
	if kerr := app.RefuseKeyless("capture into it", *o.out); kerr != nil {
		if *o.unattended {
			app.RecordRefusedRun(app.Options{Out: *o.out, Unattended: true}, kerr)
		}
		return kerr
	}

	gopts := app.GraphOptions{
		Tenant: *o.tenant, ClientID: *o.clientID, Mailboxes: mailboxes,
		IncludeDeleted: *o.includeDeleted, IncludeJunk: *o.includeJunk, Unattended: *o.unattended,
		BaseURL: graphFake.base, TokenURL: graphFake.token, DeviceAuthURL: graphFake.deviceAuth,
	}
	// A credential file inside -out travels with every copy and sync of the
	// archive (design-local-confidentiality P10). Run by hand, graph refuses it
	// before anything is created or fetched. Unattended — every scheduled job
	// carries -unattended — it warns, records the warning for `status`, and
	// captures: refusing at run time would silently stop an installed backup.
	var setupWarnings []state.RunWarning
	refuseOrWarn := func(problem error) error {
		if !*o.unattended {
			return problem
		}
		setupWarnings = append(setupWarnings, state.RunWarning{Code: "credential_in_archive", Message: problem.Error()})
		return nil
	}
	switch authMode {
	case "app":
		if len(mailboxes) == 0 {
			return errors.New("-mailbox is required (at least one mailbox UPN to archive)")
		}
		var secret string
		switch {
		case *o.secretFile != "":
			if credentialInArchive(*o.secretFile, *o.out) {
				if err := refuseOrWarn(secretFileInArchive(*o.secretFile, *o.out)); err != nil {
					return err
				}
			}
			if secret, err = readSecret(*o.secretFile); err != nil {
				return err
			}
		case os.Getenv(*o.secretEnv) != "":
			secret = os.Getenv(*o.secretEnv)
		default:
			// Fall back to the secret the setup wizard stored (OS credential
			// manager / 0600 file) for this tenant+client (MA-269).
			if store, serr := graphconfig.DefaultSecretStore(); serr == nil {
				if s, gerr := store.Get(graphconfig.AccountKey(*o.tenant, *o.clientID)); gerr == nil {
					secret = s
				}
			}
			if secret == "" {
				return fmt.Errorf("app client secret is empty: run `mailarchive setup` to store it, set the $%s environment variable, or pass -client-secret-file (or use -auth device to sign in as yourself with no secret)", *o.secretEnv)
			}
		}
		gopts.ClientSecret = secret
	case "device":
		// A public client takes no secret; refuse one so it is never fed to a flow
		// that ignores it and the operator is not misled (P6).
		if *o.secretFile != "" || flagWasSet(fs, "client-secret-env") {
			return errors.New("device auth is a public client and takes no secret — remove -client-secret-file/-client-secret-env (device mode signs you in interactively)")
		}
		if len(mailboxes) > 1 {
			return errors.New("device auth signs in as ONE user and archives that mailbox — pass at most one -mailbox (your own address), or none")
		}
		cache := strings.TrimSpace(*o.tokenCache)
		isDefault := cache == ""
		if isDefault {
			if cache, err = defaultTokenCachePath(*o.tenant, *o.clientID, mailboxes); err != nil {
				return err
			}
		}
		if credentialInArchive(cache, *o.out) {
			def, _ := defaultTokenCachePath(*o.tenant, *o.clientID, mailboxes)
			if err := refuseOrWarn(tokenCacheInArchive(cache, *o.out, def, isDefault, *o.unattended)); err != nil {
				return err
			}
		}
		gopts.Auth = "device"
		gopts.TokenCachePath = cache
		// On Windows the sign-in token lives in Credential Manager (vault); nil
		// elsewhere → the 0600 file cache (design-mailarchive-desktop P4). A fake
		// Microsoft 365 never gets the vault.
		if graphFake.base == "" {
			gopts.TokenStore = deviceTokenStore(*o.tenant, *o.clientID)
		}
	}

	mode, err := parseMode(*o.mode)
	if err != nil {
		return err
	}
	var since time.Time
	if *o.since != "" {
		since, err = util.ParseSince(*o.since, time.Now())
		if err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger, closeLog, err := newRunLogger(*o.log)
	if err != nil {
		return err
	}
	defer closeLog()
	defer func() { logFailure(logger, err) }()

	for _, w := range setupWarnings {
		logger.Printf("warning: %s", w.Message)
	}
	opts := app.Options{Out: *o.out, Mode: mode, Since: since, Index: *o.index, Pages: *o.pages, KeepRaw: *o.keepRaw, Warnings: setupWarnings}
	if authMode == "device" {
		logger.Printf("Signing in to tenant %s via Microsoft Graph device flow (mode=%s)", *o.tenant, *o.mode)
	} else {
		logger.Printf("Archiving %d mailbox(es) from tenant %s via Microsoft Graph (mode=%s)", len(mailboxes), *o.tenant, *o.mode)
	}
	if *o.includeDeleted || *o.includeJunk {
		logger.Printf("including %s in the walk (excluded by default)", includedFoldersPhrase(*o.includeDeleted, *o.includeJunk))
	}

	result, runErr := app.RunGraph(ctx, gopts, opts, logger)
	printSummary(logger, result, *o.index, *o.keepRaw, *o.out)
	err = graphOutcome(logger, runErr)
	return err
}

// printsSummary says whether a run that returned runErr reached completion, so
// the export verb prints its "Done. …" summary: a clean finish, a handled
// interrupt, and a partial run (its walk finished and the summary's "Not
// captured" block says what it could not capture). A failed run prints only its
// "FAILED:" line.
func printsSummary(runErr error) bool {
	return runErr == nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, app.ErrPartial)
}

// logFailure writes the run log's "FAILED:" line for a run that returned err. A
// partial run is not a failure of the run: its summary, "Not captured" block and
// report already say what happened, so it logs no "FAILED:" line (S1a still maps
// it to exit 1, like a failure, §4.9).
func logFailure(logger *log.Logger, err error) {
	if err != nil && !errors.Is(err, app.ErrPartial) {
		logger.Printf("FAILED: %v", err)
	}
}

// graphOutcome is the graph verb's result for RunGraph's error: a handled
// interrupt (RunGraph returned context.Canceled, X1) is a success, logged as
// one, and any other error is the verb's failure; main turns the result into
// the exit code (exitCode). It is its own function so a test can pass
// RunGraph's error through the verb's mapping (fault-isolation MA-540), which
// the verb cannot reach a fake server to show.
func graphOutcome(logger *log.Logger, runErr error) error {
	if errors.Is(runErr, context.Canceled) {
		logger.Printf("interrupted; progress saved to the manifest")
		return nil
	}
	return runErr
}

// flagWasSet reports whether a flag was explicitly passed (as opposed to left at
// its default) — used to refuse a secret-carrying flag under device auth even
// when it has a default value.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// verifyRemoveHint is printed when `-remove -- verify -out X` finds no entry
// under the archive's derived "-verify" name: the verify may still be running
// under the -name it was installed with (the README once passed a fixed one),
// which only that name removes — the same command with -name added. The hint
// names that entry when something records it: the archive's verify descriptor
// (FB-4), or else a backup descriptor that records a verify itself (a release
// from before a verify got its own entry name wrote it so). It is a hint, not
// an automatic removal: on launchd and Task Scheduler a shared name may by now
// hold another archive's verify.
func verifyRemoveHint(out string) string {
	// Only a recorded name that binds to this archive is offered (a descriptor
	// handed over with the folder can record any entry's name).
	if name, ok := schedule.RecordedVerifyName(out); ok {
		if _, err := schedule.ReadVerifyDescriptor(out); err == nil {
			return fmt.Sprintf("The archive's verify descriptor records a verify schedule named %q; if it is still this archive's verify, run this command again with -name %s before -- to remove it.", name, name)
		}
		return fmt.Sprintf("The archive's descriptor records a verify schedule named %q (written before a verify got its own entry name); if it is this archive's verify, run this command again with -name %s before -- to remove it.", name, name)
	}
	return "If this archive's verify was installed with -name NAME, run this command again with -name NAME before -- to remove it."
}

// deviceTokenStore is where a device sign-in lives besides the -token-cache file:
// the OS vault on Windows (Credential Manager, keyed by tenant + client), nil
// elsewhere (the file is the backend). A device run and `schedule`'s check of a
// device job both resolve it here, so they read the same backend; it is a
// variable only so a test can stand a real store in for the Windows vault.
var deviceTokenStore = graphconfig.DefaultTokenStore

// defaultTokenCachePath is where a device sign-in is cached when -token-cache is
// not given: under the caller's OS config dir (%APPDATA% on Windows, ~/.config on
// Unix — never inside -out, which may be a synced folder), named by the tenant +
// client, plus the mailbox when one is given so two mailboxes on one host do not
// collide (design D-C1/D-C3).
func defaultTokenCachePath(tenant, clientID string, mailboxes []string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine your OS config directory for the sign-in cache — pass -token-cache PATH: %w", err)
	}
	name := "graph-token-" + util.ShortHash(strings.ToLower(strings.TrimSpace(tenant))) + "-" + util.ShortHash(strings.TrimSpace(clientID))
	if len(mailboxes) == 1 {
		name += "-" + util.ShortHash(strings.ToLower(strings.TrimSpace(mailboxes[0])))
	}
	return filepath.Join(dir, "mailarchive", name+".json"), nil
}

func graphUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive graph - archive Microsoft 365 mailboxes server-side via Graph (app-only)

Usage:
  MAILARCHIVE_GRAPH_SECRET=... mailarchive graph -out DIR -tenant TENANT \
    -client-id APPID -mailbox user@domain [-mailbox ...] [-mode incremental|full]
  mailarchive graph ... -client-secret-file ~/.config/mailarchive/graph.secret   (scheduled jobs)

Requires an Entra app with the Mail.Read (application) permission, admin-consented
and RBAC-scoped to the mailboxes. See docs/graph-app-setup.md. Client secrets
expire (Entra caps them at 24 months): note the date and rotate before it.

Flags:
`)
		fs.PrintDefaults()
	}
}

func exportUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive - archive mail (Outlook .pst/.ost, Thunderbird, Evolution, Microsoft 365)
to self-contained HTML + attachment archives, with full-text search.

Usage:
  mailarchive -out DIR [-input FILE|DIR ...] [-auto] [-mode incremental|full] [-since 30d]
  mailarchive serve    -out DIR [-addr 127.0.0.1:8099]
  mailarchive search   -out DIR [-folder F] [-sender S] [-after D] QUERY...
  mailarchive reindex  -out DIR                          reconcile the archive with disk
  mailarchive schedule -out DIR [-auto] [-interval ...] [-install|-remove]
  mailarchive graph    -out DIR -tenant T -client-id ID -mailbox user@dom ...
  mailarchive status   -out DIR                          completeness, last run, schedule posture
  mailarchive verify   -out DIR [-json] [-record]        check archived files against recorded fixity
  mailarchive extract  -out DIR -format mbox|eml -dest OUTDIR   migrate the archive out as standard mail
  mailarchive desktop  [-capture]                        the dashboard & launcher (also opens on double-click)
  mailarchive wizard                                     step-by-step setup with native dialogs
  mailarchive run-job  FILE                              run a saved job headlessly (what a schedule runs)

Examples:
  mailarchive -auto -out ./export
  mailarchive search -out ./export from:bob invoice
  mailarchive serve  -out ./export
  mailarchive graph  -out ./export -tenant contoso.com -client-id APPID -mailbox a@contoso.com
  mailarchive schedule -out ./export -auto -install       keep it current, nightly
  mailarchive status -out ./export

Run 'mailarchive <subcommand> -h' for a subcommand's flags.

Export flags:
`)
		fs.PrintDefaults()
	}
}

func parseMode(s string) (export.Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "incremental", "":
		return export.Incremental, nil
	case "full":
		return export.Full, nil
	default:
		return 0, fmt.Errorf("invalid -mode %q (want incremental or full)", s)
	}
}

// prepareThunderbird enables offline download and/or waits for a sync on each
// Thunderbird IMAP store among the inputs, so on-demand mail is present locally.
func prepareThunderbird(ctx context.Context, inputs []string, auto, enableOffline, syncWait bool, logger *log.Logger) error {
	files, err := app.DiscoverInputs(inputs, auto)
	if err != nil {
		return err
	}
	var stores, osts []string
	for _, f := range files {
		fi, statErr := os.Stat(f)
		if statErr != nil {
			continue
		}
		switch {
		case fi.IsDir() && thunderbird.IsImapStore(f):
			stores = append(stores, f)
		case !fi.IsDir() && strings.EqualFold(filepath.Ext(f), ".ost"):
			osts = append(osts, f)
		}
	}

	// Outlook equivalent: we can't flip the offline setting, so guide instead.
	if len(osts) > 0 {
		logger.Printf("Outlook .ost detected (%d) — offline settings can't be changed from here.", len(osts))
		logger.Printf("  In Outlook: Account Settings -> Change -> \"Mail to keep offline\" -> All,")
		logger.Printf("  then Send/Receive -> Update Folder, and run again — an incremental run")
		logger.Printf("  re-examines and fills mail that had no content yet (no -mode full needed).")
	}

	if len(stores) == 0 {
		if len(osts) == 0 {
			logger.Printf("No Thunderbird IMAP or Outlook .ost stores among the inputs — nothing to prepare.")
		}
		return nil
	}
	for _, store := range stores {
		if err := prepareStore(ctx, store, enableOffline, syncWait, logger); err != nil {
			return err
		}
	}
	return nil
}

func prepareStore(ctx context.Context, store string, enableOffline, syncWait bool, logger *log.Logger) error {
	profile, ok := thunderbird.FindProfileDir(store)
	if !ok {
		logger.Printf("Could not find the Thunderbird profile for %s; skipping prep.", store)
		return nil
	}
	acct, _ := thunderbird.FindAccountForStore(profile, store)

	if enableOffline {
		if thunderbird.Running(profile) {
			return fmt.Errorf("Thunderbird looks like it is running — close it, then re-run with -enable-offline (prefs.js must not be edited while it is open)")
		}
		switch {
		case acct == nil:
			logger.Printf("Skipping -enable-offline: could not match an account to %s.", filepath.Base(store))
		case acct.OfflineDownload:
			logger.Printf("Offline download already enabled for %s.", acct.Hostname)
		default:
			changed, backup, err := thunderbird.EnableOffline(profile, acct.ServerKey)
			if err != nil {
				return err
			}
			if changed {
				logger.Printf("Enabled offline download for %s (prefs backed up to %s).", acct.Hostname, filepath.Base(backup))
			}
		}
		logger.Printf("Next: START Thunderbird, then right-click the account -> Download/Sync Now.")
		logger.Printf("Then run again — an incremental run re-examines and fills mail that had no content yet (no -mode full needed).")
	}

	if syncWait {
		return waitForSync(ctx, store, logger)
	}
	return nil
}

// waitForSync watches a store's size and returns once it stops growing (sync
// finished) or the user presses Enter.
func waitForSync(ctx context.Context, store string, logger *log.Logger) error {
	name := filepath.Base(store)
	logger.Printf("Waiting for %q to finish downloading.", name)
	logger.Printf("In Thunderbird: right-click the account -> Download/Sync Now (or File -> Offline -> Download/Sync Now).")
	logger.Printf("I will continue when the store stops growing; press Enter to continue now.")

	const stableFor = 30 * time.Second
	const poll = 3 * time.Second
	w := thunderbird.NewStableWaiter(store, stableFor, time.Now())
	start := w.Size()

	enter := make(chan struct{}, 1)
	go func() {
		bufio.NewReader(os.Stdin).ReadString('\n')
		enter <- struct{}{}
	}()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-enter:
			logger.Printf("Continuing.")
			return nil
		case now := <-ticker.C:
			size, stable := w.Poll(now)
			logger.Printf("  store size: %s (+%s since start)", thunderbird.HumanBytes(size), thunderbird.HumanBytes(size-start))
			if stable {
				logger.Printf("Store size stable for %s — assuming sync is complete.", stableFor)
				logger.Printf("Tip: you can close Thunderbird now for a clean read.")
				return nil
			}
		}
	}
}

// printNotCaptured prints the summary's account of what the run could not
// capture (fault-isolation §4.10): when it observed any fillable or terminal gap,
// how many items (and how many are new this run) and folders, how many will be
// retried when the archive runs again and how many need action, the report that
// lists them, and one line per gap (at most 20, then "+N more"), each led by the
// store's source id. A partly-read record is its own line. The "Recovered" line
// says how many earlier gaps this run closed by capturing the item.
func printNotCaptured(logger *log.Logger, r app.Result) {
	g := r.Gaps
	if g.Observed > 0 {
		head := fmt.Sprintf("Not captured: %d item(s) (%d new this run)", g.ItemGaps, g.New)
		if g.FolderGaps > 0 {
			head += fmt.Sprintf(", %d folder%s", g.FolderGaps, plural(g.FolderGaps, "", "s"))
		}
		head += fmt.Sprintf(" — %d will be retried when you run again, %d need action", g.Fillable, g.Terminal)
		if r.ReportPath != "" {
			head += fmt.Sprintf(" — details in %s", r.ReportPath)
		}
		logger.Printf("%s", head)
		for _, l := range g.Lines {
			logger.Printf("%s", l)
		}
	}
	if r.Stats.Unreadable > 0 {
		logger.Printf("Partly read: %d message(s) were saved with what could be read from them (their pages say what is missing)", r.Stats.Unreadable)
	}
	if g.Recovered > 0 {
		logger.Printf("Recovered: %d item(s) that failed on an earlier run were captured this run.", g.Recovered)
	}
}

func printSummary(logger *log.Logger, r app.Result, indexed, keepRaw bool, out string) {
	s := r.Stats
	msg := fmt.Sprintf("Done. exported=%d filled=%d skipped(seen)=%d skipped(date)=%d attachments=%d inline=%d non-html=%d no-body=%d manifest=%d",
		s.Exported, s.Filled, s.SkippedManifest, s.SkippedDate, s.Attachments, s.AttachmentsInline, s.NonHTMLBodies, s.NoBody, r.ManifestSize)
	if indexed {
		msg += fmt.Sprintf(" indexed=%d", r.Indexed)
	}
	if r.IndexErrors > 0 {
		msg += fmt.Sprintf(" INDEX-ERRORS=%d", r.IndexErrors)
	}
	logger.Printf("%s", msg)
	// Exports that were not new messages: a key exported again replaces its
	// earlier copy, so exported exceeds the growth in manifest and indexed.
	// A fill is its own explanation ("filled="); anything beyond it gets one
	// line saying so (F13).
	newMsgs := r.ManifestSize - r.ManifestBefore
	if replaced := s.Exported - newMsgs; newMsgs >= 0 && replaced > s.Filled {
		logger.Printf("Note:   %d exports made %d new message(s); %d replaced an earlier copy of the same message, so exported is above the growth in manifest and indexed.", s.Exported, newMsgs, replaced-s.Filled)
	}
	printNotCaptured(logger, r)
	// An index that holds fewer messages than the manifest (search.db lost or
	// emptied and re-created by this run, or earlier runs skipped indexing)
	// would otherwise end in a quiet exit 0 (F4). Rows the run itself could not
	// index have their own warning below.
	if indexed && r.IndexErrors == 0 && r.Indexed < r.ManifestSize {
		logger.Printf("WARNING: the search index holds %d of the archive's %d messages (search.db was missing or emptied, or earlier runs skipped indexing); search will not find the rest. Rebuild it from the archive with `mailarchive reindex -rebuild -out %q`.", r.Indexed, r.ManifestSize, out)
	}

	// One coherent Verification line: what is still missing, what the source can
	// never deliver, what has not been re-examined yet, how many fillable gaps
	// this run re-examined (invisible otherwise), and the report path only when a
	// report file exists. Printed whenever any of those is non-zero.
	if r.Fillable > 0 || r.Terminal > 0 || r.Unknown > 0 || r.Undecodable > 0 || s.UnresolvedInlineRef > 0 || s.HTMLFallback > 0 || s.Retried > 0 {
		line := fmt.Sprintf("Verification: %d message(s) still missing content, %d source-empty (never fillable), %d not yet re-examined",
			r.Fillable, r.Terminal, r.Unknown)
		if s.Retried > 0 {
			line += fmt.Sprintf(", re-examined=%d", s.Retried)
		}
		// A part this version cannot decode is kept as received, and is neither missing
		// nor empty at the source: its own count, what it means and where to read more
		// (design-mime-fidelity §4.9).
		if r.Undecodable > 0 {
			line += fmt.Sprintf(", %d message(s) with parts this version cannot decode (kept as received; see the report)", r.Undecodable)
		}
		if s.UnresolvedInlineRef > 0 {
			line += fmt.Sprintf(", %d unresolved inline image(s) this run", s.UnresolvedInlineRef)
		}
		if s.HTMLFallback > 0 {
			line += fmt.Sprintf(", %d HTML message(s) shown as source this run (could not be rendered safely: too deep, unparseable, unsafe after re-parse or too large; content kept)", s.HTMLFallback)
		}
		if r.ReportPath != "" {
			line += fmt.Sprintf(" — details in %s", r.ReportPath)
		}
		logger.Printf("%s", line)
		if r.Fillable > 0 {
			// A message that is fillable only because a part of it is cut short, or its parts could
			// not be told apart, in the file it was read from is not one the source has not delivered:
			// a download returns the same bytes from a mailbox file. Say what cures it, and say once
			// that these are also in the cannot-decode count (friction F3).
			if r.FillableParseGap > 0 {
				logger.Printf("  %d of them %s also counted as having parts this version cannot decode: the report says which.", r.FillableParseGap, plural(r.FillableParseGap, "is", "are"))
			}
			if r.FillableOnlyParseGap >= r.Fillable {
				logger.Printf("  These messages are cut short, or have parts that could not be told apart, in the file they were read from; what could be read is kept (see the report).")
				logger.Printf("  A download does not change a mailbox file: replace it with a complete export from your mail app, then simply re-run: incremental fills them. (For an IMAP cache, download the messages for offline use instead.) A message that was malformed when it was sent cannot be completed by either, and stays listed.")
			} else {
				logger.Printf("  Missing content usually means it isn't cached locally (IMAP / a limited Outlook offline window).")
				logger.Printf("  In your mail app, download for offline use, then simply re-run: incremental fills the gaps.")
				if r.FillableParseGap > 0 {
					// A mix: the download is for the messages a source has not delivered; a cut one
					// among them is not changed by it (friction F3, re-verification).
					logger.Printf("  %d of them %s cut short, or %s parts that could not be told apart, in the mailbox file: a download does not change it; replace the file with a complete export from your mail app (the report says which).", r.FillableParseGap, plural(r.FillableParseGap, "is", "are"), plural(r.FillableParseGap, "has", "have"))
				}
			}
		}
		if r.Unknown > 0 {
			logger.Printf("  %d entr%s predate completeness tracking and are re-examined by incremental runs until none remain.", r.Unknown, plural(r.Unknown, "y", "ies"))
		}
	}
	// A message this version could not decode in full is kept as the earlier version
	// read it (MC19): nothing is lost, and the report lists it.
	if r.FidelityFallback > 0 {
		line := fmt.Sprintf("Verification: %d message(s) kept as an earlier version read them, because this version could not decode them in full (nothing is lost)", r.FidelityFallback)
		if r.ReportPath != "" {
			line += fmt.Sprintf(" — details in %s", r.ReportPath)
		}
		logger.Printf("%s", line)
	}
	// Partly-read and unparsed records are complete as far as the source went, so
	// they are not "missing content to download": their own line, their own remedy.
	if r.Unreadable > 0 || r.Unparsed > 0 {
		line := fmt.Sprintf("Verification: %d message(s) read only in part, %d unparsed (kept as text)", r.Unreadable, r.Unparsed)
		if r.ReportPath != "" {
			line += fmt.Sprintf(" — details in %s", r.ReportPath)
		}
		logger.Printf("%s", line)
		if r.Unreadable > 0 {
			logger.Printf("  A damaged data file can usually be repaired in place (Outlook's scanpst.exe for a .pst, Windows), then run again.")
		}
		if r.Unparsed > 0 {
			logger.Printf("  An unparsed message is kept as text, and its original bytes as .eml when the archive is made with -raw; after upgrading, the next run re-examines it once.")
		}
	}
	// -raw is silently a no-op on sources that carry no original message bytes
	// (Outlook .pst/.ost items). Say so once, rather than let the operator assume
	// .eml files were written (P7).
	if keepRaw && s.Exported > 0 && s.RawWritten == 0 {
		logger.Printf("WARNING: -raw had no effect: these sources carry no original message bytes (typically Outlook .pst/.ost items); 0 .eml written")
	}
	// The mirror case (PC15): a raw-capable source (mbox/maildir/Graph) was
	// archived WITHOUT -raw, so no .eml was preserved and `mailarchive extract`
	// will produce nothing. Say so at capture, while the source still exists to
	// re-archive, rather than let the operator discover it after deleting it.
	if !keepRaw && s.RawAvailable > 0 {
		logger.Printf("WARNING: these sources carry original message bytes, but -raw was not set — no .eml was preserved, so `mailarchive extract` will produce nothing from this archive. Re-run with -raw to keep the originals BEFORE deleting the source.")
	}
	// Plain `reindex` only prunes; the rebuild re-adds the missing rows from the
	// archived files (F50).
	if r.IndexErrors > 0 {
		logger.Printf("WARNING: %d message(s) were exported but could not be indexed; rebuild the index from the archive with `mailarchive reindex -rebuild -out %q` and re-check.", r.IndexErrors, out)
	}

	if indexed && r.Indexed > 0 {
		logger.Printf("Search:  mailarchive serve -out %q   (then open the printed URL)", out)
	}
}
