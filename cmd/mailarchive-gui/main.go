// Command mailarchive-gui is a small native-dialog wizard around the exporter.
// Double-clicking it walks the user through picking a source, an output
// folder, a mode and an optional date window, shows a progress dialog and a
// summary, and finally offers to keep the archive current on a schedule — all
// via native OS dialogs (pure-Go, no browser, no console).
//
// Started as `mailarchive-gui -job FILE` it runs that job headlessly (what the
// schedule it installed does): no dialog is ever shown, the run log goes to
// <out>/mailarchive.log, and a job that cannot even be read leaves its reason
// in <config>/mailarchive/<name>.jobfail.log.
//
// Build for Windows without a console window:
//
//	GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o mailarchive-gui.exe ./cmd/mailarchive-gui
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/runlog"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

const appTitle = "Mail Archive Export"

// Source-type labels the wizard's first list offers. Package-level so the pure
// decision functions (keepRawApplies, preferOutlookApp) and their tests can name
// them without reconstructing a dialog.
const (
	srcAuto        = "Auto-detect my mailboxes"
	srcOutlook     = "Outlook data file (.pst / .ost)"
	srcOutlookCOM  = "Outlook account (via Outlook app — for Exchange / .ost)"
	srcThunderbird = "Thunderbird / mbox mail folder"
	srcEvolution   = "Evolution mail store (folder)"
	srcMbox        = "Single mbox file"
)

// The Deleted-Items/Junk question's two answers. The dialog defaults to the
// exclude item so a click-through (accepting the default) excludes them (T7);
// only the explicit include item opts in.
const (
	deletedJunkExcludeItem = "No — skip Deleted Items and Junk Email (recommended)"
	deletedJunkIncludeItem = "Yes — also archive Deleted Items and Junk Email"
)

// notify is the desktop-notification sink, overridable in tests. A scheduled
// (headless) run has no window, so a failure would otherwise be invisible until
// the user next opens the GUI (P3).
var notify = zenity.Notify

// detectOutlook reports whether classic Outlook COM automation is available on
// this machine; overridable in tests (the real probe is Windows-only).
var detectOutlook = func() bool { _, ok := outlookcom.Detect(); return ok }

func main() {
	jobPath := flag.String("job", "", "run this saved job headlessly (no dialogs); what the installed schedule uses")
	flag.Parse()
	if *jobPath != "" {
		os.Exit(runHeadless(*jobPath))
	}

	// The Windows build has no console, so an unhandled panic would exit silently.
	// Surface it in a dialog instead — a visible error always beats a vanish.
	defer func() {
		if r := recover(); r != nil {
			_ = zenity.Error(fmt.Sprintf("Something went wrong:\n\n%v", r), zenity.Title(appTitle))
			os.Exit(1)
		}
	}()

	err := wizard()
	switch {
	case err == nil:
		return
	case errors.Is(err, zenity.ErrCanceled):
		// User dismissed a dialog: exit quietly.
		return
	default:
		_ = zenity.Error(err.Error(), zenity.Title(appTitle))
		os.Exit(1)
	}
}

// ---- headless (scheduled) run ---------------------------------------------

// runHeadless runs a saved job with no dialogs. Exit 1 on any failure, with
// the reason in the run log (or, when the job file itself is the problem, in
// the jobfail log beside it — a sink that does not depend on the job's out).
func runHeadless(path string) int {
	j, err := job.Read(path)
	if err != nil {
		jobFail(path, err)
		return 1
	}
	// Unattended guard: an absent -out (the backup drive is not mounted) must
	// not become a fresh archive on the local disk that then reports "ok".
	if fi, err := os.Stat(j.Out); err != nil || !fi.IsDir() {
		jobFail(path, fmt.Errorf("archive directory %s is not present — is the backup drive mounted? refusing to create a new archive elsewhere", j.Out))
		return 1
	}
	logf, err := runlog.Open(filepath.Join(j.Out, "mailarchive.log"))
	if err != nil {
		jobFail(path, err)
		return 1
	}
	defer logf.Close()
	logger := log.New(logf, "", log.LstdFlags)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	inputs := j.Inputs
	if j.Outlook {
		wait := 5 * time.Minute
		if d, err := time.ParseDuration(j.OutlookSyncWait); err == nil {
			wait = d
		}
		stores, cerr := outlookcom.CreatePSTs(filepath.Join(j.Out, "_outlook-pst"),
			outlookcom.Options{Sync: wait > 0, SyncWait: wait}, logger)
		if cerr != nil {
			logger.Printf("FAILED: %v", cerr)
			fmt.Fprintln(os.Stderr, "mailarchive-gui: "+cerr.Error())
			notifyFailure(j.Name, j.Out)
			return 1
		}
		inputs = nil
		for _, s := range stores {
			inputs = append(inputs, s.Path)
		}
	}
	mode := export.Incremental
	if strings.EqualFold(j.Mode, "full") {
		mode = export.Full
	}
	var since time.Time
	if j.Since != "" {
		if t, err := util.ParseSince(j.Since, time.Now()); err == nil {
			since = t
		} else {
			logger.Printf("warning: ignoring invalid since %q: %v", j.Since, err)
		}
	}
	opts := app.Options{Inputs: inputs, Auto: j.Auto, Out: j.Out, Mode: mode, Since: since,
		CopyFirst: j.CopyFirst, Index: true, Pages: true, KeepRaw: j.KeepRaw}
	logger.Printf("Scheduled job %q: exporting to %s", j.Name, j.Out)
	result, runErr := app.Run(ctx, opts, logger, nil)
	logSummary(logger, result)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		logger.Printf("FAILED: %v", runErr)
		fmt.Fprintln(os.Stderr, "mailarchive-gui: "+runErr.Error()) // cron mail / stderr.log
		notifyFailure(j.Name, j.Out)
		return 1
	}
	// The Outlook-app path wrote scratch PSTs under <out>/_outlook-pst; once the
	// export that read them succeeded, reclaim that space (kept on failure).
	if j.Outlook && runErr == nil {
		cleanupOutlookScratch(j.Out, true, logger)
	}
	return 0
}

