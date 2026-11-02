package archdelete

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/nowindow"
	"mail-archive-tool/internal/util"
)

// symlinkOrSkip makes link a symbolic link to target. On Windows a link to a folder is always
// a junction (makeJunction), whatever the account may do: a junction is the link Windows users
// meet most, and it needs no privilege, so these tests run there through a junction even on an
// account that holds the link privilege (the lab runs as SYSTEM; developer mode; elevation),
// where os.Symlink would succeed and the junction path would go untested (MA-959, slice 5's
// carry-over; del-s5-A1-02 (a)). A link to a file on Windows is a symbolic link, and skips
// without the privilege: a junction cannot point at a file (its junction variants run
// instead).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		abs := target
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(filepath.Dir(link), target)
		}
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			makeJunction(t, abs, link)
			return
		}
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("no link privilege here, and a junction cannot point at a file (lab: run elevated or in developer mode): %v", err)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// makeJunction makes link a Windows junction to the folder target with cmd's mklink /J. It
// fails the test, never skips, when it cannot (del-s5-A1-02 (b)): a junction needs no
// privilege, so a failure here (a FAT or exFAT temp volume, cmd blocked) would otherwise
// leave a test the catalog calls proven on Windows silently unrun. It also checks that what
// it made is a link as util.IsLinkLike sees one, so the test cannot pass on a plain folder.
func makeJunction(t *testing.T, target, link string) {
	t.Helper()
	out, err := nowindow.Hide(exec.Command("cmd", "/c", "mklink", "/J", link, target)).CombinedOutput()
	if err != nil {
		t.Fatalf("could not make a junction %s -> %s, which needs no privilege (is the temp folder on NTFS?): %v %s", link, target, err, out)
	}
	if isLink, err := util.IsLinkLike(link); err != nil || !isLink {
		t.Fatalf("mklink /J made %s, but it is not a link to util.IsLinkLike (%v, %v)", link, isLink, err)
	}
}

// linkArchive is an archive whose record's folder is Mailbox/Inbox; real makes it
// a real folder, otherwise a link to the person's own folder Docs in the same root
// (which os.Root would follow, as it stays inside the root).
func linkArchive(t *testing.T, real bool) string {
	t.Helper()
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: "Mailbox/Inbox/2024-03-01_0900_x_ab12cd34.html"}})
	writeFile(t, filepath.Join(dir, "Docs", "2024-03-01_0900_x_ab12cd34.html"), "the person's page")
	writeFile(t, filepath.Join(dir, "Docs", "budget.xlsx"), "the person's sheet")
	if real {
		writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", "2024-03-01_0900_x_ab12cd34.html"), "page")
		return dir
	}
	if err := os.MkdirAll(filepath.Join(dir, "Mailbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, filepath.Join("..", "Docs"), filepath.Join(dir, "Mailbox", "Inbox"))
	return dir
}

// covers: MA-959, R36
func TestALinkMidPathInsideTheArchiveRefusesTheDelete(t *testing.T) {
	// Positive twin first: a real folder is planned.
	mustPlan(t, linkArchive(t, true), testDeps())

	dir := linkArchive(t, false)
	before := tree(t, dir)
	rc, msg, c := refusalOf(t, dir, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1),
		assure.Names("is, or holds where its files are, a link", filepath.Join(dir, "Mailbox", "Inbox"), "Delete it by hand."),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) }))
	if c != CaseLink {
		t.Fatalf("case = %s", c)
	}
}

// covers: MA-959, R36
func TestTheArchiveFolderBeingALinkRefuses(t *testing.T) {
	real := linkArchive(t, true)
	link := filepath.Join(t.TempDir(), "via-link")
	symlinkOrSkip(t, real, link)
	rc, msg, _ := refusalOf(t, link, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", link))
}

// covers: MA-959, R36
func TestALinkAsAStoreFolderOrANameThePlanListsRefuses(t *testing.T) {
	for _, tc := range []struct{ name, link string }{
		{"store folder", "Mailbox"},
		{"a leftover's name", "Mailbox/Inbox/.mailarchive-5.tmp"},
		{"a root state file", ".mailarchive-lastrun.json"},
		{"a fixed root name", "search.db"},
		{"the manifest", manifestName},
	} {
		dir := t.TempDir()
		writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, nil)
		if err := os.MkdirAll(filepath.Join(dir, "Mailbox", "Inbox"), 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "elsewhere")
		writeFile(t, filepath.Join(target, "keep.txt"), "not the archive's")
		at := filepath.Join(dir, filepath.FromSlash(tc.link))
		os.RemoveAll(at)
		symlinkOrSkip(t, target, at)
		rc, msg, _ := refusalOf(t, dir, testDeps())
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link", at))
	}
}

// covers: MA-959, R36
func TestALinkAmongFilesTheDeleteDoesNotTouchIsKept(t *testing.T) {
	dir := linkArchive(t, true)
	symlinkOrSkip(t, filepath.Join(t.TempDir()), filepath.Join(dir, "Mailbox", "Inbox", "my-shortcut"))
	symlinkOrSkip(t, filepath.Join(t.TempDir()), filepath.Join(dir, "my-root-shortcut"))
	p := mustPlan(t, dir, testDeps())
	for _, k := range []string{"Mailbox/Inbox/my-shortcut", "my-root-shortcut"} {
		if !contains(p.Kept, k) {
			t.Fatalf("%s must be kept, kept = %q", k, p.Kept)
		}
	}
}
