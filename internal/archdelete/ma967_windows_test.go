//go:build windows

package archdelete

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// readOnly reports whether Windows marks the file read-only.
func readOnly(t *testing.T, path string) bool {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := syscall.GetFileAttributes(p)
	if err != nil {
		t.Fatal(err)
	}
	return a&syscall.FILE_ATTRIBUTE_READONLY != 0
}

// writableOnCleanup clears read-only marks under dir when the test ends, so its
// temporary folder can be removed.
func writableOnCleanup(t *testing.T, dir string) {
	t.Cleanup(func() {
		filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				os.Chmod(p, 0o644)
			}
			return nil
		})
	})
}

// covers: MA-967, R36
func TestWindowsAFileHeldOpenIsReportedTheRestDeletedAndDeleteAgainFinishes(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	held := filepath.Join(dir, filepath.FromSlash(stemA+".eml"))
	f, err := os.Open(held) // Go opens without FILE_SHARE_DELETE: Windows refuses to delete it
	if err != nil {
		t.Fatal(err)
	}
	res := r.run(t, false)
	f.Close()
	if res.Status != StatusPartial || !strings.Contains(res.Lines[0], stemA+".eml") ||
		!strings.HasSuffix(res.Lines[0], "Close Outlook or any program using them, then click Delete again.") {
		t.Fatalf("%s", res.Message())
	}
	if !exists(filepath.Join(dir, manifestName)) || exists(filepath.Join(dir, filepath.FromSlash(stemA+".html"))) {
		t.Fatal("the manifest is kept and the rest deleted")
	}
	if res := r.run(t, false); res.Status != StatusComplete {
		t.Fatalf("Delete again: %s", res.Message())
	}
}

// covers: MA-967, R36
func TestWindowsReadOnlyFilesFollowTheReadOnlyRule(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	writableOnCleanup(t, dir)
	writeFile(t, filepath.Join(dir, filepath.FromSlash(unrecA)), "an older copy")
	for _, rel := range []string{stemA + ".html", "taxes.pdf", unrecA, "Mailbox/Inbox/.DS_Store"} {
		if err := os.Chmod(filepath.Join(dir, filepath.FromSlash(rel)), 0o444); err != nil {
			t.Fatal(err)
		}
	}
	r := newRig(t, dir)
	res := r.run(t, false)
	if res.Status != StatusComplete || exists(filepath.Join(dir, filepath.FromSlash(stemA+".html"))) {
		t.Fatalf("a read-only archive file is deleted: %s", res.Message())
	}
	for _, rel := range []string{"taxes.pdf", unrecA, "Mailbox/Inbox/.DS_Store"} {
		if p := filepath.Join(dir, filepath.FromSlash(rel)); !exists(p) || !readOnly(t, p) {
			t.Fatalf("%s: a read-only file the archive did not record is never touched unless ticked and listed", rel)
		}
	}

	// Ticked and listed: the read-only copy is removed.
	dir = plainBesidePersonsFiles(t)
	writableOnCleanup(t, dir)
	writeFile(t, filepath.Join(dir, filepath.FromSlash(unrecA)), "an older copy")
	if err := os.Chmod(filepath.Join(dir, filepath.FromSlash(unrecA)), 0o444); err != nil {
		t.Fatal(err)
	}
	r = newRig(t, dir)
	if res := r.run(t, true); res.Status != StatusComplete || exists(filepath.Join(dir, filepath.FromSlash(unrecA))) {
		t.Fatalf("a ticked read-only copy is removed: %s", res.Message())
	}

	// A read-only file with a second link is never changed: it is reported.
	dir = plainBesidePersonsFiles(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.html")
	writableOnCleanup(t, filepath.Dir(outside))
	writableOnCleanup(t, dir)
	recorded := filepath.Join(dir, filepath.FromSlash(stemA+".html"))
	if err := os.Remove(recorded); err != nil {
		t.Fatal(err)
	}
	writeFile(t, outside, "shared")
	if err := os.Link(outside, recorded); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	if err := os.Chmod(outside, 0o444); err != nil {
		t.Fatal(err)
	}
	r = newRig(t, dir)
	res = r.run(t, false)
	if res.Status != StatusPartial || !exists(recorded) || !readOnly(t, outside) {
		t.Fatalf("a hard-linked read-only file is not chmod-ed: %s", res.Message())
	}
	os.Chmod(outside, 0o644)
}
