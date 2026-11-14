package uninstall

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
)

// covers: MA-1307, R37
// Uninstall removes the Gmail answer's temporary copy a crash left between its write and its
// rename (the B3 adversarial pass's A6), named as gmailanswer writes it (TempPattern, kept in step
// by behaviour). Positive twin first: a file of another program's beside it is not a target.
func TestUninstallRemovesTheGmailAnswersLeftoverCopy(t *testing.T) {
	dir := t.TempDir()
	left := filepath.Join(dir, strings.Replace(gmailanswer.TempPattern, "*", "123456", 1))
	writeFile(t, left, "{}")
	writeFile(t, filepath.Join(dir, "notes.tmp"), "x")
	targets, err := ConfigTargets(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tp := range targets {
		got[filepath.Base(tp)] = true
	}
	if got["notes.tmp"] {
		t.Fatalf("positive twin: another program's file is a target: %v", targets)
	}
	if !got[filepath.Base(left)] {
		t.Fatalf("uninstall leaves the Gmail answer's copy %s behind: %v", filepath.Base(left), targets)
	}
}
