// Command mailarchive-gui is a small native-dialog wizard around the exporter.
// Double-clicking it walks the user through picking a data file, an output
// folder, a mode and an optional date window, then shows a progress dialog and
// a summary — all via native OS dialogs (pure-Go, no browser, no console).
//
// Build for Windows without a console window:
//
//	GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o mailarchive-gui.exe ./cmd/mailarchive-gui
package main

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

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/util"
)

const appTitle = "Mail Archive Export"

func main() {
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

func wizard() error {
	// 1. Choose the source type, then pick the file/folder accordingly.
	const srcAuto = "Auto-detect my mailboxes"
	const srcOutlook = "Outlook data file (.pst / .ost)"
	const srcThunderbird = "Thunderbird / mbox mail folder"
	const srcMbox = "Single mbox file"
	srcType, err := zenity.List(
		"What are you exporting?",
		[]string{srcAuto, srcOutlook, srcThunderbird, srcMbox},
		zenity.Title(appTitle),
		zenity.DefaultItems(srcAuto),
	)
	if err != nil {
		return err
	}

	// Auto-detect discovers Outlook (Windows), Thunderbird, and Evolution stores.
	// If it finds any, let the user pick one or all; otherwise fall through to the
	// manual pickers below.
	var autoInputs []string
	if srcType == srcAuto {
		autoInputs, err = autoDetectSources()
		if err != nil {
			return err
		}
		if len(autoInputs) == 0 {
			if err := zenity.Warning(
				"No Outlook, Thunderbird, or Evolution mailboxes were found automatically.\n\nPick the file or folder manually instead.",
				zenity.Title(appTitle),
			); err != nil {
				return err
			}
			srcType = srcOutlook // fall through to the manual Outlook picker
		}
	}

	var inputPath string
	switch {
	case srcType == srcAuto && len(autoInputs) > 0:
		// handled after the switch (inputs already discovered)
	case srcType == srcThunderbird:
		inputPath, err = zenity.SelectFile(
			zenity.Title("Select the Thunderbird mail folder (e.g. …/ImapMail/<account>)"),
			zenity.Directory(),
		)
	case srcType == srcMbox:
		inputPath, err = zenity.SelectFile(
			zenity.Title("Select an mbox file"),
		)
	default:
		inputPath, err = zenity.SelectFile(
			zenity.Title("Select an Outlook data file (.pst or .ost)"),
			zenity.FileFilters{{
				Name:     "Outlook data files",
				Patterns: []string{"*.pst", "*.ost"},
				CaseFold: true,
			}},
		)
	}
	if err != nil {
		return err
	}

	// Assemble the input list: the auto-detected selection, or the single manual
	// pick. Auto-detect can return several stores, so let the user choose.
	var inputs []string
	if srcType == srcAuto && len(autoInputs) > 0 {
		inputs, err = pickAutoInputs(autoInputs)
		if err != nil {
			return err
		}
	} else {
		inputs = []string{inputPath}
	}

	// 1a. IMAP prep: for a Thunderbird IMAP account, offer to enable offline and
	//     guide a full sync; for an Outlook .ost, show the equivalent guidance.
	prepared := false
	switch srcType {
	case srcThunderbird:
		prepared, err = prepareThunderbirdGUI(inputPath)
		if err != nil {
			return err
		}
	case srcOutlook:
		if strings.EqualFold(filepath.Ext(inputPath), ".ost") {
			if err := outlookOfflineNote(); err != nil {
				return err
			}
		}
	}

	// 2. Choose the output folder.
	outDir, err := zenity.SelectFile(
		zenity.Title("Select the output folder"),
		zenity.Directory(),
	)
	if err != nil {
		return err
	}

	// 3. Choose the export mode.
	const modeInc = "Incremental — only messages new since the last run"
	const modeFull = "Full — export everything"
	modeChoice, err := zenity.List(
		"Export mode:",
		[]string{modeInc, modeFull},
		zenity.Title(appTitle),
		zenity.DefaultItems(modeInc),
	)
	if err != nil {
		return err
	}
	mode := export.Incremental
	if modeChoice == modeFull {
		mode = export.Full
	}
	if prepared {
		mode = export.Full // re-export so freshly-downloaded content is written
	}

	// 4. Optional date window (re-prompt until valid or cancelled).
	since, err := askSince()
	if err != nil {
		return err
	}

	// 5. Is the mail app open? If so, snapshot the file first to avoid a lock.
	//    (Ignored for a mail folder, which is read in place.)
	openChoice, err := zenity.List(
		"Is your mail app (Outlook / Thunderbird) currently open?",
		[]string{"No", "Yes — copy the file first to avoid a lock"},
		zenity.Title(appTitle),
		zenity.DefaultItems("No"),
	)
	if err != nil {
		return err
	}
	copyFirst := strings.HasPrefix(openChoice, "Yes")

	// 6. Run with a progress dialog.
	return runExport(inputs, outDir, mode, since, copyFirst)
}

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
		zenity.CancelLabel("No, export cached"),
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
		"Outlook (.ost) accounts cache mail on demand.\n\nIf Cached Exchange Mode uses a limited window (\"Mail to keep offline\"), older mail may be header-only and won't fully export.\n\nTo include everything, in Outlook set:\n    Account Settings → Change → Mail to keep offline → All\nthen Send / Receive → Update Folder, and export again.\n\nClick OK to continue with what's currently cached.",
		zenity.Title(appTitle),
	)
	if errors.Is(e, zenity.ErrCanceled) {
		return nil
	}
	return e
}

