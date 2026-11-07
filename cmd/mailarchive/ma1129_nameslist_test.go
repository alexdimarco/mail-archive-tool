package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1129, MA-1124, R40
// The changed names at the terminal (the lead's call of 2026-11-07): `-plan` prints the
// plan's list of changed names, "old name → new name", and `-record-out` keeps the same
// lines; with -name "Mail%20backup" the top NAME is changed and listed. Positive twin
// first: the archive's own name needs no change, so no list is printed.
func TestThePlanAtTheTerminalListsTheChangedNames(t *testing.T) {
	e := newTBCLI(t, "")
	code, _, stderr := e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile, "-plan")
	assure.Reached(t, code == 0 && !strings.Contains(stderr, "had to be changed") && !strings.Contains(stderr, "→"),
		fmt.Sprintf("positive twin: nothing changed, no list: %d %q", code, stderr))

	rec := filepath.Join(t.TempDir(), "plan-record.txt")
	before := e.state()
	code, _, stderr = e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile, "-name", "Mail%20backup", "-plan", "-record-out", rec)
	line := `"Restored — Mail%20backup" → "Restored — Mail_20backup"`
	assure.Reached(t, code == 0 && strings.Contains(stderr, "1 folder name had to be changed to be safe as file names; Thunderbird shows the changed names:\n"+line+"\n"),
		fmt.Sprintf("-plan lists the changed name: %d %q", code, stderr))
	saved, err := os.ReadFile(rec)
	assure.Reached(t, err == nil && strings.Contains(string(saved), "changed names:\n"+line+"\n"), fmt.Sprintf("-record-out keeps the list: %v %q", err, saved))
	assure.Reached(t, e.state() == before, "-plan writes nothing into the profile")
}