// jobFail records a failure to even start the job, beside the job file. A run
// that fails this early (an unreadable job, an absent -out, an unopenable log)
// writes no archive-local last-run record — there may be no archive directory
// at all — so besides the .jobfail.log beside the job file and the desktop
// toast, it also leaves a single last-failure breadcrumb in the config dir that
// the GUI's launch health view can find later (friction #20).
func jobFail(path string, err error) {
	name := strings.TrimSuffix(filepath.Base(path), ".json")
	sink := filepath.Join(filepath.Dir(path), name+".jobfail.log")
	if f, e := os.OpenFile(sink, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); e == nil {
		fmt.Fprintf(f, "%s %v\n", time.Now().Format(time.RFC3339), err)
		f.Close()
	}
	recordHeadlessFailure(name, err)
	fmt.Fprintln(os.Stderr, "mailarchive-gui: "+err.Error())
	notifyFailure(name, "")
}

// notifyFailure raises a best-effort desktop notification that a scheduled
// backup failed, so a headless run that no one is watching does not fail
// silently (P3). A machine with no notification daemon simply shows nothing;
// success never notifies. The out argument is accepted for symmetry with the
// callers but the message names the backup by its schedule name.
func notifyFailure(name, out string) {
	_ = out // reserved for a future per-archive message; the schedule name is the identifier today
	_ = notify(fmt.Sprintf("Mail Archive backup failed for %s — open Mail Archive to see what went wrong.", name), zenity.Title(appTitle))
}

// dirSize sums the sizes of the regular files under root (best-effort), to
// report how much scratch space a cleanup reclaimed.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, e := d.Info(); e == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// cleanupOutlookScratch removes the scratch directory of freshly-written PSTs
// the Outlook-app path created (<out>/_outlook-pst) once the export that read
// them has SUCCEEDED, logging the space reclaimed. On failure the directory is
// kept so the run can be retried or inspected. Returns the bytes reclaimed.
func cleanupOutlookScratch(out string, exportOK bool, logger *log.Logger) int64 {
	if !exportOK {
		return 0
	}
	dir := filepath.Join(out, "_outlook-pst")
	n := dirSize(dir)
	if err := os.RemoveAll(dir); err != nil {
		logger.Printf("warning: could not remove temporary Outlook PSTs %s: %v", dir, err)
		return 0
	}
	if n > 0 {
		logger.Printf("Reclaimed %s by removing the temporary Outlook PST copy (%s).", thunderbird.HumanBytes(n), dir)
	}
	return n
}

func logSummary(logger *log.Logger, r app.Result) {
	s := r.Stats
	logger.Printf("Done. exported=%d filled=%d skipped(seen)=%d skipped(date)=%d attachments=%d fillable=%d terminal=%d unknown=%d index-errors=%d",
		s.Exported, s.Filled, s.SkippedManifest, s.SkippedDate, s.Attachments, r.Fillable, r.Terminal, r.Unknown, r.IndexErrors)
	if r.ReportPath != "" {
		logger.Printf("Verification: details in %s", r.ReportPath)
	}
}

// ---- the wizard -----------------------------------------------------------

// wizardChoice is what the wizard learned; the schedule step repeats it.
type wizardChoice struct {
	inputs     []string
	auto       bool // "all of them" from auto-detect: re-discover at run time
	out        string
	mode       export.Mode
	since      string
	copyFirst  bool
	outlookCOM bool
	keepRaw    bool // also keep each message's original .eml (raw-capable sources)
	// Deleted Items / Junk Email are excluded by default; set only for a
	// live-path (Graph) source, which the GUI does not offer yet (T7, §3.6).
	includeDeleted bool
	includeJunk    bool
}

