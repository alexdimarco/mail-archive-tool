package main

import (
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1045, R36
// mailarchive-gui (the wizard, and `-job FILE`, what an older schedule runs) installs the engine's
// unfinished-delete check (archdelete's, when it loads): the engine lets every backup through when
// none is installed, so a wizard that stopped linking archdelete would back up into a folder whose
// delete did not finish (the G2 settle pass's LOW-4). This package's only test: nothing here
// imports archdelete but the program itself.
func TestTheWizardProgramInstallsTheUnfinishedDeleteCheck(t *testing.T) {
	assure.Reached(t, app.UnfinishedDeleteCheckInstalled(), "mailarchive-gui installs the engine's unfinished-delete check")
}
