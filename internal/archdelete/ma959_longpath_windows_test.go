//go:build windows

package archdelete

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/nowindow"
)

// longArchive is an archive whose record's folder lies past 260 characters of full
// path, as deep mail folders under OneDrive reach (the exporter bounds only the
// path relative to the archive).
func longArchive(t *testing.T) (dir, deep string) {
	t.Helper()
	dir = t.TempDir()
	rel := "Mailbox"
	for len(filepath.Join(dir, rel)) < 300 {
		rel += "/" + strings.Repeat("Ordner", 6)
	}
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: rel + "/2024-03-01_0900_x_ab12cd34.html"}})
	deep = filepath.Join(dir, filepath.FromSlash(rel))
	writeFile(t, filepath.Join(deep, "2024-03-01_0900_x_ab12cd34.html"), "page")
	writeFile(t, filepath.Join(deep, "desktop.ini"), "litter")
	return dir, deep
}

// covers: MA-959, R36
// Windows: every folder is checked for a link by its reparse tag through raw Win32
// calls (util.IsLinkLike), which Go gives no long-path prefix. Past 260 characters
// the check still reads the tag — a Go program is long-path aware (its runtime sets
// the process's long-path bit), shown on the lab's Windows 11 with
// LongPathsEnabled 0 — so a deep archive is planned and a junction deep in it still
// refuses (design §9 "a path over 260 characters"; del-s1a-A3-01 was not reproduced).
func TestAnArchivePastTwoHundredSixtyCharactersIsPlannedOnWindows(t *testing.T) {
	dir, deep := longArchive(t)
	if len(deep) <= 260 {
		t.Fatalf("fixture: %d characters", len(deep))
	}
	p := mustPlan(t, dir, testDeps())
	if len(p.Remove) != 2 || len(p.Litter) != 1 {
		t.Fatalf("remove %q litter %q", removeRels(p), p.Litter)
	}

	// A junction deep in the tree: the long path's tag is still read.
	target := filepath.Join(t.TempDir(), "Docs")
	writeFile(t, filepath.Join(target, "2024-03-01_0900_x_ab12cd34.html"), "the person's page")
	if err := os.RemoveAll(deep); err != nil {
		t.Fatal(err)
	}
	out, err := nowindow.Hide(exec.Command("cmd", "/c", "mklink", "/J", `\\?\`+deep, target)).CombinedOutput()
	if err != nil {
		t.Skipf("could not make a junction at a long path (lab): %v %s", err, out)
	}
	rc, msg, c := refusalOf(t, dir, testDeps())
	if c != CaseLink {
		t.Fatalf("case = %q (%s)", c, msg)
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("a link"))
}

// covers: MA-957, R36
// Windows: a store token that is the 8.3 short name of the store folder (a tampered
// manifest; the exporter writes long names) reaches the same folder as the listing's
// long name: it is walked as the store, never also kept as the person's
// (del-s1a-A1-10). Skipped where the volume makes no short names.
func TestAStoreFolderNamedByItsShortNameIsNotAlsoKeptOnWindows(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "Person Docs")
	writeFile(t, filepath.Join(long, "Inbox", "2024-03-01_0900_x_ab12cd34.html"), "page")
	p16, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(p16, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		t.Skipf("no short name for %s: %v", long, err)
	}
	short := filepath.Base(syscall.UTF16ToString(buf[:n]))
	if strings.EqualFold(short, filepath.Base(long)) {
		t.Skipf("this volume makes no 8.3 short names (%s)", short)
	}
	writeManifest(t, dir, 6, map[string]string{"path:/m": short}, []rec{{Path: short + "/Inbox/2024-03-01_0900_x_ab12cd34.html"}})
	p := mustPlan(t, dir, testDeps())
	if contains(p.Kept, "Person Docs") {
		t.Fatalf("the store folder is both walked and kept: kept %q", p.Kept)
	}
	if !contains(removeRels(p), short+"/Inbox/2024-03-01_0900_x_ab12cd34.html") {
		t.Fatalf("remove %q", removeRels(p))
	}
}