// askSince prompts for a date window, re-prompting on invalid input. An empty
// value means "export everything" and returns the zero time.
func askSince() (time.Time, error) {
	for {
		val, err := zenity.Entry(
			"Only export items newer than\n(e.g. 30d, 4w, 720h, or a date like 2026-07-01).\n\nLeave blank to export everything.",
			zenity.Title("Date window"),
			zenity.EntryText(""),
		)
		if err != nil {
			return time.Time{}, err
		}
		val = strings.TrimSpace(val)
		if val == "" {
			return time.Time{}, nil
		}
		since, perr := util.ParseSince(val, time.Now())
		if perr == nil {
			return since, nil
		}
		if e := zenity.Error(
			fmt.Sprintf("Couldn't understand %q.\nTry 30d, 4w, 720h, or a date like 2026-07-01.", val),
			zenity.Title("Invalid date window"),
		); e != nil {
			return time.Time{}, e
		}
	}
}

// autoDetectSources discovers mail stores the same way the CLI's -auto flag
// does (Outlook on Windows; Thunderbird and Evolution on any OS).
func autoDetectSources() ([]string, error) {
	return app.DiscoverInputs(nil, true)
}

// pickAutoInputs lets the user choose one of the auto-detected stores, or all of
// them. The returned paths are concrete (Auto stays false when they run).
//
// The list shows a legible label per store (the .pst/.ost file name, or a mail
// folder's store name) rather than the raw path: the discovered paths are long
// and identical up to the point the dialog truncates them, so a raw list is
// unreadable and indistinguishable.
func pickAutoInputs(found []string) ([]string, error) {
	const allOfThem = "➤ All of them"
	if len(found) == 1 {
		return found, nil
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
		return nil, err
	}
	if choice == allOfThem {
		return found, nil
	}
	if p, ok := byLabel[choice]; ok {
		return []string{p}, nil
	}
	return []string{choice}, nil // fallback: treat the choice as a path
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

func runExport(inputs []string, outDir string, mode export.Mode, since time.Time, copyFirst bool) error {
	logger := newLogger(outDir)

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

	opts := app.Options{
		Inputs:    inputs,
		Out:       outDir,
		Mode:      mode,
		Since:     since,
		CopyFirst: copyFirst,
		Index:     true,
		Pages:     true,
	}

	var ticks int
	result, runErr := app.Run(ctx, opts, logger, func(s export.Stats) {
		ticks++
		if ticks%20 == 0 {
			_ = dlg.Text(fmt.Sprintf("exported %d · skipped %d · attachments %d",
				s.Exported, s.SkippedManifest+s.SkippedDate, s.Attachments))
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
		return fmt.Errorf("%v\n\nA log was written to:\n%s", runErr, filepath.Join(outDir, "mailarchive.log"))
	}

	s := result.Stats
	summary := fmt.Sprintf(
		"Export complete.\n\n"+
			"Exported:               %d\n"+
			"Skipped (already done): %d\n"+
			"Skipped (date filter):  %d\n"+
			"Attachments archived:   %d\n\n"+
			"Output folder:\n%s",
		s.Exported, s.SkippedManifest, s.SkippedDate, s.Attachments, outDir)
	return zenity.Info(summary, zenity.Title(appTitle))
}

// newLogger writes a run log into the output folder (there is no console in a
// GUI build), falling back to discarding output if the file can't be created.
func newLogger(outDir string) *log.Logger {
	if err := os.MkdirAll(outDir, 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join(outDir, "mailarchive.log"),
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			return log.New(f, "", log.LstdFlags)
		}
	}
	return log.New(io.Discard, "", 0)
}
