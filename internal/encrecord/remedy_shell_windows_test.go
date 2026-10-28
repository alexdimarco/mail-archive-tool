package encrecord

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-854, R37
// On Windows, the record's remedy run in the Command Prompt it names ends the
// refusal even when the record's folder name holds $ and ` (the P3-8 fixes' pass,
// WINACL-3), where the same line typed into PowerShell names another path and fails.
// Giving a file to another account needs an elevated account or SYSTEM; unelevated
// it is said and the test stops there. Positive twin: the record this account wrote
// loads.
func TestTheRecordsRemedyWorksInTheCommandPromptWithDollarSigns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rec$ords `x")
	t.Cleanup(SetDirForTest(dir))
	add(t, idA, t.TempDir())
	p := filepath.Join(dir, FileName)
	if _, err := Load(); err != nil {
		t.Fatalf("positive twin: a record this account wrote: %v", err)
	}
	if b, err := exec.Command("icacls", p, "/setowner", "*S-1-5-32-545").CombinedOutput(); err != nil {
		t.Skipf("this account cannot give a file to Users (it is not elevated or SYSTEM): %v: %s", err, b)
	}
	_, err := Load()
	var ue *UnusableError
	if !errors.As(err, &ue) || ue.Case != CasePermissions {
		t.Fatalf("want the permissions case, got %v", err)
	}
	var lines []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "icacls ") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 || !strings.Contains(lines[0], p) || !strings.Contains(err.Error(), "Command Prompt (not PowerShell") {
		t.Fatalf("the remedy: %q", err.Error())
	}
	// Why not PowerShell: the same line names another path there and fails.
	if _, perr := typedInForTest("powershell", lines[0]); perr == nil {
		t.Fatalf("PowerShell ran %q without an error: the folder name was not read as something else", lines[0])
	}
	for _, line := range lines {
		runAsTypedForTest(t, "cmd", line)
	}
	_, err = Load()
	assure.Reached(t, err == nil, "after the printed command, run in the Command Prompt, the record loads")
}
