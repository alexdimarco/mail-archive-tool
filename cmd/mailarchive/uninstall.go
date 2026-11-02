package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/uninstall"
)

// runUninstall removes MailArchive — its schedules, settings and sign-in — while keeping
// every archive (your mail is never deleted). It ALWAYS prints what it will remove and the
// archives it will keep first; `-dry-run` stops there, and a real run requires `-yes`.
func runUninstall(args []string) error {
	fs := flag.NewFlagSet("mailarchive uninstall", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "show exactly what would be removed, and stop")
	yes := fs.Bool("yes", false, "proceed with removal (required for a real, non-dry run)")
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
	rep, err := uninstall.Uninstall(configDir, known, exe, true)
	if err != nil {
		return err
	}
	printUninstallPlan(os.Stdout, rep, *yes && !*dry)
	if *dry {
		return nil
	}
	if !*yes {
		return errors.New("this removes MailArchive's schedules, settings and sign-in — your archives are kept. Re-run with -yes to proceed, or -dry-run to preview")
	}
	done, err := uninstall.Uninstall(configDir, known, exe, false)
	if err != nil {
		return err
	}
	printUninstallDone(os.Stdout, done)
	return nil
}

// deletionsLogRemovingNote is said under the deletion log in place of DeletionsLogNote when this
// run removes it (-yes): there is no "first" left to copy it in (del-s5 re-verify residual 4).
const deletionsLogRemovingNote = "This run removes it. A copy had to be made before running uninstall -yes (uninstall -dry-run shows this list and removes nothing)."

// printUninstallDone is what a finished uninstall says.
func printUninstallDone(w io.Writer, done *uninstall.Report) {
	fmt.Fprintf(w, "\nDone — MailArchive's schedules, settings and sign-in are removed; your archives are untouched.\n")
	if done.DeletionsLog != "" {
		fmt.Fprintf(w, "The deletion log was removed: %s\n", done.DeletionsLog)
	}
	fmt.Fprintf(w, "%s\n", done.BinaryNote)
}

// printUninstallPlan prints what uninstall removes and keeps; removing says this run removes it
// (-yes), so the deletion log's note says it is being removed rather than to copy it first.
func printUninstallPlan(w io.Writer, rep *uninstall.Report, removing bool) {
	fmt.Fprintln(w, "MailArchive uninstall will REMOVE:")
	fmt.Fprintf(w, "  - scheduled tasks: %d\n", len(rep.Schedules))
	for _, s := range rep.Schedules {
		fmt.Fprintf(w, "      %s\n", s)
	}
	// List every settings/log file BY PATH (not just a count), so the owner sees exactly
	// what will be deleted before confirming (U-C7) — and any unexpected file is visible.
	fmt.Fprintf(w, "  - settings/log files: %d\n", len(rep.ConfigFiles))
	for _, f := range rep.ConfigFiles {
		fmt.Fprintf(w, "      %s\n", f)
	}
	// The delete's journal, named on its own with what to do first (design-archive-delete §8.1).
	if rep.DeletionsLog != "" {
		note := uninstall.DeletionsLogNote
		if removing {
			note = deletionsLogRemovingNote
		}
		fmt.Fprintf(w, "  - the deletion log: %s\n      %s\n", rep.DeletionsLog, note)
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
	fmt.Fprintf(w, "It will KEEP your %d archive(s) — your mail is never deleted:\n", len(rep.KeptArchives))
	for _, a := range rep.KeptArchives {
		fmt.Fprintf(w, "      %s\n", a)
	}
}
