// Command mailarchive exports messages from local Outlook data files (.pst/.ost)
// into a directory of self-contained HTML files with per-email attachment
// archives, mirroring the Outlook folder tree, and builds a full-text search
// index for discovery.
//
// Subcommands:
//
//	mailarchive [flags]        export (default)
//	mailarchive serve  [flags] start the local search + reader web UI
//	mailarchive search [flags] QUERY...   full-text search from the terminal
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
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
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

func main() {
	args := os.Args[1:]
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
	default:
		err = runExport(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mailarchive: "+err.Error())
		os.Exit(1)
	}
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
	var inputs stringSlice
	fs := flag.NewFlagSet("mailarchive", flag.ContinueOnError)
	fs.Usage = exportUsage(fs)
	fs.Var(&inputs, "input", "PST/OST file or a directory to scan (repeatable, comma-separated)")
	out := fs.String("out", "", "output directory (required)")
	modeStr := fs.String("mode", "incremental", "export mode: incremental|full")
	sinceStr := fs.String("since", "", "only export items newer than this (e.g. 30d, 4w, 720h, 2026-07-01)")
	manifestPath := fs.String("manifest", "", "manifest path (default <out>/.mailarchive-manifest.json)")
	copyFirst := fs.Bool("copy-first", false, "copy each data file to a temp snapshot before reading (avoids locks when Outlook is open)")
	auto := fs.Bool("auto", false, "auto-discover mail stores (Outlook on Windows; Thunderbird on any OS)")
	outlook := fs.Bool("outlook", false, "Windows + classic Outlook: have Outlook export each account to a .pst first, then archive that (use when a .ost can't be read directly)")
	outlookSyncWait := fs.Duration("outlook-sync-wait", 5*time.Minute, "with -outlook: run Send/Receive and wait up to this long for downloads before creating the PST (0 to skip)")
	doIndex := fs.Bool("index", true, "build/update the full-text search index (search.db)")
	doPages := fs.Bool("pages", true, "generate browsable folder index.html pages")
	enableOffline := fs.Bool("enable-offline", false, "Thunderbird IMAP: enable offline download in prefs.js so all mail can be synced (Thunderbird must be closed)")
	syncWait := fs.Bool("sync-wait", false, "Thunderbird IMAP: pause and wait for Download/Sync to finish before exporting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs = append(inputs, fs.Args()...)

	if *out == "" {
		return errors.New("-out is required (or use a subcommand: serve, search)")
	}
	mode, err := parseMode(*modeStr)
	if err != nil {
		return err
	}
	var since time.Time
	if *sinceStr != "" {
		since, err = util.ParseSince(*sinceStr, time.Now())
		if err != nil {
			return err
		}
	}

	logger := log.New(os.Stderr, "", 0)

	// Optionally have Outlook itself export each account to a fresh .pst, then
	// archive those. This is the reliable path for a live Exchange/IMAP .ost cache
	// go-pst can't read directly. Windows + classic Outlook only; elsewhere it
	// refuses with a legible message.
	if *outlook {
		// Refuse up front on a platform that can't run Outlook automation, before
		// printing the completeness note or any "exporting…" progress — a guaranteed
		// refusal must not first look like it started work.
		if runtime.GOOS != "windows" {
			return fmt.Errorf("%w", outlookcom.ErrUnsupported)
		}
		logger.Printf("%s", outlookcom.CompletenessNote)
		pstDir := filepath.Join(*out, "_outlook-pst")
		logger.Printf("Outlook: exporting each account to a PST under %s ...", pstDir)
		stores, cerr := outlookcom.CreatePSTs(pstDir,
			outlookcom.Options{Sync: *outlookSyncWait > 0, SyncWait: *outlookSyncWait}, logger)
		if cerr != nil {
			return cerr
		}
		for _, s := range stores {
			inputs = append(inputs, s.Path)
		}
		logger.Printf("Outlook: %d PST(s) ready; archiving them now.", len(stores))
	}

	opts := app.Options{
		Inputs:    inputs,
		Auto:      *auto,
		Out:       *out,
		Mode:      mode,
		Since:     since,
		CopyFirst: *copyFirst,
		Manifest:  *manifestPath,
		Index:     *doIndex,
		Pages:     *doPages,
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

	result, err := app.Run(ctx, opts, logger, nil)
	printSummary(logger, result, *doIndex, *out)

	if errors.Is(err, context.Canceled) {
		logger.Printf("interrupted; progress saved to the manifest")
		return nil
	}
	return err
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("mailarchive serve", flag.ContinueOnError)
	out := fs.String("out", ".", "export directory to serve (contains search.db)")
	addr := fs.String("addr", "127.0.0.1:8099", "address to listen on")
	if err := fs.Parse(args); err != nil {
		return err
	}

	idxPath := filepath.Join(*out, "search.db")
	ix, err := index.OpenReadonly(idxPath)
	if err != nil {
		return err
	}
	defer ix.Close()

	count, _ := ix.Count()
	handler := server.New(*out, ix)

	fmt.Printf("Serving %d indexed messages from %s\n", count, *out)
	fmt.Printf("Search UI:  http://%s/\n", *addr)
	fmt.Println("Press Ctrl-C to stop.")
	return http.ListenAndServe(*addr, handler)
}

var markTags = regexp.MustCompile(`</?mark>`)

func runSearch(args []string) error {
	fs := flag.NewFlagSet("mailarchive search", flag.ContinueOnError)
	out := fs.String("out", ".", "export directory to search (contains search.db)")
	limit := fs.Int("limit", 20, "maximum results")
	folder := fs.String("folder", "", "restrict to a folder (and its subfolders)")
	sender := fs.String("sender", "", "restrict to a sender (substring)")
	afterStr := fs.String("after", "", "only items on/after this date (YYYY-MM-DD)")
	beforeStr := fs.String("before", "", "only items before this date (YYYY-MM-DD)")
	attach := fs.Bool("attach", false, "only items with attachments")
	if err := fs.Parse(args); err != nil {
		return err
	}

	q := index.Query{
		Text:      strings.Join(fs.Args(), " "),
		Folder:    *folder,
		Sender:    *sender,
		HasAttach: *attach,
		Limit:     *limit,
	}
	if t, err := time.Parse("2006-01-02", *afterStr); err == nil {
		q.After = t
	}
	if t, err := time.Parse("2006-01-02", *beforeStr); err == nil {
		q.Before = t
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

	fmt.Printf("%d match(es)%s:\n\n", total, moreNote(total, len(results)))
	for _, r := range results {
		date := "          "
		if !r.Date.IsZero() {
			date = r.Date.Format("2006-01-02")
		}
		from := r.SenderName
		if from == "" {
			from = r.SenderEmail
		}
		subject := r.Subject
		if subject == "" {
			subject = "(no subject)"
		}
		fmt.Printf("%s  %-28.28s  %s\n", date, from, r.Folder)
		fmt.Printf("    %s\n", subject)
		if r.Snippet != "" {
			fmt.Printf("    %s\n", markTags.ReplaceAllString(r.Snippet, ""))
		}
		fmt.Printf("    -> %s\n\n", r.Path)
	}
	return nil
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
func runReindex(args []string) error {
	fs := flag.NewFlagSet("mailarchive reindex", flag.ContinueOnError)
	out := fs.String("out", "", "export directory to reconcile (contains search.db) (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the export directory to reconcile)")
	}

	logger := log.New(os.Stderr, "", 0)
	kept, pruned, err := app.Reindex(*out, logger)
	if err != nil {
		return err
	}
	logger.Printf("reindexed: kept=%d pruned=%d", kept, pruned)
	return nil
}

// runSchedule prints (default) or installs/removes a recurring-backup entry for
// the host OS's scheduler. The scheduled command is this executable plus the
// export flags the operator passed, so it runs `mailarchive -out DIR <sources>`.
func runSchedule(args []string) error {
	var inputs stringSlice
	fs := flag.NewFlagSet("mailarchive schedule", flag.ContinueOnError)
	fs.Usage = scheduleUsage(fs)
	interval := fs.String("interval", "daily", "backup cadence: hourly|daily|weekly")
	at := fs.String("at", "02:00", "time of day HH:MM (hourly uses only the minute)")
	name := fs.String("name", schedule.DefaultName, "scheduler entry name")
	install := fs.Bool("install", false, "install the schedule (default: print it without applying)")
	remove := fs.Bool("remove", false, "remove a previously installed schedule by name")
	// Pass-through export flags: these become the scheduled command's arguments.
	fs.Var(&inputs, "input", "PST/OST file or a directory to back up (repeatable, comma-separated)")
	out := fs.String("out", "", "output directory the backup writes to (required)")
	auto := fs.Bool("auto", false, "auto-discover mail stores when the backup runs")
	modeStr := fs.String("mode", "incremental", "backup export mode: incremental|full")
	copyFirst := fs.Bool("copy-first", false, "snapshot each data file before reading (avoids locks)")
	sinceStr := fs.String("since", "", "only export items newer than this (e.g. 30d, 2026-07-01)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs = append(inputs, fs.Args()...)

	if *install && *remove {
		return errors.New("choose either -install or -remove, not both")
	}
	iv, err := schedule.ParseInterval(*interval)
	if err != nil {
		return err
	}
	mode, err := parseMode(*modeStr)
	if err != nil {
		return err
	}
	modeName := "incremental"
	if mode == export.Full {
		modeName = "full"
	}
	if *sinceStr != "" {
		if _, err := util.ParseSince(*sinceStr, time.Now()); err != nil {
			return err
		}
	}

	// -out is required except when removing (removal keys off the name alone).
	if !*remove && *out == "" {
		return errors.New("-out is required (the directory the scheduled backup writes to)")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine my own executable path: %w", err)
	}

	// A scheduled job runs from an unknown working directory, so make the paths
	// absolute before baking them into the entry.
	absOut := abspath(*out)
	var absInputs []string
	for _, in := range inputs {
		absInputs = append(absInputs, abspath(in))
	}

	spec := schedule.Spec{
		Name:     *name,
		Interval: iv,
		At:       *at,
		Exe:      exe,
		Args:     scheduleExportArgs(absOut, absInputs, *auto, modeName, *copyFirst, *sinceStr),
	}
	if absOut != "" {
		spec.Log = schedule.DefaultLogPath(absOut, *name)
	}
	if err := spec.Validate(); err != nil {
		return err
	}

	switch {
	case *remove:
		if err := schedule.Remove(spec); err != nil {
			return err
		}
		fmt.Printf("Removed scheduled backup %q.\n", *name)
	case *install:
		if err := schedule.Install(spec); err != nil {
			return err
		}
		fmt.Printf("Installed scheduled backup %q (%s at %s).\n", *name, iv, *at)
		fmt.Printf("It runs: %s %s\n", exe, strings.Join(spec.Args, " "))
	default:
		text, err := schedule.Preview(spec)
		if err != nil {
			return err
		}
		fmt.Print(text)
		fmt.Println("\nThis was NOT applied. Re-run with -install to schedule it, or -remove to uninstall.")
	}
	return nil
}

// scheduleExportArgs reconstructs the export flag list the scheduled run will
// receive: `mailarchive -out DIR -mode MODE [sources...]`.
func scheduleExportArgs(out string, inputs []string, auto bool, mode string, copyFirst bool, since string) []string {
	args := []string{"-out", out, "-mode", mode}
	if auto {
		args = append(args, "-auto")
	}
	for _, in := range inputs {
		args = append(args, "-input", in)
	}
	if copyFirst {
		args = append(args, "-copy-first")
	}
	if since != "" {
		args = append(args, "-since", since)
	}
	return args
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

func scheduleUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive schedule - schedule a recurring backup with the host OS scheduler

Usage:
  mailarchive schedule -out DIR [-input ...|-auto] [-interval daily|weekly|hourly] [-at HH:MM] [-name NAME]
  mailarchive schedule -out DIR -auto -install        apply the schedule
  mailarchive schedule -name NAME -remove             uninstall by name

By default the exact scheduler entry is printed and NOT applied.

Flags:
`)
		fs.PrintDefaults()
	}
}

func exportUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive - export Outlook .pst/.ost mail to HTML + attachment archives, with search

Usage:
  mailarchive -out DIR [-input FILE|DIR ...] [-auto] [-mode incremental|full] [-since 30d]
  mailarchive serve  -out DIR [-addr 127.0.0.1:8099]
  mailarchive search -out DIR [-folder F] [-sender S] [-after D] QUERY...

Examples:
  mailarchive -auto -out ./export
  mailarchive search -out ./export from:bob invoice
  mailarchive serve  -out ./export

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
		logger.Printf("  then Send/Receive -> Update Folder, and re-run with -mode full.")
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
		logger.Printf("To re-export mail previously exported without content, use -mode full.")
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

func printSummary(logger *log.Logger, r app.Result, indexed bool, out string) {
	s := r.Stats
	msg := fmt.Sprintf("Done. exported=%d skipped(seen)=%d skipped(date)=%d attachments=%d inline=%d non-html=%d no-body=%d manifest=%d",
		s.Exported, s.SkippedManifest, s.SkippedDate, s.Attachments, s.AttachmentsInline, s.NonHTMLBodies, s.NoBody, r.ManifestSize)
	if indexed {
		msg += fmt.Sprintf(" indexed=%d", r.Indexed)
	}
	logger.Printf("%s", msg)

	if s.AttachmentsEmpty > 0 || s.UnresolvedInlineRef > 0 {
		suffix := ""
		if r.ReportPath != "" {
			suffix = fmt.Sprintf(" — details in %s", r.ReportPath)
		}
		logger.Printf("Verification: %d empty/not-downloaded attachment(s), %d unresolved inline image(s)%s",
			s.AttachmentsEmpty, s.UnresolvedInlineRef, suffix)
		if s.AttachmentsEmpty > 0 {
			logger.Printf("  Empty attachments usually mean the content isn't cached locally (IMAP). In your mail")
			logger.Printf("  app, download for offline use, then re-run with -mode full to fill the gaps.")
		}
	}

	if indexed && r.Indexed > 0 {
		logger.Printf("Search:  mailarchive serve -out %q   (then open the printed URL)", out)
	}
}