func wizard() error {
	// 0. Health of the last archive this wizard scheduled, if any (S8): the
	//    double-click user has no other way to learn the backup stopped.
	if err := healthCheck(); err != nil {
		return err
	}

	// 1. Choose the source type, then pick the file/folder accordingly.
	choices := []string{srcAuto, srcOutlook}
	if runtime.GOOS == "windows" {
		choices = append(choices, srcOutlookCOM) // needs classic Outlook, Windows-only
	}
	choices = append(choices, srcThunderbird)
	if runtime.GOOS == "linux" {
		choices = append(choices, srcEvolution)
	}
	choices = append(choices, srcMbox)

	var (
		srcType    string
		autoInputs []string
		inputPath  string
		err        error
	)
	for {
		srcType, err = zenity.List("What are you exporting?", choices, zenity.Title(appTitle), zenity.DefaultItems(srcAuto))
		if err != nil {
			return err
		}
		if srcType != srcAuto {
			break
		}
		autoInputs, err = app.DiscoverInputs(nil, true)
		if err != nil {
			return err
		}
		if len(autoInputs) > 0 {
			break
		}
		// Nothing found: explain per-OS what that means and what to do (never
		// drop into a picker for a program the user may not have), then ask again.
		if err := zenity.Warning(nothingFoundMessage(runtime.GOOS), zenity.Title(appTitle)); err != nil {
			return err
		}
	}
	useOutlookCOM := srcType == srcOutlookCOM

	// On Windows with classic Outlook, a discovered live .ost is more reliably
	// read through the Outlook app than directly (its on-disk format varies).
	// Probe for Outlook only when a .ost is actually present, so a plain auto
	// run never spins up COM (which could launch Outlook) needlessly.
	if srcType == srcAuto && anyOST(autoInputs) && preferOutlookApp(runtime.GOOS, autoInputs, detectOutlook()) {
		q := zenity.Question(
			"A live Outlook (.ost) account was found. Reading it directly can miss mail Outlook hasn't fully downloaded to this computer; having the Outlook app export it first is more reliable.\n\nUse the Outlook app to export this account?",
			zenity.Title(appTitle),
			zenity.OKLabel("Use the Outlook app"),
			zenity.CancelLabel("Read the files directly"),
		)
		if q == nil {
			srcType, useOutlookCOM = srcOutlookCOM, true
		} else if !errors.Is(q, zenity.ErrCanceled) {
			return q
		}
	}

	switch srcType {
	case srcOutlookCOM, srcAuto:
		// Nothing to pick.
	case srcThunderbird:
		inputPath, err = zenity.SelectFile(zenity.Title("Select the Thunderbird mail folder (e.g. …/ImapMail/<account>)"), zenity.Directory())
	case srcEvolution:
		inputPath, err = zenity.SelectFile(zenity.Title("Select the Evolution mail store (~/.local/share/evolution/mail/local or ~/.cache/evolution/mail/<account>)"), zenity.Directory())
	case srcMbox:
		inputPath, err = zenity.SelectFile(zenity.Title("Select an mbox file"))
	default:
		inputPath, err = zenity.SelectFile(
			zenity.Title("Select an Outlook data file (.pst or .ost)"),
			zenity.FileFilters{{Name: "Outlook data files", Patterns: []string{"*.pst", "*.ost"}, CaseFold: true}},
		)
	}
	if err != nil {
		return err
	}

	choice := wizardChoice{outlookCOM: useOutlookCOM}
	switch {
	case useOutlookCOM:
	case srcType == srcAuto:
		choice.inputs, choice.auto, err = pickAutoInputs(autoInputs)
		if err != nil {
			return err
		}
	default:
		choice.inputs = []string{inputPath}
	}

	// 1a. IMAP prep, for every input that needs it (auto-detected ones too):
	//     a Thunderbird IMAP account is offered a full offline download; an
	//     Outlook .ost gets the equivalent guidance.
	prepared := false
	ostNoted := false
	for _, in := range choice.inputs {
		fi, statErr := os.Stat(in)
		switch {
		case statErr != nil:
		case fi.IsDir() && thunderbird.IsImapStore(in):
			p, err := prepareThunderbirdGUI(in)
			if err != nil {
				return err
			}
			prepared = prepared || p
		case !fi.IsDir() && strings.EqualFold(filepath.Ext(in), ".ost") && !ostNoted:
			ostNoted = true
			if err := outlookOfflineNote(); err != nil {
				return err
			}
		}
	}

	// 2. Choose the output folder.
	choice.out, err = zenity.SelectFile(zenity.Title("Select the output folder"), zenity.Directory())
	if err != nil {
		return err
	}

	// 3. Export mode — not asked when the prep just forced a full re-export
	//    (asking, then overriding the answer, would be a lie).
	choice.mode = export.Incremental
	if prepared {
		choice.mode = export.Full
		if err := zenity.Info("Because a full offline download was just prepared, this run exports everything (Full mode) so messages captured earlier without their content are rewritten.\n\nLater runs can be incremental.", zenity.Title(appTitle)); err != nil && !errors.Is(err, zenity.ErrCanceled) {
			return err
		}
	} else {
		const modeInc = "Incremental — only messages new since the last run (and any still missing content)"
		const modeFull = "Full — export everything again"
		modeChoice, err := zenity.List("Export mode:", []string{modeInc, modeFull}, zenity.Title(appTitle), zenity.DefaultItems(modeInc))
		if err != nil {
			return err
		}
		if modeChoice == modeFull {
			choice.mode = export.Full
		}
	}

	// 4. Optional date window (re-prompt until valid or cancelled).
	choice.since, err = askSince()
	if err != nil {
		return err
	}

	// 5. Is the mail app open? Only meaningful for a data FILE, which may be
	//    locked; folders are read in place and the Outlook-app path needs
	//    Outlook running anyway.
	if choice.outlookCOM == false && anyRegularFile(choice.inputs) {
		openChoice, err := zenity.List(
			"Is your mail app (Outlook / Thunderbird) currently open?",
			[]string{"No", "Yes — copy the file first to avoid a lock"},
			zenity.Title(appTitle), zenity.DefaultItems("No"))
		if err != nil {
			return err
		}
		choice.copyFirst = strings.HasPrefix(openChoice, "Yes")
	}

	// 5b. Keep the original messages too? Only sources that carry raw RFC-822
	//     bytes (mbox/maildir readers) can honour it — a .pst/.ost item and the
	//     Outlook-app export path have no originals to keep, so they aren't asked.
	if keepRawWorthAsking(srcType, choice.inputs) {
		rawChoice, err := zenity.List(
			"Also keep a copy of each original message (.eml) so you can re-import it into a mail program later? (uses more disk space)",
			[]string{"No", "Yes — keep the original .eml files too"},
			zenity.Title(appTitle), zenity.DefaultItems("No"))
		if err != nil {
			return err
		}
		choice.keepRaw = strings.HasPrefix(rawChoice, "Yes")
	}

	// 5c. Include the Deleted Items / Junk Email folders? Excluded by default;
	//     a click-through excludes them (T7). Only asked for a source whose walk
	//     can honor the exclusion by resolved well-known-folder id — a live-path
	//     (Graph) source. The GUI reads only local stores today, so the step is
	//     suppressed rather than asking a question the local import cannot honor
	//     (deletedJunkApplies); the pure decision is exercised by the tests.
	if deletedJunkApplies(srcType) {
		choice.includeDeleted, choice.includeJunk, err = askDeletedJunk()
		if err != nil {
			return err
		}
	}

	// 6. Run with a progress dialog, then offer to keep it current.
	return runExport(choice)
}

// includeDeletedJunk maps the wizard's Deleted-Items/Junk answer to the two
// include flags. The dialog defaults to the exclude item, so a click-through
// (accepting the default — or any answer that is not the explicit include item)
// leaves both folders EXCLUDED (T7, operator ruling); only the explicit include
// answer opts in. Pure, so the default-excluded contract is testable without a
// dialog (the GUI is lab-tier).
func includeDeletedJunk(answer string) (includeDeleted, includeJunk bool) {
	if answer == deletedJunkIncludeItem {
		return true, true
	}
	return false, false
}

