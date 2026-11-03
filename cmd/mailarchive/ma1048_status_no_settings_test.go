package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1048, R36
// The slice-2 adversarial pass's LOW-4, its last item (docs/design-archive-delete.md §14; built in
// slice 5; lead's words, owner-delegated 2026-11-02), as the real program: run with no home folder
// set, MailArchive's settings folder — which holds this computer's deletion log — cannot be found, so
// `status` of a folder holding the delete marker says that, with the remedy, and never that this
// computer's log "has no record of it"; the terminal's refusal of an action (`verify`) says the same.
// Positive twin first: with a home folder, the marker-only line.
func TestMA1048_StatusWithNoSettingsFolderSaysSo(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "Begun elsewhere")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, stderr)
	}
	ma1048Plant(t, dir)
	if got, code := statusOf(t, dir); code != 0 || !strings.Contains(got, "this computer's deletion log has no record of it") {
		t.Fatalf("positive twin:\n%s", got)
	}

	noHome := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(testBin, args...)
		cmd.Env = append(os.Environ(), "HOME=", "XDG_CONFIG_HOME=", "APPDATA=")
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		} else if err != nil {
			t.Fatalf("the program did not run: %v", err)
		}
		return string(out), 0
	}
	const said = "MailArchive could not find its settings folder, which holds this computer's deletion log ("
	got, code := noHome("status", "-out", dir)
	if code != 0 || !strings.Contains(got, "RED: a delete of this archive begun ") || !strings.Contains(got, said) ||
		!strings.Contains(got, "check that your home folder is set (HOME, or AppData on Windows), then run mailarchive delete-archive -out ") ||
		strings.Contains(got, "has no record") {
		t.Fatalf("status with no settings folder:\n%s", got)
	}
	got, code = noHome("verify", "-out", dir)
	if code != 1 || !strings.Contains(got, said) || !strings.Contains(got, "so it was not checked; nothing was changed. To finish it, check that your home folder is set") ||
		strings.Contains(got, "has no record") {
		t.Fatalf("verify with no settings folder: exit %d\n%s", code, got)
	}
}
