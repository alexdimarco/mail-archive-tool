package main

import "mail-archive-tool/internal/desktop"

// runQuit is the `quit` verb (design-menubar, the S1 fix round 2, B1, B2): it asks this
// account's running MailArchive to quit, over the signed hand-off, and never stops work.
// desktop.Quit prints its own words; its exit status is the process's, one code per
// outcome (the upgrade script reads it), so main prints nothing more. It never exits 1,
// which is what an older program without this verb exits with ("-out is required").
func runQuit(args []string) error {
	if code := desktop.Quit(args); code != 0 {
		return &exitError{code}
	}
	return nil
}