// deletedJunkApplies reports whether the Deleted-Items/Junk exclusion question
// is worth putting to the user for a source type. Exclusion is a LIVE-path
// (Graph) mechanism resolved by well-known-folder id (§3.6); the sources this
// GUI reads today are one-shot LOCAL imports (Outlook data files, Thunderbird/
// Evolution/mbox), which have no such mechanism — asking a question we could not
// honor would be a lie (cf. keepRawWorthAsking), so it is not asked. It becomes
// reachable when a server-side (Graph) source is offered in the GUI.
func deletedJunkApplies(srcType string) bool {
	// The set of live/server-side GUI sources — currently empty, since the GUI
	// offers only local imports. A future Graph source is added here.
	return liveGUISources[srcType]
}

// liveGUISources are the source types whose walk honors the well-known-folder
// exclusion (Graph); the GUI has none yet (§3.6).
var liveGUISources = map[string]bool{}

// askDeletedJunk asks whether to include the Deleted Items and Junk Email
// folders, defaulting the dialog to EXCLUDE so a click-through excludes them
// (T7). The answer→flags mapping is the pure includeDeletedJunk.
func askDeletedJunk() (includeDeleted, includeJunk bool, err error) {
	answer, err := zenity.List(
		"Include the Deleted Items and Junk Email folders in the archive?",
		[]string{deletedJunkExcludeItem, deletedJunkIncludeItem},
		zenity.Title(appTitle), zenity.DefaultItems(deletedJunkExcludeItem))
	if err != nil {
		return false, false, err
	}
	d, j := includeDeletedJunk(answer)
	return d, j, nil
}

// keepRawApplies reports whether the "also keep the original .eml" question is
// worth asking for a source type: only readers that yield raw RFC-822 bytes
// (Thunderbird/mbox, Evolution, a single mbox, and auto-detect, which may find
// any of those) can honour -raw. A .pst/.ost item has no raw bytes, and the
// Outlook-app (COM) path exports via a PST, so neither is asked.
func keepRawApplies(srcType string) bool {
	switch srcType {
	case srcThunderbird, srcEvolution, srcMbox, srcAuto:
		return true
	default: // srcOutlook (.pst/.ost), srcOutlookCOM
		return false
	}
}

// keepRawWorthAsking reports whether the keep-raw question is worth putting to
// the user for this run: the source type must be raw-capable (keepRawApplies)
// AND at least one chosen input must be able to yield raw bytes. On the
// auto-detect path a run may resolve to only Outlook data files (.pst/.ost),
// which carry no raw .eml to keep — asking there is a dead question, so it is
// skipped (friction #24). A folder source or any non-Outlook input still asks.
func keepRawWorthAsking(srcType string, inputs []string) bool {
	if !keepRawApplies(srcType) {
		return false
	}
	return !allOutlookFiles(inputs)
}

// isOutlookFile reports whether p is an Outlook data file by extension.
func isOutlookFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".pst", ".ost":
		return true
	default:
		return false
	}
}

// allOutlookFiles reports whether every path is an Outlook data file. An empty
// list is not "all Outlook" (nothing is known yet), so it does not suppress.
func allOutlookFiles(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if !isOutlookFile(p) {
			return false
		}
	}
	return true
}

// preferOutlookApp reports whether the auto-detect result should steer the user
// to the Outlook-app (COM) path instead of reading a file directly: on Windows,
// with classic Outlook present, when a discovered store is a live .ost, whose
// on-disk format go-pst may not read.
func preferOutlookApp(goos string, discovered []string, outlookDetected bool) bool {
	return goos == "windows" && outlookDetected && anyOST(discovered)
}

// anyOST reports whether any discovered path is an Outlook .ost cache.
func anyOST(paths []string) bool {
	for _, p := range paths {
		if strings.EqualFold(filepath.Ext(p), ".ost") {
			return true
		}
	}
	return false
}

// nothingFoundMessage explains, per OS, why auto-detect found no mailboxes and
// what to do next — so the macOS / New Outlook user is not dropped into a file
// picker with no context. It always ends by pointing back at the type list.
func nothingFoundMessage(goos string) string {
	switch goos {
	case "darwin":
		return "No mailboxes were found automatically.\n\n" +
			"On a Mac, Apple Mail is not supported, and Outlook for Mac / New Outlook keep no local mail files this tool can read.\n\n" +
			"A Microsoft 365 mailbox can be archived server-side by an administrator instead — see docs/graph-app-setup.md.\n\n" +
			"If you have a copied .pst, .ost or mbox file from another machine, choose its type next and pick the file."
	default:
		return "No Outlook, Thunderbird, or Evolution mailboxes were found automatically.\n\n" +
			"New Outlook keeps no .pst/.ost files, and a Microsoft 365 mailbox is archived server-side by an administrator (see docs/graph-app-setup.md).\n\n" +
			"Otherwise choose the type below and pick the file or folder yourself."
	}
}

func anyRegularFile(paths []string) bool {
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// ---- health view on launch (S8) --------------------------------------------

func lastArchiveFile() (string, error) {
	d, err := job.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "last-archive.txt"), nil
}

func rememberArchive(out string) {
	if p, err := lastArchiveFile(); err == nil {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(out+"\n"), 0o600)
	}
}

// healthCheck shows the health of the last archive this wizard worked on when
// a schedule is recorded there, beside a "remove the scheduled backup" choice.
func healthCheck() error {
	p, err := lastArchiveFile()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	out := strings.TrimSpace(string(data))

	// A headless run whose -out was absent leaves no archive-local record — the
	// config-dir breadcrumb is the only trace (friction #20). Read it up front so
	// it can be shown even when the archive itself is now unreachable.
	var fail lastFailure
	var haveFail bool
	if dir, e := failureDir(); e == nil {
		fail, haveFail = readLastFailure(dir)
	}

	if _, err := schedule.ReadDescriptor(out); err != nil {
		// No readable schedule at the last archive path (or the archive is
		// unreachable, e.g. an unmounted drive). Nothing else to report, but a
		// recent start-failure still deserves a warning.
		if showLastFailure(fail, haveFail, time.Time{}, false) {
			return zenity.Warning(lastFailureLine(fail), zenity.Title(appTitle))
		}
		return nil
	}
	in := health.Gather(out, "")
	rep := health.Assess(in, time.Now())
	lines := health.Summary(in, rep)
	var lastRun time.Time
	reachable := in.HasManifest || in.LastRunState == state.LastRunPresent
	if in.LastRunState == state.LastRunPresent {
		lastRun = in.LastRun.Started
	}
	if showLastFailure(fail, haveFail, lastRun, reachable) {
		lines = append([]string{lastFailureLine(fail), ""}, lines...)
	}
	text := strings.Join(lines, "\n")

	const cont = "Continue to the wizard"
	const repair = "Repair the scheduled backup"
	const remove = "Remove the scheduled backup"
	choices := []string{cont}
	if repairable(in) {
		choices = append(choices, repair)
	}
	choices = append(choices, remove)
	pick, err := zenity.List("Backup health — "+rep.Posture+"\n\n"+text, choices, zenity.Title(appTitle), zenity.DefaultItems(cont))
	if err != nil {
		return err
	}
	switch pick {
	case repair:
		return repairSchedule(out, in.Desc)
	case remove:
		return removeSchedule(out)
	default:
		return nil
	}
}

