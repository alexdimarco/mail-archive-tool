package main

import (
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1045, R36
// The one program installs the engine's unfinished-delete check (archdelete's, when it loads): the
// engine lets every backup through when none is installed, so a program that stopped linking
// archdelete would back up into a folder whose delete did not finish (the G2 settle pass's LOW-4).
// The Desktop's and the wizard's own programs prove the same in their packages.
func TestTheProgramInstallsTheUnfinishedDeleteCheck(t *testing.T) {
	assure.Reached(t, app.UnfinishedDeleteCheckInstalled(), "mailarchive installs the engine's unfinished-delete check")
}
