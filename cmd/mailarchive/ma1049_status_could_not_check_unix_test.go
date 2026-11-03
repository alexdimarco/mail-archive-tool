//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1049, R36
// `status` of a folder whose delete-marker lookup fails, this computer's deletion log holding no
// line for it (the slice-2 pass's LOW-4 (c)): it said nothing of the marker (and told the person to
// run an export into the folder); it now says "MailArchive could not check this archive's folder
// for a delete that did not finish (permission denied), so nothing is backed up or changed in it —
// check that the drive or folder can be read." The real program; the folder is there but cannot be
// searched (mode 0600). Positive twin first: the same folder, searchable, says nothing of the kind.
func TestMA1049_StatusSaysItCouldNotCheckTheFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind the superuser")
	}
	ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "Cannot be searched")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, stderr)
	}
	want := "MailArchive could not check this archive's folder for a delete that did not finish (permission denied), so nothing is backed up or changed in it — check that the drive or folder can be read."

	// Positive twin.
	if got, _ := statusOf(t, dir); strings.Contains(got, "could not check") {
		t.Fatalf("positive twin:\n%s", got)
	}

	if err := os.Chmod(dir, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	got, _ := statusOf(t, dir)
	if !strings.Contains(got, want) || strings.Contains(got, "run an export into it first") {
		t.Fatalf("status of a folder whose marker lookup fails, no log line:\n%s", got)
	}
}