// repairable reports whether the launch health view should offer to re-install
// the schedule: there is a descriptor, it was installed on THIS host, and the
// schedule is in a state a re-install from the current binary fixes — not
// present in the scheduler, or its program moved or gone. A schedule recorded
// for another host cannot be repaired from here.
func repairable(in health.Input) bool {
	if !in.HasDescriptor {
		return false
	}
	if in.ThisHost != "" && in.Desc.Host != "" && in.ThisHost != in.Desc.Host {
		return false
	}
	return in.SchedState == schedule.NotInstalled || !in.ExeExists || !in.ExeIsThis
}

// repairSpec rebuilds the install Spec for a recorded schedule using the CURRENT
// executable, so re-installing points the scheduler back at this binary. The
// descriptor lives in the archive directory and is untrusted (any writer of that
// directory could edit it), so — exactly as offerSchedule builds a fresh install
// and as the INS-7 fix already treats the name/wrapper path — the job args and
// log are RECONSTRUCTED from the sanitized name and -out, never copied from the
// descriptor's Job/Log/Exe. A tampered descriptor therefore cannot redirect the
// scheduled `-job` at an attacker-writable file. Only the cadence and time are
// taken from the descriptor (they name no path and cannot escalate).
func repairSpec(out string, d schedule.Descriptor, exe string) (schedule.Spec, error) {
	name, err := schedule.SanitizeName(d.Name)
	if err != nil {
		return schedule.Spec{}, err
	}
	jobPath, err := job.PathFor(name)
	if err != nil {
		return schedule.Spec{}, err
	}
	iv, _ := schedule.ParseInterval(d.Interval)
	at := d.At
	if strings.ContainsRune(at, '?') || strings.TrimSpace(at) == "" {
		at = "02:00" // ReadDescriptor parks an unreadable time as "??:??"
	}
	spec := schedule.Spec{
		Name: name, Interval: iv, At: at, Exe: exe,
		Args: []string{"-job", jobPath}, Log: filepath.Join(out, "mailarchive.log"), Out: out,
		WrapperPath: schedule.DefaultWrapperPath(name),
	}
	// No console window when the direct run string fits; otherwise the wrapper
	// is the price of a working job (S12) — mirrors offerSchedule.
	spec.Wrapper = runtime.GOOS == "windows" && spec.TaskRunLength() > schedule.SchtasksRunLimit
	if err := spec.Validate(); err != nil {
		return schedule.Spec{}, err
	}
	return spec, nil
}

// repairSchedule re-installs a recorded schedule from the current executable.
func repairSchedule(out string, d schedule.Descriptor) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	spec, err := repairSpec(out, d, exe)
	if err != nil {
		return err
	}
	if err := schedule.Install(spec); err != nil {
		return fmt.Errorf("could not repair the scheduled backup:\n%v", err)
	}
	rememberArchive(out)
	// The repair is done; offer to continue into the wizard (run an export now)
	// rather than forcing a re-launch — a returning user who repairs a schedule
	// often wants to run one too (friction #24). "Close" exits quietly.
	q := zenity.Question(
		fmt.Sprintf("Repaired the scheduled backup %q for\n%s\n\nIt now runs this copy of the program. Re-open this program any time to check the backup's health.\n\nContinue to run an export now?", spec.Name, out),
		zenity.Title(appTitle),
		zenity.OKLabel("Continue to the wizard"),
		zenity.CancelLabel("Close"),
	)
	cont, err := repairFollowUp(q)
	switch {
	case err != nil:
		return err
	case cont:
		return nil // fall through into the wizard
	default:
		return zenity.ErrCanceled // quiet exit in main
	}
}

// repairFollowUp interprets the user's answer to the post-repair "continue?"
// prompt: OK (nil) continues into the wizard; Cancel or a dismissed dialog
// (ErrCanceled) stops quietly; any other error propagates. Pure so the branch
// is testable without a display.
func repairFollowUp(choice error) (cont bool, err error) {
	switch {
	case choice == nil:
		return true, nil
	case errors.Is(choice, zenity.ErrCanceled):
		return false, nil
	default:
		return false, choice
	}
}

func removeSchedule(out string) error {
	d, err := schedule.ReadDescriptor(out)
	if err != nil {
		return err
	}
	name, err := schedule.SanitizeName(d.Name)
	if err != nil {
		return err
	}
	iv, _ := schedule.ParseInterval(d.Interval)
	// The wrapper path is derived from the name, never taken from the file:
	// the descriptor is not trusted to name a file to delete.
	spec := schedule.Spec{Name: name, Interval: iv, At: "02:00", Exe: d.Exe, Out: out, WrapperPath: schedule.DefaultWrapperPath(name)}
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := schedule.Remove(spec); err != nil {
		return err
	}
	if jp, err := job.PathFor(d.Name); err == nil {
		_ = os.Remove(jp)
	}
	return zenity.Info(fmt.Sprintf("Removed the scheduled backup %q for\n%s", d.Name, out), zenity.Title(appTitle))
}

