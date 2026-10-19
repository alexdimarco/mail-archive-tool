// Command mailarchive-gui is the native-dialog wizard around the exporter. Its
// logic now lives in internal/wizard, shared in-process with the one program's
// `wizard` and `run-job` verbs (R33, docs/scenario-catalog.md) — this file is a
// thin entry point that preserves the historical binary and its CLI:
//
//	mailarchive-gui            walk the wizard with native OS dialogs
//	mailarchive-gui -job FILE  run that saved job headlessly (what the schedule runs)
//
// Build for Windows without a console window:
//
//	GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o mailarchive-gui.exe ./cmd/mailarchive-gui
package main

import (
	"flag"
	"os"

	"mail-archive-tool/internal/wizard"
)

func main() {
	jobPath := flag.String("job", "", "run this saved job headlessly (no dialogs); what the installed schedule uses")
	flag.Parse()
	if *jobPath != "" {
		os.Exit(wizard.RunHeadless(*jobPath))
	}
	os.Exit(wizard.Run())
}
