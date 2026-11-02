package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// statusOf runs `mailarchive status -out dir` in this test's home, returning what it printed and
// its exit code.
func statusOf(t *testing.T, dir string) (string, int) {
	t.Helper()
	cmd := exec.Command(testBin, "status", "-out", dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	ee, isExit := err.(*exec.ExitError)
	if !isExit {
		t.Fatalf("status did not run: %v", err)
	}
	return string(out), ee.ExitCode()
}

// covers: MA-1005, R36
// `status` never mentions a deleted archive (docs/design-archive-delete.md §10): after a complete
// delete, `status -out` of its folder answers that there is no archive there — not a report on
// the archive, and no word of a deletion — and `status -out` of the archive kept beside it names
// the deleted one nowhere. Positive twin first: before the delete, status reports on the archive.
func TestStatusNeverMentionsADeletedArchive(t *testing.T) {
	e := newCLIDel(t)
	gone := e.plainArchive("Retired mail 2019")
	kept := e.plainArchive("Kept mail")
	before, code := statusOf(t, gone)
	if code != 0 || strings.Contains(before, "no archive at") {
		t.Fatalf("positive twin: before the delete status reports on the archive:\n%s", before)
	}

	a := &scripted{isAttended: true, answers: []string{"Retired mail 2019"}}
	var res bytes.Buffer
	if err := deleteArchive(a, gone, "", "", &res); err != nil || !strings.Contains(res.String(), "Status: complete") {
		t.Fatalf("delete-archive: %v\n%s\n%s", err, a.transcript(), res.String())
	}

	// The words, not the test's own temp folders (whose names carry this test's name).
	words := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(s, filepath.Dir(filepath.Dir(gone)), "TEMP"))
	}
	after, code := statusOf(t, gone)
	assure.Refused(t, code, after, assure.Code(1),
		assure.Names("no archive at "+gone),
		assure.NoSideEffect(func() bool { return !strings.Contains(words(after), "delet") }))
	other, code := statusOf(t, kept)
	if code != 0 {
		t.Fatalf("status of the archive kept beside it: %s", other)
	}
	for _, never := range []string{gone, filepath.Base(gone)} {
		if strings.Contains(other, never) {
			t.Errorf("status of another archive names the deleted one (%q):\n%s", never, other)
		}
	}
	if strings.Contains(words(other), "delet") {
		t.Errorf("status of another archive speaks of a deletion:\n%s", other)
	}
}