// ---- IMAP prep ------------------------------------------------------------

// prepareThunderbirdGUI offers, for a Thunderbird IMAP account, to enable
// offline download and guide a full sync so nothing is missed. It returns
// whether preparation was done (in which case the export should run in Full
// mode). Non-IMAP stores need no preparation.
func prepareThunderbirdGUI(store string) (bool, error) {
	if !thunderbird.IsImapStore(store) {
		return false, nil
	}

	choice := zenity.Question(
		"This looks like an IMAP account.\n\nThunderbird stores mail locally only on demand, so the export may be missing messages or attachments that haven't been downloaded yet.\n\nPrepare a full offline download first?",
		zenity.Title(appTitle),
		zenity.OKLabel("Yes, prepare"),
		zenity.CancelLabel("No, export what's cached"),
	)
	if errors.Is(choice, zenity.ErrCanceled) {
		return false, nil
	}
	if choice != nil {
		return false, choice
	}

	profile, ok := thunderbird.FindProfileDir(store)
	if !ok {
		return false, zenity.Warning("Couldn't find the Thunderbird profile; exporting whatever is cached.", zenity.Title(appTitle))
	}
	acct, _ := thunderbird.FindAccountForStore(profile, store)

	// prefs.js must not be edited while Thunderbird is running.
	for acct != nil && thunderbird.Running(profile) {
		e := zenity.Question(
			"Please QUIT Thunderbird completely, then click \"I've quit it\".\n(Required to change the offline setting.)",
			zenity.Title(appTitle),
			zenity.OKLabel("I've quit it"),
			zenity.CancelLabel("Skip this step"),
		)
		if errors.Is(e, zenity.ErrCanceled) {
			acct = nil // skip enabling, but still guide the sync
			break
		}
		if e != nil {
			return false, e
		}
	}

	if acct != nil && !acct.OfflineDownload {
		if _, _, e := thunderbird.EnableOffline(profile, acct.ServerKey); e != nil {
			return false, zenity.Error("Couldn't enable offline download:\n"+e.Error(), zenity.Title(appTitle))
		}
	}

	// Guide the sync, then watch the store until the download stops growing.
	if e := guiWaitForSync(store); e != nil {
		return false, e
	}
	return true, nil
}

// guiWaitForSync shows a live progress dialog while the user runs Download/Sync
// Now in Thunderbird, watching the store size and finishing automatically once
// it stops growing (or the user closes the dialog).
func guiWaitForSync(store string) error {
	dlg, err := zenity.Progress(zenity.Title(appTitle), zenity.Pulsate())
	if err != nil {
		return err
	}
	defer dlg.Close()

	const stableFor = 45 * time.Second // extra slack while Thunderbird starts up
	const poll = 2 * time.Second
	w := thunderbird.NewStableWaiter(store, stableFor, time.Now())
	start := w.Size()

	_ = dlg.Text("Start Thunderbird, then right-click the account → Download / Sync Now.\nThis finishes automatically when the download stops.")

	for {
		select {
		case <-dlg.Done(): // user closed/cancelled the dialog → continue now
			return nil
		default:
		}

		time.Sleep(poll)
		size, stable := w.Poll(time.Now())
		_ = dlg.Text(fmt.Sprintf(
			"Downloaded %s so far (+%s).\nWaiting for the sync to finish…\n\nIn Thunderbird: right-click the account → Download / Sync Now.",
			thunderbird.HumanBytes(size), thunderbird.HumanBytes(size-start)))
		if stable {
			_ = dlg.Text("Download complete — starting export…")
			_ = dlg.Complete()
			return nil
		}
	}
}

// outlookOfflineNote shows the Outlook equivalent guidance (we can't change the
// setting programmatically, so we explain how).
func outlookOfflineNote() error {
	e := zenity.Info(
		"Outlook (.ost) accounts cache mail on demand.\n\nIf Cached Exchange Mode uses a limited window (\"Mail to keep offline\"), older mail may be header-only and won't fully export.\n\nTo include everything, in Outlook set:\n    Account Settings → Change → Mail to keep offline → All\nthen Send / Receive → Update Folder, and export again — an incremental run fills what was missing.\n\nClick OK to continue with what's currently cached.",
		zenity.Title(appTitle),
	)
	if errors.Is(e, zenity.ErrCanceled) {
		return nil
	}
	return e
}

// askSince prompts for a date window, re-prompting on invalid input. An empty
// value means "export everything".
func askSince() (string, error) {
	for {
		val, err := zenity.Entry(
			"Only export items newer than\n(e.g. 30d, 4w, 720h, or a date like 2026-07-01).\n\nLeave blank to export everything.",
			zenity.Title("Date window"),
			zenity.EntryText(""),
		)
		if err != nil {
			return "", err
		}
		val = strings.TrimSpace(val)
		if val == "" {
			return "", nil
		}
		if _, perr := util.ParseSince(val, time.Now()); perr == nil {
			return val, nil
		}
		if e := zenity.Error(
			fmt.Sprintf("Couldn't understand %q.\nTry 30d, 4w, 720h, or a date like 2026-07-01.", val),
			zenity.Title("Invalid date window"),
		); e != nil {
			return "", e
		}
	}
}

// pickAutoInputs lets the user choose one of the auto-detected stores, or all
// of them. "All of them" is remembered as auto-discovery, so a scheduled run
// picks up accounts added later.
//
// The list shows a legible label per store (the .pst/.ost file name, or a mail
// folder's store name) rather than the raw path: the discovered paths are long
// and identical up to the point the dialog truncates them, so a raw list is
// unreadable and indistinguishable.
func pickAutoInputs(found []string) ([]string, bool, error) {
	const allOfThem = "➤ All of them (and any added later)"
	if len(found) == 1 {
		return found, false, nil
	}

	labels := make([]string, 0, len(found)+1)
	labels = append(labels, allOfThem)
	byLabel := make(map[string]string, len(found))
	counts := map[string]int{}
	for _, p := range found {
		base := autoInputLabel(p)
		counts[base]++
		label := base
		if counts[base] > 1 { // disambiguate identical names
			label = fmt.Sprintf("%s (%d)", base, counts[base])
		}
		byLabel[label] = p
		labels = append(labels, label)
	}

	choice, err := zenity.List(
		"These mailboxes were found. Export which one?",
		labels,
		zenity.Title(appTitle),
		zenity.DefaultItems(allOfThem),
	)
	if err != nil {
		return nil, false, err
	}
	if choice == allOfThem {
		return found, true, nil
	}
	if p, ok := byLabel[choice]; ok {
		return []string{p}, false, nil
	}
	return []string{choice}, false, nil // fallback: treat the choice as a path
}

