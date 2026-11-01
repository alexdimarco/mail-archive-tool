package archdelete

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-959, R36
// A link that is only shaped like mail or an index page, among files Delete does not
// touch, is not followed and is kept: it is never removed, not even with §4.3's box
// ticked, so it does not block the delete (del-s1a-A1-09). A link named as a run
// leftover still refuses (positive twin of the refusal: Delete would remove it).
func TestALinkShapedLikeMailOrAnIndexPageIsKept(t *testing.T) {
	dir := linkArchive(t, true)
	target := filepath.Join(t.TempDir(), "elsewhere.html")
	writeFile(t, target, "the person's page")
	if err := os.MkdirAll(filepath.Join(dir, "Mailbox", "Old"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, target, filepath.Join(dir, "Mailbox", "Old", "2019-01-01_0000_x_99999999.html"))
	symlinkOrSkip(t, target, filepath.Join(dir, "Mailbox", "Old", "index.html"))
	p := mustPlan(t, dir, testDeps())
	for _, k := range []string{"Mailbox/Old/2019-01-01_0000_x_99999999.html", "Mailbox/Old/index.html"} {
		if !contains(p.Kept, k) || contains(unrecordedRels(p), k) {
			t.Fatalf("%s must be kept, not listed: kept %q unrecorded %q", k, p.Kept, unrecordedRels(p))
		}
	}
	leftover := filepath.Join(dir, "Mailbox", "Old", ".mailarchive-9.tmp")
	symlinkOrSkip(t, target, leftover)
	rc, msg, _ := refusalOf(t, dir, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", leftover))
}

// covers: MA-959, R36
// Step 7 removes .mailarchive.lock by name and step 1 takes the lock on it: a lock
// file that is a link refuses at the plan, in a plain and an encrypted archive
// (del-s1a-A1-11).
func TestALockFileThatIsALinkRefuses(t *testing.T) {
	for _, kind := range []string{"plain", "encrypted"} {
		var dir string
		if kind == "plain" {
			dir = smallArchive(t, t.TempDir())
		} else {
			dir = encryptedArchive(t)
			os.Remove(filepath.Join(dir, lockName))
		}
		// Positive twin first: a regular lock file is never listed and never refuses.
		writeFile(t, filepath.Join(dir, lockName), "")
		mustPlan(t, dir, testDeps())
		os.Remove(filepath.Join(dir, lockName))
		target := filepath.Join(t.TempDir(), "elsewhere")
		writeFile(t, target, "not the archive's")
		lock := filepath.Join(dir, lockName)
		symlinkOrSkip(t, target, lock)
		rc, msg, c := refusalOf(t, dir, testDeps())
		if c != CaseLink {
			t.Fatalf("%s: case = %q (%s)", kind, c, msg)
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", lock))
	}
}

// covers: MA-959, R36
// The folder os.OpenRoot opens must be the one Plan checked: one swapped for a link
// to another folder between the check and the open is refused, never listed with
// the other folder's identity (del-s1a-A1-07).
func TestTheFolderSwappedForALinkBeforeItIsOpenedRefuses(t *testing.T) {
	parent := t.TempDir()
	dir := smallArchive(t, filepath.Join(parent, "Archive"))
	other := smallArchive(t, filepath.Join(parent, "Other"))
	// Positive twin first: nothing swapped.
	if p := mustPlan(t, dir, testDeps()); p.Folder != dir {
		t.Fatalf("folder = %s", p.Folder)
	}
	swapped := false
	beforeOpenRoot = func(abs string) {
		if swapped {
			return
		}
		swapped = true
		if err := os.Rename(abs, abs+"-moved"); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, other, abs)
	}
	t.Cleanup(func() { beforeOpenRoot = func(string) {} })
	_, err := Plan(dir, testDeps())
	r := AsRefusal(err)
	if r == nil || r.Case != CaseLink {
		t.Fatalf("want the link refusal, got %v", err)
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("a link", dir),
		assure.NoSideEffect(func() bool { _, err := os.Stat(filepath.Join(other, manifestName)); return err == nil }))
}

// covers: MA-959, R36
// On a case-sensitive volume of an OS whose default volumes ignore case (a
// case-sensitive APFS drive, a Windows folder with per-folder case sensitivity), a
// folder found to be real under one spelling does not spare another spelling — a
// link — its own link check (del-s1a-A1-06). Simulated with macOS's rules on a
// case-sensitive volume (Linux; on a case-insensitive one the two spellings cannot
// both exist and this is skipped).
func TestEachSpellingOfAFolderIsCheckedForLinks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "probe"), "")
	if _, err := os.Stat(filepath.Join(dir, "PROBE")); err == nil {
		t.Skip("this volume ignores case; the two spellings cannot both exist here (Linux CI, lab)")
	}
	os.Remove(filepath.Join(dir, "probe"))
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{
		{Path: "Mailbox/Sub/2024-01-01_0000_a_11111111.html"},
		{Path: "Mailbox/sub/2024-01-01_0000_b_22222222.html"},
	})
	writeFile(t, filepath.Join(dir, "Mailbox", "Sub", "2024-01-01_0000_a_11111111.html"), "a")
	writeFile(t, filepath.Join(dir, "Personal", "2024-01-01_0000_b_22222222.html"), "the person's")
	deps := testDeps()
	deps.GOOS = "darwin"
	// Positive twin first: with "sub" a real folder too, the plan goes ahead.
	writeFile(t, filepath.Join(dir, "Mailbox", "sub", "2024-01-01_0000_b_22222222.html"), "b")
	mustPlan(t, dir, deps)
	if err := os.RemoveAll(filepath.Join(dir, "Mailbox", "sub")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "Mailbox", "sub")
	symlinkOrSkip(t, filepath.Join("..", "Personal"), link)
	rc, msg, c := refusalOf(t, dir, deps)
	if c != CaseLink {
		t.Fatalf("case = %q (%s): the person's file through the link must never be listed", c, msg)
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", link))
}
