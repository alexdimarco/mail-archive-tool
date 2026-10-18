// Command mailarchive-desktop is the in-process dashboard-and-launcher: a
// loopback-only local web control panel for capturing, browsing and scheduling a
// Microsoft 365 archive, calling the engine's internal packages directly (never a
// child binary). Its logic now lives in internal/desktop (Launch), shared
// in-process with the one program's `desktop` verb (R33). See
// docs/design-mailarchive-desktop.md.
package main

import (
	"fmt"
	"os"

	"mail-archive-tool/internal/desktop"
)

func main() {
	if err := desktop.Launch(os.Args[1:]); err != nil {
		// A refusal has already been shown (a dialog, and stderr); the windowsgui
		// binary has no console to print it to again.
		if !desktop.RefusalShown(err) {
			fmt.Fprintf(os.Stderr, "mailarchive-desktop: %v\n", err)
		}
		os.Exit(1)
	}
}