// autoInputLabel returns a human-readable label for a discovered store: the file
// name for an Outlook .pst/.ost, or the store name for a mail-store directory
// (Thunderbird account, Evolution account/local store).
func autoInputLabel(p string) string {
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		if s, err := source.Open(p); err == nil {
			name := strings.TrimSpace(s.StoreName())
			s.Close()
			if name != "" {
				return name
			}
		}
		return filepath.Base(strings.TrimRight(p, string(os.PathSeparator)))
	}
	return filepath.Base(p)
}

// ---- the run and the summary ------------------------------------------------

func runExport(c wizardChoice) error {
	logger := newLogger(c.out)

	// For the Outlook-automation path, show the completeness caveat and confirm
	// before spending time on a Send/Receive + PST copy.
	if c.outlookCOM {
		q := zenity.Question(
			outlookcom.CompletenessNote+"\n\nI'll run Send/Receive, wait a few minutes, then export each account. Continue?",
			zenity.Title(appTitle),
			zenity.OKLabel("Continue"),
			zenity.CancelLabel("Cancel"),
		)
		if errors.Is(q, zenity.ErrCanceled) {
			return nil
		}
		if q != nil {
			return q
		}
	}

	dlg, err := zenity.Progress(zenity.Title("Exporting…"), zenity.Pulsate())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancelling the progress dialog cancels the export.
	go func() {
		<-dlg.Done()
		cancel()
	}()

	inputs := c.inputs
	// Outlook-automation path: have Outlook write a fresh PST per account, then
	// archive those. Reliable for Exchange/IMAP .ost caches go-pst can't read.
	if c.outlookCOM {
		_ = dlg.Text("Running Send/Receive, then exporting each account to a PST…\nThis can take several minutes for large mailboxes.")
		stores, cerr := outlookcom.CreatePSTs(filepath.Join(c.out, "_outlook-pst"),
			outlookcom.Options{Sync: true, SyncWait: 5 * time.Minute}, logger)
		if cerr != nil {
			_ = dlg.Close()
			return cerr
		}
		inputs = nil
		for _, s := range stores {
			inputs = append(inputs, s.Path)
		}
	}

	var since time.Time
	if c.since != "" {
		since, _ = util.ParseSince(c.since, time.Now())
	}
	opts := app.Options{
		Inputs:    inputs,
		Auto:      c.auto,
		Out:       c.out,
		Mode:      c.mode,
		Since:     since,
		CopyFirst: c.copyFirst,
		Index:     true,
		Pages:     true,
		KeepRaw:   c.keepRaw,
	}

	var ticks int
	result, runErr := app.Run(ctx, opts, logger, func(s export.Stats) {
		ticks++
		if ticks%20 == 0 {
			_ = dlg.Text(fmt.Sprintf("exported %d · filled %d · skipped %d · attachments %d",
				s.Exported, s.Filled, s.SkippedManifest+s.SkippedDate, s.Attachments))
		}
	})

	_ = dlg.Complete()
	_ = dlg.Close()

	if errors.Is(runErr, context.Canceled) {
		return zenity.Info(
			fmt.Sprintf("Export cancelled.\n\nExported %d message(s) before stopping.\nProgress was saved — run again to resume.",
				result.Stats.Exported),
			zenity.Title(appTitle),
		)
	}
	if runErr != nil {
		msg := fmt.Sprintf("%v\n\nA log was written to:\n%s", runErr, filepath.Join(c.out, "mailarchive.log"))
		if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(runErr.Error()), ".ost") {
			msg += "\n\nA live Exchange/IMAP .ost can't always be read directly: run the wizard again and choose \"Outlook account (via Outlook app)\", which has Outlook write a clean .pst first."
		}
		return errors.New(msg)
	}
	rememberArchive(c.out)
	// The Outlook-app path wrote scratch PSTs under <out>/_outlook-pst; the
	// export that read them succeeded, so reclaim that space.
	if c.outlookCOM {
		cleanupOutlookScratch(c.out, true, logger)
	}

	// Finish line: show the summary and offer to open the archive, then continue
	// to the schedule offer exactly as before.
	open := zenity.Question(exportSummary(c.out, c.keepRaw, result), zenity.Title(appTitle),
		zenity.OKLabel("Open the archive"), zenity.CancelLabel("Close"))
	if open == nil {
		openPath(filepath.Join(c.out, "index.html"))
	} else if !errors.Is(open, zenity.ErrCanceled) {
		return open
	}

	// 7. Keep it current?
	return offerSchedule(c)
}

