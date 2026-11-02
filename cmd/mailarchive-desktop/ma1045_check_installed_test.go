package main

import (
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1045, R36
// mailarchive-desktop installs the engine's unfinished-delete check (archdelete's, when it loads):
// the engine lets every backup through when none is installed, so a Desktop that stopped linking
// archdelete would back up into a folder whose delete did not finish (the G2 settle pass's LOW-4).
// This package's only test: nothing here imports archdelete but the program itself.
func TestTheDesktopProgramInstallsTheUnfinishedDeleteCheck(t *testing.T) {
	assure.Reached(t, app.UnfinishedDeleteCheckInstalled(), "mailarchive-desktop installs the engine's unfinished-delete check")
}
