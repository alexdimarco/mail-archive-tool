package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/uninstall"
)

// runUninstall removes MailArchive — its schedules, settings and sign-in — while keeping
// every archive (uninstalling never deletes your mail). It ALWAYS prints what it will remove
// and the archives it will keep first; `-dry-run` stops there, and a real run requires `-yes`.
// A real run refuses, nothing removed, while deletions.log holds any line, until -log-copied
// says it was copied (the 1.5.3 friction review's D2).
func runUninstall(args []string) error {
	fs := flag.NewFlagSet("mailarchive uninstall", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "show exactly what would be removed, and stop")
	yes := fs.Bool("yes", false, "proceed with removal (required for a real, non-dry run)")
	logCopied := fs.Bool("log-copied", false, "you have copied deletions.log somewhere you keep records, so -yes may remove it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	configDir, err := graphconfig.DefaultConfigDir()
	if err != nil {
		return err
	}
	var known []string
	if idx, rerr := archindex.Read(); rerr == nil {
		for _, e := range idx.Entries {
			known = append(known, e.Path)
		}
	}
	exe, _ := os.Executable()

	// Always preview first — the owner sees what goes and what stays before anything moves.
	// A delete not finished still gives the plan, then says uninstall will refuse until it is.
	rep, err := uninstall.Uninstall(configDir, known, exe, true)
	var pending *uninstall.DeleteNotFinishedError
	if err != nil && (rep == nil || !errors.As(err, &pending)) {
		return err
	}
	realRun := *yes && !*dry
	removing := realRun && pending == nil
	// notCopied picks the note said under the log; the refusal itself is UninstallYes's.
	var notCopied error
	if removing {
		notCopied = uninstall.CheckLogCopied(rep.DeletionsLog, *logCopied)
	}
	note := uninstall.DeletionsLogNote
	switch {
	case !removing:
		// A plan only: once the log is copied, -yes needs -log-copied when the log holds any line,
		// so the plan says so, not only the refusal (the 1.5.3 re-walk's N10). Not while a delete is
		// not finished: -yes is refused then whatever is copied, as the plan goes on to say (the
		// settle pass's LOW-4).
		if holds, herr := archdelete.LogHoldsLines(rep.DeletionsLog); pending == nil && rep.DeletionsLog != "" && herr == nil && holds {
			note += " " + uninstall.DeletionsLogCopyNext
		}
	case notCopied != nil:
	case *logCopied:
		note = deletionsLogCopiedNote
	default:
		note = deletionsLogEmptyNote
	}
	printUninstallPlan(os.Stdout, rep, note)
	switch {
	case pending != nil && realRun:
		return pending // a real run: "so uninstall removed nothing"
	case pending != nil:
		return errors.New(pending.Until())
	case *dry:
		return nil
	case !*yes:
		return errors.New("this removes MailArchive's schedules, settings and sign-in — your archives are kept. Re-run with -yes to proceed, or -dry-run to preview")
	}
	// The real run asks the log again itself, before anything is removed and right before the log
	// would go, so a log a delete made or filled since this plan was printed is not removed
	// without -log-copied (LogNotCopiedError, as notCopied above; adversarial finding 1).
	done, err := uninstall.UninstallYes(configDir, known, exe, *logCopied)
	if err != nil {
		return err
	}
	printUninstallDone(os.Stdout, done)
	return nil
}

// The notes said under the deletion log, in place of uninstall.DeletionsLogNote, when this run
// removes it (-yes): there is no "first" left to copy it in (del-s5 re-verify residual 4).
// -yes goes ahead only with -log-copied, or when the log holds no line (D2).
const (
	deletionsLogCopiedNote = "This run removes it: you said you have copied it (-log-copied)."
	deletionsLogEmptyNote  = "This run removes it: it lists no deletes."
)

// printUninstallDone is what a finished uninstall says.
func printUninstallDone(w io.Writer, done *uninstall.Report) {
	fmt.Fprintf(w, "\nDone — MailArchive's schedules, settings and sign-in are removed; your archives are untouched.\n")
	if done.DeletionsLog != "" {
		fmt.Fprintf(w, "The deletion log was removed: %s\n", done.DeletionsLog)
	}
	fmt.Fprintf(w, "%s\n", done.BinaryNote)
}

// printUninstallPlan prints what uninstall removes and keeps; logNote is said under the deletion
// log ("" says uninstall.DeletionsLogNote), which is listed once, on its own line, and not again
// among the settings files (the 1.5.3 friction review's B11).
func printUninstallPlan(w io.Writer, rep *uninstall.Report, logNote string) {
	fmt.Fprintln(w, "MailArchive uninstall will REMOVE:")
	fmt.Fprintf(w, "  - scheduled tasks: %d\n", len(rep.Schedules))
	for _, s := range rep.Schedules {
		fmt.Fprintf(w, "      %s\n", s)
	}
	// List every settings/log file BY PATH (not just a count), so the owner sees exactly
	// what will be deleted before confirming (U-C7) — and any unexpected file is visible.
	var files []string
	for _, f := range rep.ConfigFiles {
		if rep.DeletionsLog == "" || f != rep.DeletionsLog {
			files = append(files, f)
		}
	}
	fmt.Fprintf(w, "  - settings/log files: %d\n", len(files))
	for _, f := range files {
		fmt.Fprintf(w, "      %s\n", f)
	}
	// The delete's journal, named on its own with what to do first (design-archive-delete §8.1).
	if rep.DeletionsLog != "" {
		if logNote == "" {
			logNote = uninstall.DeletionsLogNote
		}
		fmt.Fprintf(w, "  - the deletion log: %s\n      %s\n", rep.DeletionsLog, logNote)
	}
	// Only when there is any: an encrypted archive's leftover working copies (P2a-C12).
	if len(rep.Scratch) > 0 {
		fmt.Fprintf(w, "  - leftover working copies in MailArchive's working folder: %d\n", len(rep.Scratch))
		for _, f := range rep.Scratch {
			fmt.Fprintf(w, "      %s\n", f)
		}
	}
	if rep.ProgramCache != "" {
		fmt.Fprintf(w, "  - downloaded copies of MailArchive kept for encrypted archives: %s\n", rep.ProgramCache)
	}
	if rep.ClearedSignIn {
		fmt.Fprintln(w, "  - the saved Microsoft sign-in (token and app secret)")
	}
	if len(rep.KeptArchives) == 0 {
		fmt.Fprintln(w, "It will KEEP your archives — uninstalling never deletes your mail. No archives are listed.")
		return
	}
	fmt.Fprintf(w, "It will KEEP your %d archive(s) — uninstalling never deletes your mail:\n", len(rep.KeptArchives))
	for _, a := range rep.KeptArchives {
		fmt.Fprintf(w, "      %s\n", a)
	}
}
