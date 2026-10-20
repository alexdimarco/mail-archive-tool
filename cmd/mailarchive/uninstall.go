package main

import (
	"errors"
	"flag"
	"fmt"
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
	printUninstallPlan(rep)
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
	fmt.Printf("\nDone — MailArchive's schedules, settings and sign-in are removed; your archives are untouched.\n%s\n", done.BinaryNote)
	return nil
}

func printUninstallPlan(rep *uninstall.Report) {
	fmt.Println("MailArchive uninstall will REMOVE:")
	fmt.Printf("  - scheduled tasks: %d\n", len(rep.Schedules))
	for _, s := range rep.Schedules {
		fmt.Printf("      %s\n", s)
	}
	fmt.Printf("  - settings/log files: %d\n", len(rep.ConfigFiles))
	if rep.ClearedSignIn {
		fmt.Println("  - the saved Microsoft sign-in (token and app secret)")
	}
	fmt.Printf("It will KEEP your %d archive(s) — your mail is never deleted:\n", len(rep.KeptArchives))
	for _, a := range rep.KeptArchives {
		fmt.Printf("      %s\n", a)
	}
}
