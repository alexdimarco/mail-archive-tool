//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/encmark"
)

// covers: MA-1049, MA-1027, R36
// `status` keeps what this computer's deletion log knows when the delete marker's lookup fails (the
// slice-2 adversarial pass's LOW-3): the archive folder is there but cannot be searched (mode 0600,
// so the marker's lookup fails with "permission denied" while the folder itself can be looked up),
// and the log has the delete's started line: `status` still says MA-1027's "a delete of this
// archive did not finish — run mailarchive delete-archive -out DIR again, or click Delete… on its
// row in MailArchive Desktop, to finish it." Run as the real program in this package's throwaway
// home. Positive twin first: the same folder, searchable, says it.
func TestMA1049_StatusKeepsTheLogsLineWhenTheMarkerLookupFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind the superuser")
	}
	logPath := ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "Half deleted")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, stderr)
	}
	ma1044Started(t, logPath, dir)
	want := "a delete of this archive did not finish — run mailarchive delete-archive -out " + encmark.CommandPath(dir) + " again, or click Delete… on its row in MailArchive Desktop, to finish it."

	// Positive twin.
	if got, _ := statusOf(t, dir); !strings.Contains(got, want) {
		t.Fatalf("positive twin: status of a folder this computer's log says did not finish:\n%s", got)
	}

	if err := os.Chmod(dir, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if _, err := os.Lstat(filepath.Join(dir, ".mailarchive-deleting.json")); err == nil || os.IsNotExist(err) {
		t.Fatalf("precondition: the marker's lookup fails for another reason than \"not there\": %v", err)
	}
	if got, _ := statusOf(t, dir); !strings.Contains(got, want) {
		t.Fatalf("a failed marker lookup hid the log's line from status:\n%s", got)
	}
}
