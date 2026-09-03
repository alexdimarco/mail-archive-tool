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
	"log"
	"os"
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
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

const appTitle = "Mail Archive Export"

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
		return 1
	}
	return 0
}

// jobFail records a failure to even start the job, beside the job file.
func jobFail(path string, err error) {
	name := strings.TrimSuffix(filepath.Base(path), ".json")
	sink := filepath.Join(filepath.Dir(path), name+".jobfail.log")
	if f, e := os.OpenFile(sink, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); e == nil {
		fmt.Fprintf(f, "%s %v\n", time.Now().Format(time.RFC3339), err)
		f.Close()
	}
	fmt.Fprintln(os.Stderr, "mailarchive-gui: "+err.Error())
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
}

func wizard() error {
	// 0. Health of the last archive this wizard scheduled, if any (S8): the
	//    double-click user has no other way to learn the backup stopped.
	if err := healthCheck(); err != nil {
		return err
	}

	// 1. Choose the source type, then pick the file/folder accordingly.
	const srcAuto = "Auto-detect my mailboxes"
	const srcOutlook = "Outlook data file (.pst / .ost)"
	const srcOutlookCOM = "Outlook account (via Outlook app — for Exchange / .ost)"
	const srcThunderbird = "Thunderbird / mbox mail folder"
	const srcEvolution = "Evolution mail store (folder)"
	const srcMbox = "Single mbox file"
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
		// Nothing found: say so and ask again (never drop into a picker for a
		// program the user may not have).
		if err := zenity.Warning(
			"No Outlook, Thunderbird, or Evolution mailboxes were found automatically.\n\nChoose the type and pick the file or folder yourself.",
			zenity.Title(appTitle)); err != nil {
			return err
		}
	}
	useOutlookCOM := srcType == srcOutlookCOM

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

	// 6. Run with a progress dialog, then offer to keep it current.
	return runExport(choice)
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
	if _, err := schedule.ReadDescriptor(out); err != nil {
		return nil // no schedule here: nothing to report
	}
	in := health.Gather(out, "")
	rep := health.Assess(in, time.Now())
	text := strings.Join(health.Summary(in, rep), "\n")

	const cont = "Continue to the wizard"
	const remove = "Remove the scheduled backup"
	pick, err := zenity.List("Backup health — "+rep.Posture+"\n\n"+text, []string{cont, remove}, zenity.Title(appTitle), zenity.DefaultItems(cont))
	if err != nil {
		return err
	}
	if pick != remove {
		return nil
	}
	return removeSchedule(out)
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

	s := result.Stats
	summary := fmt.Sprintf(
		"Export complete.\n\n"+
			"Exported:               %d\n"+
			"Filled (content arrived): %d\n"+
			"Skipped (already done): %d\n"+
			"Skipped (date filter):  %d\n"+
			"Attachments archived:   %d\n",
		s.Exported, s.Filled, s.SkippedManifest, s.SkippedDate, s.Attachments)
	if result.Fillable > 0 || result.Terminal > 0 || result.Unknown > 0 {
		summary += fmt.Sprintf("\nStill missing content:  %d (download for offline use in your mail app, then run again — incremental fills them)\nSource-empty (never fillable): %d\nNot yet re-examined:    %d\nDetails: %s\n",
			result.Fillable, result.Terminal, result.Unknown, result.ReportPath)
	}
	if result.IndexErrors > 0 {
		summary += fmt.Sprintf("\nWARNING: %d message(s) could not be indexed for search.\n", result.IndexErrors)
	}
	summary += fmt.Sprintf("\nOutput folder:\n%s\n\nOpen index.html there to browse (no software needed); for full-text search run:\n  mailarchive serve -out \"%s\"", c.out, c.out)
	if err := zenity.Info(summary, zenity.Title(appTitle)); err != nil && !errors.Is(err, zenity.ErrCanceled) {
		return err
	}

	// 7. Keep it current? (P7)
	return offerSchedule(c)
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
	j := job.Job{
		Name: name, Inputs: c.inputs, Auto: c.auto, Out: c.out,
		Mode:  "incremental", // repeats are incremental: they also fill what was missing
		Since: c.since, CopyFirst: c.copyFirst, Outlook: c.outlookCOM,
	}
	if c.outlookCOM {
		j.Inputs = nil
		j.OutlookSyncWait = "5m"
	}
	if err := job.Write(jobPath, j); err != nil {
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
	return zenity.Info(fmt.Sprintf("Scheduled: %s at %s.\n\nArchive: %s\nLog: %s\n\nStarting this wizard shows the backup's health first; the command line has `mailarchive status -out \"%s\"`.%s",
		iv, at, c.out, spec.Log, c.out, note), zenity.Title(appTitle))
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
