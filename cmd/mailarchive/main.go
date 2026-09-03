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
	"mail-archive-tool/internal/runlog"
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
	case len(args) > 0 && args[0] == "graph":
		err = runGraph(args[1:])
	case len(args) > 0 && args[0] == "status":
		err = runStatus(args[1:])
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
	fs := flag.NewFlagSet("mailarchive", flag.ContinueOnError)
	fs.Usage = exportUsage(fs)
	o := exportFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	inputs := append(o.inputs, fs.Args()...)

	if *o.out == "" {
		return errors.New("-out is required (or use a subcommand: serve, search, reindex, schedule, graph, status)")
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

	logger, closeLog, err := newRunLogger(*o.log)
	if err != nil {
		return err
	}
	defer closeLog()
	defer func() { // a failed unattended run leaves its reason in its own log too
		if err != nil {
			logger.Printf("FAILED: %v", err)
		}
	}()

	outlook, outlookSyncWait, auto, copyFirst := o.outlook, o.outlookSyncWait, o.auto, o.copyFirst
	manifestPath, doIndex, doPages, keepRaw := o.manifest, o.index, o.pages, o.keepRaw
	enableOffline, syncWait, modeStr := o.enableOffline, o.syncWait, o.mode
	out := o.out

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
		KeepRaw:   *keepRaw,
	}
	_ = modeStr

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
	printSummary(logger, result, *doIndex, *out)

	if errors.Is(runErr, context.Canceled) {
		logger.Printf("interrupted; progress saved to the manifest")
		return nil
	}
	err = runErr
	return err
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

	if !server.IsLoopback(*addr) {
		fmt.Fprintf(os.Stderr, "WARNING: serve has no authentication. Binding %s exposes every archived message to anyone who can reach this machine on that port; use 127.0.0.1 (the default) unless you mean it.\n", *addr)
	}
	fmt.Printf("Serving %d indexed messages from %s\n", count, *out)
	fmt.Printf("Search UI:  http://%s/\n", *addr)
	fmt.Println("Press Ctrl-C to stop.")
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second, // a stalled client cannot hold a connection open forever
		IdleTimeout:       2 * time.Minute,
	}
	return srv.ListenAndServe()
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

	logger, closeLog, err := newRunLogger(*o.log)
	if err != nil {
		return err
	}
	defer closeLog()
	kept, pruned, err := app.Reindex(*o.out, logger)
	if err != nil {
		logger.Printf("FAILED: %v", err)
		return err
	}
	logger.Printf("reindexed: kept=%d pruned=%d", kept, pruned)
	return nil
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
			return fmt.Errorf("with -- the job carries its own flags: move %s after -- (or drop the --)", strings.Join(stray, ", "))
		}
	} else {
		jobArgs = flatJobArgs(eo, fs.Args())
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine my own executable path: %w", err)
	}

	// Removal keys off the name, found via -name or the archive's descriptor.
	if *remove {
		out := abspath(*eo.out)
		if out == "" && len(jobArgs) > 0 {
			if j, jerr := parseJob(jobArgs); jerr == nil {
				out = j.out
			}
		}
		n := *name
		if n == "" && out != "" {
			if d, derr := schedule.ReadDescriptor(out); derr == nil {
				n = d.Name
			} else {
				n = schedule.DefaultNameFor(out)
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
		if err := schedule.Remove(spec); err != nil {
			return err
		}
		fmt.Printf("Removed scheduled backup %q.\n", n)
		return nil
	}

	j, err := parseJob(jobArgs)
	if err != nil {
		return err
	}
	n := *name
	if n == "" {
		n = schedule.DefaultNameFor(j.out)
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
		Wrapper:     runtime.GOOS == "windows",
		WrapperPath: schedule.DefaultWrapperPath(n),
		Out:         j.out,
	}
	if err := spec.Validate(); err != nil {
		return err
	}

	if *install {
		if err := schedule.Install(spec); err != nil {
			return err
		}
		fmt.Printf("Installed scheduled backup %q (%s at %s).\n", n, iv, *at)
		fmt.Printf("It runs: %s %s\n", exe, strings.Join(spec.Args, " "))
		fmt.Printf("Log: %s · descriptor: %s · check with: mailarchive status -out %q\n", logPath, filepath.Join(j.out, schedule.DescriptorName), j.out)
		return nil
	}
	text, err := schedule.Preview(spec)
	if err != nil {
		return err
	}
	fmt.Print(text)
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
  mailarchive schedule -name NAME -remove                          uninstall by name

The job after -- may be an export (no verb), graph, or reindex job; it is
validated now, and what cannot run unattended is refused now. By default the
exact scheduler entry is printed and NOT applied. The job writes its log to
<out>/<name>.log; one schedule per archive by default.

Flags:
`)
		fs.PrintDefaults()
	}
}

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

	if *o.out == "" {
		return errors.New("-out is required (the output directory)")
	}
	if *o.tenant == "" {
		return errors.New("-tenant is required (the Microsoft 365 tenant id or domain)")
	}
	if *o.clientID == "" {
		return errors.New("-client-id is required (the Entra app id)")
	}
	if len(mailboxes) == 0 {
		return errors.New("-mailbox is required (at least one mailbox UPN to archive)")
	}
	if *o.unattended {
		if err := requireExistingOut(*o.out); err != nil {
			return err
		}
	}
	var secret string
	if *o.secretFile != "" {
		if secret, err = readSecret(*o.secretFile); err != nil {
			return err
		}
	} else {
		secret = os.Getenv(*o.secretEnv)
		if secret == "" {
			return fmt.Errorf("app client secret is empty: set it in the $%s environment variable, or pass -client-secret-file", *o.secretEnv)
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
	defer func() {
		if err != nil {
			logger.Printf("FAILED: %v", err)
		}
	}()

	gopts := app.GraphOptions{Tenant: *o.tenant, ClientID: *o.clientID, ClientSecret: secret, Mailboxes: mailboxes}
	opts := app.Options{Out: *o.out, Mode: mode, Since: since, Index: *o.index, Pages: *o.pages, KeepRaw: *o.keepRaw}
	logger.Printf("Archiving %d mailbox(es) from tenant %s via Microsoft Graph (mode=%s)", len(mailboxes), *o.tenant, *o.mode)

	result, runErr := app.RunGraph(ctx, gopts, opts, logger)
	printSummary(logger, result, *o.index, *o.out)
	if errors.Is(runErr, context.Canceled) {
		logger.Printf("interrupted; progress saved to the manifest")
		return nil
	}
	err = runErr
	return err
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
	msg := fmt.Sprintf("Done. exported=%d filled=%d skipped(seen)=%d skipped(date)=%d attachments=%d inline=%d non-html=%d no-body=%d manifest=%d",
		s.Exported, s.Filled, s.SkippedManifest, s.SkippedDate, s.Attachments, s.AttachmentsInline, s.NonHTMLBodies, s.NoBody, r.ManifestSize)
	if indexed {
		msg += fmt.Sprintf(" indexed=%d", r.Indexed)
	}
	if r.IndexErrors > 0 {
		msg += fmt.Sprintf(" INDEX-ERRORS=%d", r.IndexErrors)
	}
	logger.Printf("%s", msg)

	if r.Fillable > 0 || r.Terminal > 0 || r.Unknown > 0 || s.UnresolvedInlineRef > 0 {
		suffix := ""
		if r.ReportPath != "" {
			suffix = fmt.Sprintf(" — details in %s", r.ReportPath)
		}
		logger.Printf("Verification: %d message(s) still missing content, %d source-empty (never fillable), %d not yet re-examined, %d unresolved inline image(s) this run%s",
			r.Fillable, r.Terminal, r.Unknown, s.UnresolvedInlineRef, suffix)
		if r.Fillable > 0 {
			logger.Printf("  Missing content usually means it isn't cached locally (IMAP / a limited Outlook offline window).")
			logger.Printf("  In your mail app, download for offline use, then simply re-run: incremental fills the gaps.")
		}
		if r.Unknown > 0 {
			logger.Printf("  %d entr%s predate completeness tracking and are re-examined by incremental runs until none remain.", r.Unknown, plural(r.Unknown, "y", "ies"))
		}
	}
	if r.IndexErrors > 0 {
		logger.Printf("WARNING: %d message(s) were exported but could not be indexed; run `mailarchive reindex -out %q` and re-check.", r.IndexErrors, out)
	}

	if indexed && r.Indexed > 0 {
		logger.Printf("Search:  mailarchive serve -out %q   (then open the printed URL)", out)
	}
}
