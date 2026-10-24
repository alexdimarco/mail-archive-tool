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
package wizard

import (
	"context"
	"errors"
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
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/runlog"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/source"
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

// The wizard's first step (MA-651) opens the various options: the quick local-file
// importer below, or MailArchive Desktop — the dashboard that holds Microsoft 365
// sign-in, folder-preserving export, browsing, scheduling and the maintenance tools.
// The local item is the default, so a click-through stays on the quick path.
const (
	surfaceLocalItem   = "Archive a mail file on this computer (Outlook, Thunderbird, mbox)"
	surfaceDesktopItem = "Microsoft 365, folder export, browsing & tools (open MailArchive Desktop)"
)

// surfaceChoices is the first step's list, local item first (and defaulted).
var surfaceChoices = []string{surfaceLocalItem, surfaceDesktopItem}

// OpenDesktop launches MailArchive Desktop in the SAME process (MA-612/R33 — never a
// child). The cmd layer wires it: it may import internal/desktop, but the wizard may
// not (internal/desktop imports internal/wizard — a cycle). nil on a build that wired
// no desktop surface, and then openDesktopSurface names the command instead (MA-651).
var OpenDesktop func() error

// openDesktopUnwired is shown when no launcher was wired, so the choice still tells
// the user how to reach MailArchive Desktop rather than failing.
const openDesktopUnwired = "To open MailArchive Desktop, run this in a terminal:\n\n    mailarchive desktop\n\nIt opens a local control panel in your browser with Microsoft 365 sign-in, folder export, browsing and the maintenance tools."

// notify is the desktop-notification sink, overridable in tests. A scheduled
// (headless) run has no window, so a failure would otherwise be invisible until
// the user next opens the GUI (P3).
var notify = zenity.Notify

// detectOutlook reports whether classic Outlook COM automation is available on
// this machine; overridable in tests (the real probe is Windows-only).
var detectOutlook = func() bool { _, ok := outlookcom.Detect(); return ok }

// Run launches the interactive wizard, presenting any error — or a recovered
// panic — in a dialog, and returns the process exit code. It is what the
// `wizard` verb, the `mailarchive-gui` former name, and the retained
// mailarchive-gui binary all reach in-process (R33); it never starts a copy of
// itself (MA-612).
func Run() (code int) {
	// The GUI-subsystem build has no console, so an unhandled panic would exit
	// silently. Surface it on stderr and then in a dialog — in that order, as the
	// desktop's showRefusal does (§4.4, MA-639) — a visible error beats a vanish.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "mailarchive wizard: %v\n", r)
			_ = zenity.Error(fmt.Sprintf("Something went wrong:\n\n%v", r), zenity.Title(appTitle))
			code = 1
		}
	}()

	// The wizard is made of native dialogs; on Linux/BSD it cannot open them
	// without a dialog tool and a desktop session, so it refuses on stderr rather
	// than fail silently (§4.4, MA-639). It looks up a file and an environment
	// variable, and executes nothing.
	if refusal, ok := dialogPreflight(runtime.GOOS, dialogToolOnPath(), displayIsSet()); !ok {
		fmt.Fprintln(os.Stderr, refusal)
		return 1
	}

	switch err := wizard(); {
	case err == nil, errors.Is(err, zenity.ErrCanceled):
		// Clean finish, or the user dismissed a dialog: exit quietly.
		return 0
	default:
		fmt.Fprintln(os.Stderr, "mailarchive wizard: "+err.Error())
		_ = zenity.Error(err.Error(), zenity.Title(appTitle))
		return 1
	}
}

