package archdelete

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/nowindow"
)

// symlinkOrSkip makes link a symbolic link to target. On Windows, where a symbolic link needs
// a privilege most accounts lack (elevation or developer mode), a link to a folder is made a
// junction instead, which needs none — the link Windows users meet most — so these tests run
// there (MA-959, slice 5's carry-over). A link to a file without the privilege still skips:
// a junction cannot point at a file.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			abs := target
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(filepath.Dir(link), target)
			}
			if fi, serr := os.Stat(abs); serr == nil && fi.IsDir() {
				junctionOrSkip(t, abs, link)
				return
			}
			t.Skipf("no link privilege here, and a junction cannot point at a file (lab: run elevated or in developer mode): %v", err)
		}
		t.Fatal(err)
	}
}

// junctionOrSkip makes link a Windows junction to the folder target with cmd's mklink /J (no
// privilege needed), or skips where it cannot (lab).
func junctionOrSkip(t *testing.T, target, link string) {
	t.Helper()
	out, err := nowindow.Hide(exec.Command("cmd", "/c", "mklink", "/J", link, target)).CombinedOutput()
	if err != nil {
		t.Skipf("could not make a junction (lab): %v %s", err, out)
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