// exportSummary renders the success summary shown after a run. It is a pure
// function of the run's counts (the same numbers `status` reports — ux-contract
// X6) and the choice, worded for someone who has never met the engine, so a unit
// test can assert both the counts and the wording without a display.
func exportSummary(out string, keepRaw bool, r app.Result) string {
	s := r.Stats
	b := &strings.Builder{}
	fmt.Fprintf(b,
		"Export complete.\n\n"+
			"Newly downloaded this run: %d\n"+
			"Exported:                  %d\n"+
			"Skipped (already done):    %d\n"+
			"Skipped (date filter):     %d\n"+
			"Attachments archived:      %d\n",
		s.Filled, s.Exported, s.SkippedManifest, s.SkippedDate, s.Attachments)

	notDone := r.Fillable + r.Unknown
	if notDone > 0 {
		line := fmt.Sprintf("\nNot fully downloaded yet: %d — open these in your mail app so it downloads them, then run this again to add them", notDone)
		if r.Unknown > 0 {
			line += fmt.Sprintf(" (%d are from an older archive and get re-checked automatically each run)", r.Unknown)
		}
		b.WriteString(line + "\n")
	}
	if r.Terminal > 0 {
		fmt.Fprintf(b, "Empty at the source (nothing to download): %d\n", r.Terminal)
	}
	if (notDone > 0 || r.Terminal > 0) && r.ReportPath != "" {
		fmt.Fprintf(b, "Details: %s\n", r.ReportPath)
	}
	if r.IndexErrors > 0 {
		fmt.Fprintf(b, "\nWARNING: %d message(s) could not be indexed for search.\n", r.IndexErrors)
	}
	if keepRaw {
		b.WriteString("\nEach message is saved twice: an .html page to read, and an .eml file to import back into a mail program if you ever need to.\n")
	}
	fmt.Fprintf(b, "\nOutput folder:\n%s\n\nOpen index.html there to browse — no extra software needed. Each folder page has a box to filter within that folder; to search the whole archive at once, use the command-line tool: mailarchive serve -out %q", out, out)
	return b.String()
}

// openPathArgv is the detached command that opens a file or folder with the OS's
// default handler. It is a pure function of GOOS so a unit test can assert the
// argv without a display and without launching anything.
func openPathArgv(goos, p string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{p}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", p}
	default:
		return "xdg-open", []string{p}
	}
}

// openPath opens p with the OS's default handler, detached and best-effort: a
// GUI has no console to show a launch error, and the summary already named the
// folder, so a failure is silent.
func openPath(p string) {
	name, args := openPathArgv(runtime.GOOS, p)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err == nil && cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

// ---- keep it current (P7) --------------------------------------------------

func offerSchedule(c wizardChoice) error {
	const no = "No — just this once"
	const daily = "Yes — every day at 02:00"
	const weekly = "Yes — every week, Sunday 03:00"
	pick, err := zenity.List(
		"Keep this archive current automatically?\n\nA scheduled run repeats exactly this export (incremental: only new mail and any content that was still missing) while you are logged in.",
		[]string{no, daily, weekly}, zenity.Title(appTitle), zenity.DefaultItems(no))
	if err != nil {
		if errors.Is(err, zenity.ErrCanceled) {
			return nil
		}
		return err
	}
	if pick == no {
		return nil
	}
	iv, at := schedule.Daily, "02:00"
	if pick == weekly {
		iv, at = schedule.Weekly, "03:00"
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if inVolatileDir(exe) {
		q := zenity.Question(
			fmt.Sprintf("This program runs from\n%s\nwhich is a place that tends to get cleaned up or replaced. If it moves, the scheduled backup silently stops.\n\nMove it somewhere permanent first (e.g. your Applications or Program Files folder) and run the wizard again — or continue anyway.", filepath.Dir(exe)),
			zenity.Title(appTitle), zenity.OKLabel("Continue anyway"), zenity.CancelLabel("Cancel"))
		if errors.Is(q, zenity.ErrCanceled) {
			return nil
		}
		if q != nil {
			return q
		}
	}

	name := schedule.DefaultNameFor(c.out)
	jobPath, err := job.PathFor(name)
	if err != nil {
		return err
	}
	if err := job.Write(jobPath, jobFor(name, c)); err != nil {
		return err
	}

	spec := schedule.Spec{
		Name:        name,
		Interval:    iv,
		At:          at,
		Exe:         exe,
		Args:        []string{"-job", jobPath},
		Log:         filepath.Join(c.out, "mailarchive.log"),
		Out:         c.out,
		WrapperPath: schedule.DefaultWrapperPath(name),
	}
	// No console window when the direct run string fits; otherwise the
	// wrapper (and its brief console) is the price of a working job (S12).
	spec.Wrapper = runtime.GOOS == "windows" && spec.TaskRunLength() > schedule.SchtasksRunLimit
	if err := schedule.Install(spec); err != nil {
		return fmt.Errorf("could not install the schedule:\n%v", err)
	}
	rememberArchive(c.out)
	note := ""
	if runtime.GOOS == "windows" {
		note = "\n\nThe job runs only while you are logged in; a night the PC is off is skipped and shown as such."
		if spec.Wrapper {
			note += " A console window will appear briefly while it runs."
		}
	}
	return zenity.Info(fmt.Sprintf("Scheduled: %s at %s.\n\nArchive: %s\nLog: %s\n\nRe-open this program any time to check the backup's health.%s",
		iv, at, c.out, spec.Log, note), zenity.Title(appTitle))
}

// jobFor builds the scheduled-job file from the wizard's answers. A repeat is
// always incremental (it also fills what was still missing); everything else
// — inputs/auto, out, date window, copy-first, the Outlook-app path, and
// keep-raw — carries over so the headless run repeats exactly this export.
func jobFor(name string, c wizardChoice) job.Job {
	j := job.Job{
		Name: name, Inputs: c.inputs, Auto: c.auto, Out: c.out,
		Mode:  "incremental",
		Since: c.since, CopyFirst: c.copyFirst, Outlook: c.outlookCOM,
		KeepRaw: c.keepRaw,
	}
	if c.outlookCOM {
		j.Inputs = nil
		j.OutlookSyncWait = "5m"
	}
	return j
}

// inVolatileDir reports whether the executable lives somewhere that gets
// cleaned up or replaced: Downloads, Desktop, a temp dir.
func inVolatileDir(exe string) bool {
	low := strings.ToLower(filepath.ToSlash(exe))
	for _, needle := range []string{"/downloads/", "/desktop/", "/tmp/", "/temp/", "/appdata/local/temp/"} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

// newLogger writes a run log into the output folder (there is no console in a
// GUI build), appending with rotation; falls back to discarding output if the
// file can't be created.
func newLogger(outDir string) *log.Logger {
	if err := os.MkdirAll(outDir, 0o755); err == nil {
		if f, err := runlog.Open(filepath.Join(outDir, "mailarchive.log")); err == nil {
			return log.New(f, "", log.LstdFlags)
		}
	}
	return log.New(io.Discard, "", 0)
}
