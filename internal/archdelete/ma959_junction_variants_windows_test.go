//go:build windows

package archdelete

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// Junction variants of MA-959's tests whose link points at a file, which without the link
// privilege skip on Windows (a junction cannot point at a file): here each link is a junction
// to a folder, made with mklink /J, which needs no privilege (slice 5's carry-over from slice
// 1a's re-verification). makeJunction fails, never skips, when it cannot make one
// (del-s5-A1-02 (b)). Lab: the Windows VM.

// covers: MA-959, R36
// A junction only shaped like mail or an index page, among files Delete does not touch, is not
// followed and is kept; a junction named as a run leftover refuses (the positive twin of the
// refusal: Delete would remove that name).
func TestAJunctionShapedLikeMailOrAnIndexPageIsKeptOnWindows(t *testing.T) {
	dir := linkArchive(t, true)
	target := t.TempDir()
	writeFile(t, filepath.Join(target, "page.html"), "the person's page")
	if err := os.MkdirAll(filepath.Join(dir, "Mailbox", "Old"), 0o755); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, target, filepath.Join(dir, "Mailbox", "Old", "2019-01-01_0000_x_99999999.html"))
	makeJunction(t, target, filepath.Join(dir, "Mailbox", "Old", "index.html"))
	p := mustPlan(t, dir, testDeps())
	for _, k := range []string{"Mailbox/Old/2019-01-01_0000_x_99999999.html", "Mailbox/Old/index.html"} {
		if !contains(p.Kept, k) || contains(unrecordedRels(p), k) {
			t.Fatalf("%s must be kept, not listed: kept %q unrecorded %q", k, p.Kept, unrecordedRels(p))
		}
	}
	leftover := filepath.Join(dir, "Mailbox", "Old", ".mailarchive-9.tmp")
	makeJunction(t, target, leftover)
	rc, msg, _ := refusalOf(t, dir, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", leftover),
		assure.NoSideEffect(func() bool { _, err := os.Stat(filepath.Join(target, "page.html")); return err == nil }))
}

// covers: MA-959, R36
// A lock file that is a junction refuses at the plan, in a plain and an encrypted archive
// (step 7 removes .mailarchive.lock by name and step 1 takes the lock on it).
func TestALockFileThatIsAJunctionRefusesOnWindows(t *testing.T) {
	for _, kind := range []string{"plain", "encrypted"} {
		var dir string
		if kind == "plain" {
			dir = smallArchive(t, t.TempDir())
		} else {
			dir = encryptedArchive(t)
			os.Remove(filepath.Join(dir, lockName))
		}
		// Positive twin first: a regular lock file never refuses.
		writeFile(t, filepath.Join(dir, lockName), "")
		mustPlan(t, dir, testDeps())
		os.Remove(filepath.Join(dir, lockName))
		target := t.TempDir()
		lock := filepath.Join(dir, lockName)
		makeJunction(t, target, lock)
		rc, msg, c := refusalOf(t, dir, testDeps())
		if c != CaseLink {
			t.Fatalf("%s: case = %q (%s)", kind, c, msg)
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", lock))
	}
}