// dialogToolOnPath reports whether a zenity-compatible dialog tool is on PATH —
// the names ncruces/zenity drives on Linux and the BSDs.
func dialogToolOnPath() bool {
	for _, name := range []string{"zenity", "qarma", "matedialog"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

// displayIsSet reports whether a desktop session is advertised in the environment.
func displayIsSet() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// dialogPreflight decides whether the wizard can open its native dialogs. On
// Linux and the BSDs those dialogs are zenity (or a compatible tool), which needs
// the tool on PATH and a desktop session; when either is missing the wizard
// cannot show a window and must refuse on stderr rather than fail silently (§4.4,
// MA-639). macOS (osascript) and Windows (a native MessageBox) always have a
// dialog, so they always proceed. The refusal names no repair it cannot deliver
// (not MailArchive Desktop, which cannot repair a wizard-kind schedule). Pure, so
// MA-639 tables every (OS, lookup, display) case.
func dialogPreflight(goos string, dialogToolFound, displaySet bool) (refusal string, ok bool) {
	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		if dialogToolFound && displaySet {
			return "", true
		}
		return "mailarchive wizard: the wizard cannot open its windows. Why: it is made of native dialogs, which need the zenity tool and a desktop session. Do this: install the zenity package and run mailarchive wizard again from your desktop, or use the commands in the README (mailarchive -h).", false
	default:
		return "", true
	}
}

// ---- headless (scheduled) run ---------------------------------------------

// RunHeadless runs a saved job with no dialogs. Exit 1 on any failure, with
// the reason in the run log (or, when the job file itself is the problem, in
// the jobfail log beside it — a sink that does not depend on the job's out).
func RunHeadless(path string) int {
	j, err := job.Read(path)
	if err != nil {
		jobFail(path, err)
		return 1
	}
	// Unattended guard: an absent -out (the backup drive is not mounted) must
	// not become a fresh archive on the local disk that then reports "ok".
	if fi, err := os.Stat(j.Out); err != nil || !fi.IsDir() {
		e := fmt.Errorf("archive directory %s is not present — is the backup drive mounted? refusing to create a new archive elsewhere", j.Out)
		jobFail(path, e)
		fireBackupAlert(j.Out, backupalert.ResultFailed, e, 0, nil)
		return 1
	}
	// A paused archive is hard read-only: refuse before the run-log open and any PST
	// recreate, which write into the archive before the engine lock (S2-C2). This is
	// not a failure, so no jobFail record or notification — just stderr and a non-zero
	// exit; the lingering per-run scheduler stderr breadcrumb is the conceded residual
	// (Pause removes the schedule, §5.3.1 S2-C3).
	if err := seal.RefuseWrite(j.Out); err != nil {
		fmt.Fprintln(os.Stderr, "mailarchive run-job: "+err.Error())
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
	var outlookSkips []outlookcom.Skip
	var outlookExported []string // the PSTs this run's export wrote: a fresh image of the mailbox (NF1)
	if j.Outlook {
		wait := 5 * time.Minute
		if d, err := time.ParseDuration(j.OutlookSyncWait); err == nil {
			wait = d
		}
		stores, skips, cerr := outlookcom.CreatePSTs(filepath.Join(j.Out, "_outlook-pst"),
			outlookcom.Options{Sync: wait > 0, SyncWait: wait}, logger)
		if cerr != nil {
			logger.Printf("FAILED: %v", cerr)
			fmt.Fprintln(os.Stderr, "mailarchive run-job: "+cerr.Error())
			notifyFailure(j.Name, j.Out)
			fireBackupAlert(j.Out, backupalert.ResultFailed, cerr, 0, logger)
			return 1
		}
		inputs = nil
		for _, s := range stores {
			inputs = append(inputs, s.Path)
			outlookExported = append(outlookExported, s.Path)
		}
		outlookSkips = skips
	}
	// Unattended: a vanished input on this existing archive is recorded as a
	// failed run, exactly as the CLI's -unattended does (N13, R18). optsForJob
	// also installs the Thunderbird pre-sync when the job carries it (§4.2.5),
	// shared with the Run-now path so a scheduled run and a manual one behave alike.
	opts := optsForJob(j, inputs, logger.Writer())
	opts.OutlookSkips = outlookSkips
	opts.OutlookExported = outlookExported
	logger.Printf("Scheduled job %q: exporting to %s", j.Name, j.Out)
	result, runErr := app.Run(ctx, opts, logger, nil)
	// "Done." only for a run that finished or was cancelled: a refused or failed
	// run logs FAILED alone, never a Done above it (F11).
	if runErr == nil || errors.Is(runErr, context.Canceled) {
		logSummary(logger, result)
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		// An index refusal reads in the owner's words (this program has no
		// command line to run a `reindex` from), with the technical text kept
		// on the next line for whoever helps (F3).
		var ie *app.IndexOpenError
		if errors.As(runErr, &ie) {
			logger.Printf("FAILED: %s", ie.ForOwner(headlessRebuildAction))
			logger.Printf("detail: %v", runErr)
		} else {
			logger.Printf("FAILED: %v", runErr)
		}
		fmt.Fprintln(os.Stderr, "mailarchive run-job: "+runErr.Error()) // cron mail / stderr.log
		notifyFailure(j.Name, j.Out)
		fireBackupAlert(j.Out, backupalert.ResultFailed, runErr, 0, logger)
		return 1
	}
	// The Outlook-app path wrote scratch PSTs under <out>/_outlook-pst; once the
	// export that read them succeeded, reclaim that space (kept on failure).
	if j.Outlook && runErr == nil {
		cleanupOutlookScratch(j.Out, true, logger)
	}
	// A completed (not cancelled) local backup emails its success too, if alerts want it.
	if runErr == nil {
		fireBackupAlert(j.Out, backupalert.ResultOK, nil, result.Stats.Exported, logger)
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
	fmt.Fprintln(os.Stderr, "mailarchive run-job: "+err.Error())
	notifyFailure(name, "")
}

// notifyFailure raises a best-effort desktop notification that a scheduled
// backup failed, so a headless run that no one is watching does not fail
// silently (P3). A machine with no notification daemon simply shows nothing;
// success never notifies. The out argument is accepted for symmetry with the
// callers but the message names the backup by its schedule name.
func notifyFailure(name, out string) {
	_ = out // reserved for a future per-archive message; the schedule name is the identifier today
	_ = notify(fmt.Sprintf("Mail Archive backup failed for %s. Open the MailArchive wizard (mailarchive wizard) to see what went wrong.", name), zenity.Title(appTitle))
}

// fireBackupAlert sends the configured backup-result email for this scheduled LOCAL job,
// via the shared backupalert layer — the same one the Desktop's Microsoft 365 capture uses
// — so a local backup alerts from the one configuration the dashboard saves. It is the
// email twin of the local notifyFailure toast (both fire; the toast is on this machine, the
// email reaches whoever is configured). Best-effort: a mail problem never fails the backup.
func fireBackupAlert(out, result string, runErr error, messages int, logger *log.Logger) {
	backupalert.FireForRunDefault(out, result, runErr, messages, logger)
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
	// followLinks are the inputs whose folder links the user asked to have read
	// (owner ruling OR6: the per-input opt-in; a folder store only).
	followLinks []string
	// tbSync is the owner's "sync Thunderbird before each scheduled run" answer
	// (§4.2.6): it records job.ThunderbirdSync, so the scheduled/Run-now backup
	// pre-syncs. The profile is derived from the inputs and membership-checked at
	// RUN time (PC3), so none is recorded here. macOS and Windows only.
	tbSync bool
	// Deleted Items / Junk Email are excluded by default; set only for a
	// live-path (Graph) source, which the GUI does not offer yet (T7, §3.6).
	includeDeleted bool
	includeJunk    bool
}

// surfaceOpensDesktop reports whether the first step's pick opens MailArchive
// Desktop (the Microsoft 365 / tools surface) rather than continuing into the
// local-file wizard. Pure, so MA-651 tables it: only the Desktop item opens
// Desktop; the local item, an empty pick and anything else stay local.
func surfaceOpensDesktop(pick string) bool { return pick == surfaceDesktopItem }

// openDesktopSurface hands off to MailArchive Desktop IN-PROCESS (MA-612/R33 — never
// a child). If the cmd layer wired no launcher, it names the command instead of
// failing, so the choice is never a dead end (MA-651).
func openDesktopSurface() error {
	if OpenDesktop == nil {
		return zenity.Info(openDesktopUnwired, zenity.Title(appTitle))
	}
	return OpenDesktop()
}

func wizard() error {
	// 1. First step (MA-651): open the various options. The Desktop item hands off
	//    IN-PROCESS to MailArchive Desktop (Microsoft 365, folder export, browsing,
	//    scheduling, the maintenance tools); the local item (the default) stays on
	//    the quick path. zenity.List returns ErrCanceled on dismiss → quiet exit.
	switch pick, err := zenity.List("What would you like to do?", surfaceChoices, zenity.Title(appTitle), zenity.DefaultItems(surfaceLocalItem)); {
	case err != nil:
		return err
	case surfaceOpensDesktop(pick):
		return openDesktopSurface()
	}

	// 2. Health of the last archive this wizard scheduled, if any (S8): the
	//    double-click user has no other way to learn the backup stopped.
	if err := healthCheck(); err != nil {
		return err
	}

	// 3. The local add-an-archive flow, shared with the Desktop's "Add another
	//    archive" button (AddArchive calls it directly).
	return localArchiveFlow(nil)
}

// AddArchive runs the wizard's local add-an-archive flow directly — source choice
// through export — WITHOUT the first-step surface or the scheduled-archive health
// check. The Desktop's "Add another archive" button calls it IN PROCESS (desktop
// imports wizard; MA-612 — never a child), so a running Desktop can create a new
// local archive through the same native-dialog flow the double-click wizard uses.
// It returns the new archive's output folder, or "" when the user backed out of a
// dialog (zenity.ErrCanceled) — a cancel is "nothing added", not an error.
func AddArchive() (string, error) {
	if refusal, ok := dialogPreflight(runtime.GOOS, dialogToolOnPath(), displayIsSet()); !ok {
		return "", errors.New(refusal)
	}
	var out string
	err := localArchiveFlow(func(o string) { out = o })
	if errors.Is(err, zenity.ErrCanceled) {
		return "", nil
	}
	return out, err
}

// localArchiveFlow is the wizard's local path: choose the source, pick the files,
// prepare IMAP, choose the output folder and options, then export. Shared by the
// double-click wizard (after its first step and health check) and by AddArchive.
// chosen, when set, is called with the output folder just before the export runs,
// so a caller (AddArchive) can register the new archive even if the export is later
// cancelled.
func localArchiveFlow(chosen func(out string)) error {
	// 3. Choose the source type, then pick the file/folder accordingly.
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

	// 5b2. Read through the folder links of a mail folder? Not by default: a link in the
	//      store is recorded and not followed, and its gap says to answer this question.
	//      Asked only when a chosen input is a folder (a link lives in a folder store).
	if followLinksWorthAsking(srcType, choice.inputs) {
		linkChoice, err := zenity.List(
			followLinksQuestion,
			[]string{followLinksNo, followLinksYes},
			zenity.Title(appTitle), zenity.DefaultItems(followLinksNo))
		if err != nil {
			return err
		}
		choice.followLinks = followLinksFor(linkChoice, choice.inputs)
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

	// 6. Run with a progress dialog, then offer to keep it current. Tell the caller
	//    the chosen output folder first, so AddArchive can register the new archive
	//    (a cancel during the export still leaves it registered, nothing half-hidden).
	if chosen != nil {
		chosen(choice.out)
	}
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

// The directory-link opt-in question (owner ruling OR6). The default is No, so a
// click-through reads no link out of the store; only the explicit Yes opts the folder
// inputs in. The wording of the Yes answer is what the capture gap's remedy names.
const (
	followLinksQuestion = "Does this mail folder contain shortcuts (links) to other folders that should be read too? By default a link is not followed; each one is listed as not captured, so nothing is lost silently. Following links reads the folders they lead to, and the archive keeps using the same folder."
	followLinksNo       = "No — do not follow folder links"
	followLinksYes      = "Follow folder links"
)

// followLinksWorthAsking reports whether the directory-link question is worth putting
// to the user: a link lives in a FOLDER store (Thunderbird, Evolution, or what
// auto-detect found), never in a .pst/.ost, a single mbox file or the Outlook-app path,
// so it needs a chosen input that is a directory.
func followLinksWorthAsking(srcType string, inputs []string) bool {
	switch srcType {
	case srcThunderbird, srcEvolution, srcAuto:
	default:
		return false
	}
	return len(folderInputs(inputs)) > 0
}

// folderInputs are the chosen inputs that are directories.
func folderInputs(paths []string) []string {
	var out []string
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// followLinksFor maps the wizard's answer to the inputs that opt in: every folder input
// on the explicit Yes, none on anything else (the default answer, a cancel).
func followLinksFor(answer string, inputs []string) []string {
	if answer != followLinksYes {
		return nil
	}
	return folderInputs(inputs)
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

func lastArchiveFile() (string, error) { return job.LastArchiveFile() }

func rememberArchive(out string) {
	if p, err := lastArchiveFile(); err == nil {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(out+"\n"), 0o600)
	}
}

// currentArchive is the archive the wizard's health view reports on: the Desktop's
// authoritative active archive (desktop-settings "out") when one is set, else the
// wizard's own last-touched breadcrumb (last-archive.txt). Reading the same pointer the
// Desktop writes means an archive switched in the Desktop shows here too, instead of
// each surface tracking a different file (design-archive-manager §13 UX-2). When the
// Desktop's pointer is set, the breadcrumb is reconciled to it so the two never drift.
func currentArchive() string {
	if sp, err := job.DesktopSettingsFile(); err == nil {
		if out := job.ReadDesktopOut(sp); out != "" {
			reconcileBreadcrumb(out)
			return out
		}
	}
	p, err := lastArchiveFile()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// reconcileBreadcrumb points the wizard's last-touched breadcrumb (last-archive.txt) at
// out when it differs, so it tracks the Desktop's active archive rather than drifting
// from it (design-archive-manager §13 UX-2). Best-effort: the health view still works
// from out if the breadcrumb cannot be written.
func reconcileBreadcrumb(out string) {
	p, err := lastArchiveFile()
	if err != nil {
		return
	}
	if data, rerr := os.ReadFile(p); rerr == nil && strings.TrimSpace(string(data)) == out {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte(out+"\n"), 0o600)
}

// healthCheck shows the health of the last archive this wizard worked on when
// a schedule is recorded there, beside a "remove the scheduled backup" choice.
func healthCheck() error {
	out := currentArchive()
	if out == "" {
		return nil
	}

	// health.Gather is the one reader of the headless start-failure breadcrumb
	// (§4.6.6, MA-634): it raises start_failed in the posture (keyed on out, not
	// the archive-held Desc.Name), so the launch view no longer reads the file
	// itself and shows the line that comes back like any other reason.
	in := health.Gather(out, "")
	in.Surface = "gui" // name the rebuild choice below, not a command line
	rep := health.Assess(in, time.Now())
	// No schedule recorded here and no start-failure to report: stay quiet, as
	// before — the launch view does not nag an archive with no backup set up.
	if !in.HasDescriptor && !in.HasStartFailure {
		return nil
	}
	lines := health.Summary(in, rep)
	text := strings.Join(lines, "\n")

	const cont = "Continue to the wizard"
	const repair = "Repair the scheduled backup"
	const remove = "Remove the scheduled backup"
	choices := []string{cont}
	if repairable(in) {
		choices = append(choices, repair)
	}
	if rebuildOffered(rep) {
		choices = append(choices, rebuildChoice)
	}
	choices = append(choices, remove)
	pick, err := zenity.List("Backup health — "+rep.Posture+"\n\n"+text, choices, zenity.Title(appTitle), zenity.DefaultItems(cont))
	if err != nil {
		return err
	}
	switch pick {
	case rebuildChoice:
		msg, rerr := rebuildSearchIndex(out)
		if rerr != nil {
			return zenity.Error(msg, zenity.Title(appTitle))
		}
		return zenity.Info(msg, zenity.Title(appTitle))
	case repair:
		return repairSchedule(out, in.Desc)
	case remove:
		return removeSchedule(out)
	default:
		return nil
	}
}

// rebuildChoice is the health view's choice that rebuilds a damaged, missing or
// lagging search index from the archive's own files; the health lines and the
// job log's refusals name it (F3).
const rebuildChoice = "Rebuild the search index"

// headlessRebuildAction is the index-repair action RunHeadless shows an owner who
// has no command line (the scheduled run's log and stderr): it names each surface
// by its click route, not "the window that opens with this program" — no owner
// text depends on which surface a no-argument launch opens (§6, SC21, MA-622).
const headlessRebuildAction = `Maintenance → Rebuild search index in MailArchive Desktop (with that folder as its archive location), or "` + rebuildChoice + `" in the wizard (mailarchive wizard)`

// rebuildOffered reports whether the posture carries an index reason the
// rebuild fixes (damaged, missing, or behind the manifest).
func rebuildOffered(rep health.Report) bool {
	for _, c := range rep.Codes {
		switch c {
		case "index_unreadable", "index_missing", "index_behind":
			return true
		}
	}
	return false
}

// rebuildSearchIndex rebuilds the archive's search index and folder pages from
// its own files, the action behind rebuildChoice, and returns the text to show:
// what was rebuilt, or the failure (err non-nil) with its cause.
func rebuildSearchIndex(out string) (string, error) {
	rr, err := app.Rebuild(out, nil)
	if err != nil {
		return "The search index could not be rebuilt:\n" + err.Error(), err
	}
	return fmt.Sprintf("The search index was rebuilt: %d message(s) indexed, %d pruned.", rr.Rebuilt, rr.Pruned), nil
}

// repairable reports whether the launch health view should offer to re-install
// the schedule. There must be a descriptor installed on THIS host; the repair
// route must be the WIZARD's (§4.6.3: a corroborated run-job — a capture is the
// Weekly backup card's and an export the command line's, so for those the health
// lines name the right surface and no Repair button is offered here, which also
// keeps the wizard from rebuilding a kind it does not own); and the schedule must
// be in a state a repair fixes — not present in the scheduler, its program moved
// or gone, or a re-point worth recording (ExeRepoint, §4.6.3).
func repairable(in health.Input) bool {
	if !in.HasDescriptor {
		return false
	}
	if in.ThisHost != "" && in.Desc.Host != "" && in.ThisHost != in.Desc.Host {
		return false
	}
	if schedule.RepairRoute(in.Desc, in.Out, in.KindEv) != schedule.RouteWizard {
		return false
	}
	return in.SchedState == schedule.NotInstalled || !in.ExeExists || !in.ExeIsThis || in.ExeRepoint
}

// installSpec is the pure run-job install Spec the wizard writes, from the
// archive, the sanitized name, the chosen program and the cadence — never from a
// descriptor. offerSchedule (a fresh install) and repairSpec (a repair) both
// build through it, so install and repair cannot drift (MA-641): same Args, same
// Exe rule, same wrapper decision.
func installSpec(out, name, jobPath string, iv schedule.Interval, at, exe string) schedule.Spec {
	spec := schedule.Spec{
		Name: name, Interval: iv, At: at, Exe: exe,
		Args: []string{"-job", jobPath}, Log: filepath.Join(out, "mailarchive.log"), Out: out,
		WrapperPath: schedule.DefaultWrapperPath(name),
	}
	// No console window when the direct run string fits; otherwise the wrapper
	// (and its brief cmd.exe console) is the price of a working job (S12). The one
	// shared decision, so every install caller agrees (MA-641).
	spec.Wrapper = schedule.WrapperNeeded(spec)
	return spec
}

// RunJobSpec is the exported run-job install Spec, so MailArchive Desktop's local-
// schedule card builds the SAME spec (and the same MA-641 wrapper decision) as the
// wizard's keep-current step — one install-path, not a fork. The caller writes the
// job file (internal/job.Write) and installs the returned Spec through its own
// installer (the Desktop's injectable seam, or schedule.Install).
func RunJobSpec(out, name, jobPath string, iv schedule.Interval, at, exe string) schedule.Spec {
	return installSpec(out, name, jobPath, iv, at, exe)
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
	spec := installSpec(out, name, jobPath, iv, at, exe)
	if err := spec.Validate(); err != nil {
		return schedule.Spec{}, err
	}
	return spec, nil
}

// RepairSpec is the run-job repair Spec a repair installs, or why it must not: a
// rebuild from the sanitized name, -out and the chosen program (never the
// descriptor's Job/Log/Exe), refused when the recorded entry name is not shown to
// be this archive's (G8-AO1, AI1). Exported so the dashboard's Weekly backup card
// rebuilds a corroborated run-job through the SAME pure path the wizard uses
// (§4.6.3, MA-617) — one home, so a repair cannot diverge by surface.
func RepairSpec(out string, d schedule.Descriptor, exe string) (schedule.Spec, error) {
	if err := schedule.NameBound(out, d); err != nil {
		return schedule.Spec{}, fmt.Errorf("not repairing: %v", err)
	}
	return repairSpec(out, d, exe)
}

// repairSchedule re-installs a recorded schedule from the current executable.
func repairSchedule(out string, d schedule.Descriptor) error {
	// §4.6.1 (MA-615): resolve the program through ProgramForSchedule, keeping the
	// recorded wrapper decision; a translocated program is refused before install.
	exe, progReason, err := schedule.ProgramForSchedule(d.Wrapper != "")
	if err != nil {
		return err
	}
	// §4.6.3: every repair asks the volatile-directory question first, as every
	// install does; Cancel installs nothing.
	if proceed, qerr := volatileDirCheck(exe); qerr != nil {
		return qerr
	} else if !proceed {
		return nil
	}
	spec, err := RepairSpec(out, d, exe)
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
	msg := fmt.Sprintf("Repaired the scheduled backup %q for\n%s\n\nIt now runs this copy of the program.", spec.Name, out)
	if pn := schedule.ProgramNote(exe, progReason); pn != "" {
		msg += "\n\n" + pn
	}
	msg += "\n\nOpen the MailArchive wizard (mailarchive wizard) any time to check the backup's health.\n\nContinue to run an export now?"
	q := zenity.Question(
		msg,
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

// removeConfirmed reads the answer to the "remove this backup?" confirm (MA-652):
// OK (nil) proceeds with the removal, Cancel or a dismissed dialog (ErrCanceled)
// keeps the backup, any other error propagates. Pure so the branch is tested
// without a display, mirroring repairFollowUp.
func removeConfirmed(choice error) (proceed bool, err error) {
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
	// The descriptor is untrusted: its name drives the removal only when it is
	// this archive's entry (a handed-over folder can record any entry's name).
	recorded, err := schedule.RecordedBackupName(out)
	if err != nil {
		return err
	}
	name, err := schedule.SanitizeName(recorded)
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
	// Confirm before the destructive removal (MA-652): the health view's "Remove the
	// scheduled backup" pick lands here directly, so without this a mistaken pick
	// would delete the backup with no way back. The confirm sits AFTER the MA-455
	// name-binding above, so only a real, bound entry is ever offered; on Keep the
	// scheduler, the descriptor and the job file are all left as they were, and the
	// archive folder and its files are never touched on either answer.
	switch proceed, cerr := removeConfirmed(zenity.Question(
		fmt.Sprintf("Remove the scheduled backup %q for\n%s?\n\nThis only stops the automatic backups — your archive and everything in it is kept.", name, out),
		zenity.Title(appTitle),
		zenity.OKLabel("Remove the backup"),
		zenity.CancelLabel("Keep it"),
	)); {
	case cerr != nil:
		return cerr
	case !proceed:
		return nil
	}
	if err := schedule.Remove(spec); err != nil {
		return err
	}
	if jp, err := job.PathFor(name); err == nil {
		_ = os.Remove(jp)
	}
	return zenity.Info(fmt.Sprintf("Removed the scheduled backup %q for\n%s", name, out), zenity.Title(appTitle))
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
	var outlookSkips []outlookcom.Skip
	var outlookExported []string // the PSTs this run's export wrote: a fresh image of the mailbox (NF1)
	// Outlook-automation path: have Outlook write a fresh PST per account, then
	// archive those. Reliable for Exchange/IMAP .ost caches go-pst can't read.
	if c.outlookCOM {
		_ = dlg.Text("Running Send/Receive, then exporting each account to a PST…\nThis can take several minutes for large mailboxes.")
		stores, skips, cerr := outlookcom.CreatePSTs(filepath.Join(c.out, "_outlook-pst"),
			outlookcom.Options{Sync: true, SyncWait: 5 * time.Minute}, logger)
		if cerr != nil {
			_ = dlg.Close()
			return cerr
		}
		inputs = nil
		for _, s := range stores {
			inputs = append(inputs, s.Path)
			outlookExported = append(outlookExported, s.Path)
		}
		outlookSkips = skips
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

		FollowLinks: c.followLinks,

		OutlookSkips:    outlookSkips,
		OutlookExported: outlookExported,
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
	// A message that is fillable only because a part is cut short, or its parts could not be told
	// apart, in the mailbox file it was read from is not one the source has not delivered: a download
	// returns the same bytes. Say what cures it, that one malformed when it was sent stays listed, and
	// once that these are also in the kept-as-received count below (friction F3, GUI half).
	onlyGaps := r.Fillable > 0 && r.FillableOnlyParseGap >= r.Fillable
	switch {
	case onlyGaps:
		fmt.Fprintf(b, "\nCut short, or with parts that could not be told apart, in the mailbox file: %d — a download does not change a mailbox file: replace it with a complete export from your mail app, then run this again to add them (an IMAP cache: download the messages for offline use instead). A message that was malformed when it was sent cannot be completed by either, and stays listed\n", r.Fillable)
		fmt.Fprintf(b, "%d of them %s also counted under \"Kept as received\" below\n", r.FillableParseGap, guiVerb(r.FillableParseGap, "is", "are"))
		if r.Unknown > 0 {
			fmt.Fprintf(b, "Not yet re-checked: %d (from an older archive; re-checked automatically each run)\n", r.Unknown)
		}
	case notDone > 0:
		line := fmt.Sprintf("\nNot fully downloaded yet: %d — open these in your mail app so it downloads them, then run this again to add them", notDone)
		if r.Unknown > 0 {
			line += fmt.Sprintf(" (%d are from an older archive and get re-checked automatically each run)", r.Unknown)
		}
		b.WriteString(line + "\n")
		if r.FillableParseGap > 0 {
			// The download is for the messages a source has not delivered; the cut ones among them
			// need the file replaced.
			fmt.Fprintf(b, "%d of them %s cut short, or %s parts that could not be told apart, in the mailbox file: a download does not change it, so replace the file with a complete export from your mail app (the report says which)\n",
				r.FillableParseGap, guiVerb(r.FillableParseGap, "is", "are"), guiVerb(r.FillableParseGap, "has", "have"))
			fmt.Fprintf(b, "%d of them %s also counted under \"Kept as received\" below\n", r.FillableParseGap, guiVerb(r.FillableParseGap, "is", "are"))
		}
	}
	if r.Terminal > 0 {
		fmt.Fprintf(b, "Empty at the source (nothing to download): %d\n", r.Terminal)
	}
	// A part this version cannot decode is kept as received: not empty at the source,
	// and not a download to wait for (design-mime-fidelity §4.9).
	if r.Undecodable > 0 {
		fmt.Fprintf(b, "Kept as received, because this version cannot decode a part of them: %d (the files are in each message's attachments zip)\n", r.Undecodable)
	}
	if r.FidelityFallback > 0 {
		fmt.Fprintf(b, "Kept as an earlier version read them, because this version could not decode them in full: %d (nothing is lost)\n", r.FidelityFallback)
	}
	if (notDone > 0 || r.Terminal > 0 || r.Undecodable > 0 || r.FidelityFallback > 0) && r.ReportPath != "" {
		fmt.Fprintf(b, "Details: %s\n", r.ReportPath)
	}
	if r.IndexErrors > 0 {
		fmt.Fprintf(b, "\nWARNING: %d message(s) could not be indexed for search.\n", r.IndexErrors)
	}
	if keepRaw {
		b.WriteString("\nEach message is saved twice: an .html page to read, and an .eml file to import back into a mail program if you ever need to.\n")
	}
	b.WriteString("\nYour data stays on this computer: the archive is read from your own mail files, nothing is uploaded anywhere, and the saved pages run no scripts and load nothing from the internet — safe to open offline.\n")
	fmt.Fprintf(b, "\nOutput folder:\n%s\n\nOpen index.html there to browse — no extra software needed. Each folder page has a box to filter within that folder; to search the whole archive at once, use the command-line tool: mailarchive serve -out %q", out, out)
	return b.String()
}

// guiVerb picks the singular or plural form of a verb for a count.
func guiVerb(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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

const (
	presyncNo  = "No — archive what is already on disk"
	presyncYes = "Yes — sync Thunderbird first"
)

// presyncOfferable reports whether the wizard should offer the Thunderbird pre-
// sync for these inputs (§4.2.6): this platform has a pre-sync adapter (macOS or
// Windows) AND at least one input lies in a Thunderbird profile
// (thunderbird.FindProfileDir ascends to a prefs.js). Whether that profile is one
// Thunderbird lists (profiles.ini membership, PC3) is checked at run time, not
// here — matching the CLI, which defers profile validation to the run.
func presyncOfferable(inputs []string) bool {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return false
	}
	for _, in := range inputs {
		if _, ok := thunderbird.FindProfileDir(in); ok {
			return true
		}
	}
	return false
}

// presyncQuestionText is the §4.2.6 schedule-step question, built from the single
// source notice (presync.SyncNotice, MA-567) so the GUI wording cannot drift from
// the §6 paragraph, plus the running-Thunderbird defer (R31). The macOS
// Automation-permission note is appended on macOS only.
func presyncQuestionText() string {
	q := "Sync Thunderbird before each scheduled backup?\n\n" + presync.SyncNotice +
		"\n\nIf Thunderbird is already open, or you are using the computer, when the backup starts, Thunderbird is left alone — never started or closed."
	if runtime.GOOS == "darwin" {
		q += "\n\nOn a Mac, macOS asks your permission once to let Mail Archive control Thunderbird; grant it the first time the scheduled backup runs."
	}
	return q
}

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

	// §4.2.6: when a Thunderbird store is among the inputs (and this platform has
	// a pre-sync adapter), offer to sync Thunderbird before each scheduled run.
	// Default No. A Yes records job.ThunderbirdSync (profile derived + membership-
	// checked at run time, §4.3/PC3); No or cancel installs the schedule without
	// the sync. The running-Thunderbird defer leaves an in-use Thunderbird alone.
	if !c.outlookCOM && presyncOfferable(c.inputs) {
		syncPick, serr := zenity.List(presyncQuestionText(), []string{presyncNo, presyncYes},
			zenity.Title(appTitle), zenity.DefaultItems(presyncNo))
		if serr != nil && !errors.Is(serr, zenity.ErrCanceled) {
			return serr
		}
		c.tbSync = serr == nil && syncPick == presyncYes
	}

	// §4.6.1 (MA-615): the wizard's schedule runs ProgramForSchedule's program, not
	// os.Executable(); wrapped is dormant while L-W2 holds, so the wrapper fit is
	// decided from the chosen program below. A translocated program is refused.
	exe, progReason, err := schedule.ProgramForSchedule(false)
	if err != nil {
		return err
	}
	if proceed, qerr := volatileDirCheck(exe); qerr != nil {
		return qerr
	} else if !proceed {
		return nil
	}

	name := schedule.DefaultNameFor(c.out)
	jobPath, err := job.PathFor(name)
	if err != nil {
		return err
	}
	if err := job.Write(jobPath, jobFor(name, c)); err != nil {
		return err
	}

	spec := installSpec(c.out, name, jobPath, iv, at, exe)
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
	if pn := schedule.ProgramNote(exe, progReason); pn != "" {
		note += "\n\n" + pn
	}
	syncLine := ""
	if c.tbSync {
		syncLine = "\nThunderbird: synced before each run (if Thunderbird is already open, it is left alone)."
	}
	return zenity.Info(fmt.Sprintf("Scheduled: %s at %s.\n\nArchive: %s\nLog: %s%s\n\nOpen the MailArchive wizard (mailarchive wizard) any time to check the backup's health.%s",
		iv, at, c.out, spec.Log, syncLine, note), zenity.Title(appTitle))
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
		KeepRaw: c.keepRaw, FollowLinks: c.followLinks,
	}
	if c.outlookCOM {
		j.Inputs = nil
		j.OutlookSyncWait = "5m"
	}
	if c.tbSync {
		// Presence means ON; an empty profile means "derive from the inputs and
		// require profiles.ini membership at run time" (§4.2.5/§4.3, PC3).
		j.ThunderbirdSync = &job.ThunderbirdSync{}
	}
	return j
}

// volatileDirCheck shows the volatile-directory confirmation when exe lives in a
// place that gets cleaned up or replaced (§4.6.3: every repair asks first, as
// every install does). proceed is false with a nil err when the owner cancels —
// the caller installs nothing, quietly; a non-nil err is the dialog's.
func volatileDirCheck(exe string) (proceed bool, err error) {
	if !inVolatileDir(exe) {
		return true, nil
	}
	return volatileDecision(zenity.Question(
		fmt.Sprintf("This program runs from\n%s\nwhich is a place that tends to get cleaned up or replaced. If it moves, the scheduled backup silently stops.\n\nMove it somewhere permanent first (e.g. your Applications or Program Files folder) and run the wizard again — or continue anyway.", filepath.Dir(exe)),
		zenity.Title(appTitle), zenity.OKLabel("Continue anyway"), zenity.CancelLabel("Cancel")))
}

// volatileDecision maps the owner's answer to the volatile-directory question:
// OK (nil) proceeds; Cancel (ErrCanceled) stops WITHOUT installing; any other
// error propagates. Pure, so "Cancel installs nothing" is testable without a
// display (mirrors repairFollowUp).
func volatileDecision(choice error) (proceed bool, err error) {
	switch {
	case choice == nil:
		return true, nil
	case errors.Is(choice, zenity.ErrCanceled):
		return false, nil
	default:
		return false, choice
	}
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
