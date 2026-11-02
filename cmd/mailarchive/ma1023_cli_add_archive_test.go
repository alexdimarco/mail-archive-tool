package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
)

// covers: MA-1023, R36, R12
// An archive made at the command line can be deleted at the command line (the 1.5.3 friction
// review's H3): delete-archive of a folder not in Your archives is refused naming how to list it —
// `mailarchive add-archive DIR`, or Open an archive folder… in MailArchive Desktop (never Add
// another archive, which makes a new archive) — and add-archive lists it, after which
// delete-archive deletes it. Positive twin first: a listed archive is deleted.
func TestAnArchiveMadeAtTheCommandLineIsListedThenDeleted(t *testing.T) {
	e := newCLIDel(t)
	listed := e.plainArchive("Listed")
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Listed"}}, listed, "", "", io.Discard); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "Made At The Terminal")
	e.archiveAt(dir) // an export at the command line: never listed
	before, listBefore, logBefore := tree(t, dir), readFile(t, e.indexPath()), e.logText()
	a := &scripted{isAttended: true, answers: []string{"Made At The Terminal"}}
	err := deleteArchive(a, dir, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names(dir, "is not in Your archives", "nothing was deleted", "run mailarchive add-archive "+encmark.CommandPath(dir), "Open an archive folder… in MailArchive Desktop"),
		assure.NoSideEffect(func() bool {
			return sameTree(before, tree(t, dir)) && bytes.Equal(listBefore, readFile(t, e.indexPath())) && len(a.prompts) == 0 && e.logText() == logBefore
		}))
	if strings.Contains(errText(err), "Add another archive") {
		t.Fatalf("the refusal names the button that makes a new archive: %s", errText(err))
	}

	var out bytes.Buffer
	if err := addArchiveWith("", dir, &out); err != nil {
		t.Fatalf("add-archive: %v", err)
	}
	if !e.listed(dir) || !sameTree(before, tree(t, dir)) || !strings.Contains(out.String(), "Added to Your archives: "+dir) ||
		!strings.Contains(out.String(), "mailarchive delete-archive -out "+encmark.CommandPath(dir)) {
		t.Fatalf("add-archive listed %v, folder unchanged %v:\n%s", e.listed(dir), sameTree(before, tree(t, dir)), out.String())
	}
	// Adding it again changes nothing.
	listAfter := readFile(t, e.indexPath())
	out.Reset()
	if err := addArchiveWith("", dir, &out); err != nil || !strings.Contains(out.String(), "is already in Your archives; nothing was changed.") ||
		!bytes.Equal(listAfter, readFile(t, e.indexPath())) {
		t.Fatalf("add-archive again: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Made At The Terminal"}}, dir, "", "", &out); err != nil {
		t.Fatalf("delete-archive once listed: %v\n%s", err, out.String())
	}
	if e.listed(dir) || !strings.Contains(out.String(), "Deleted.") {
		t.Fatalf("not deleted:\n%s", out.String())
	}
}

// covers: MA-1023, R36, R12
// add-archive lists only a folder that holds an archive, with the checks the Desktop's Open an
// archive folder… gives (archiveFolderProblem): a folder of the person's files is refused, named,
// nothing changed; the folder that holds an archive inside it is not that archive. Positive twin
// first: an archive's own folder is listed. Its help names its flags and its exit set.
func TestAddArchiveListsOnlyAnArchiveFolder(t *testing.T) {
	e := newCLIDel(t)
	good := filepath.Join(t.TempDir(), "Good")
	e.archiveAt(good)
	if err := addArchiveWith("", good, io.Discard); err != nil || !e.listed(good) {
		t.Fatalf("the positive twin: %v (listed %v)", err, e.listed(good))
	}

	plain := filepath.Join(t.TempDir(), "Letters")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "letter.txt"), []byte("the person's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listBefore, folderBefore := readFile(t, e.indexPath()), tree(t, plain)
	err := addArchiveWith("", plain, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names(plain, "isn't a MailArchive archive", "Nothing was changed."),
		assure.NoSideEffect(func() bool {
			return bytes.Equal(listBefore, readFile(t, e.indexPath())) && sameTree(folderBefore, tree(t, plain)) && !e.listed(plain)
		}))

	code, stderr := runCLI("add-archive", "-h")
	for _, w := range []string{"Usage: mailarchive add-archive DIR", "-config", "Open an archive folder…", "Exit: 0 added (or already listed); 1 refused"} {
		if code != 0 || !strings.Contains(stderr, w) {
			t.Fatalf("add-archive -h (exit %d) lacks %q:\n%s", code, w, stderr)
		}
	}
	// The root usage names it.
	if usage := captureExportUsage(t); !strings.Contains(usage, "  mailarchive add-archive DIR") {
		t.Fatalf("the root usage lacks add-archive:\n%s", usage)
	}
}

// covers: MA-1024, MA-1018, R36
// The three Your-archives verbs take -config (the 1.5.3 friction review's B19, the command line's
// part): with -config FILE they read and write the list, the settings and deletions.log beside
// that file, as a MailArchive Desktop started with -config keeps them, and the commands they print
// carry it. Positive twin first: without -config, the default list is used and the other Desktop's
// row is not seen.
func TestTheYourArchivesVerbsReachADesktopStartedWithConfig(t *testing.T) {
	e := newCLIDel(t)
	cfgDir := t.TempDir()
	cfg := filepath.Join(cfgDir, "graph-config.json")
	otherList := filepath.Join(cfgDir, archindex.Name)
	otherLog := filepath.Join(cfgDir, archdelete.LogName)

	dir := filepath.Join(t.TempDir(), "Other Desktop")
	e.archiveAt(dir)
	// add-archive -config lists it beside that config, not in the default list: the program
	// itself, its flag parsed, then in this test's home.
	if code, stderr := runCLI("add-archive", "-config", cfg, dir); code != 0 {
		t.Fatalf("add-archive -config: exit %d\n%s", code, stderr)
	}
	if idx, err := archindex.ReadFile(otherList); err != nil || len(idx.Entries) != 1 || idx.Entries[0].Path != dir {
		t.Fatalf("add-archive -config wrote %v (%v)", idx.Entries, err)
	}
	if err := os.Remove(otherList); err != nil {
		t.Fatal(err)
	}
	if err := addArchiveWith(cfg, dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if idx, err := archindex.ReadFile(otherList); err != nil || len(idx.Entries) != 1 || e.listed(dir) {
		t.Fatalf("add-archive -config wrote %v (%v); default list has it: %v", idx.Entries, err, e.listed(dir))
	}

	// Positive twin: without -config the row is not seen.
	err := deleteArchive(&scripted{isAttended: true, answers: []string{"Other Desktop"}}, dir, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("is not in Your archives"),
		assure.NoSideEffect(func() bool { _, err := os.Stat(dir); return err == nil }))

	// unlist-archive -config sees it (No to the question: nothing changed).
	if err := unlistArchiveWith(cfg, &scripted{isAttended: true, answers: []string{"n"}}, dir, io.Discard); err == nil || err.Error() != "nothing was changed" {
		t.Fatalf("unlist-archive -config did not find the row: %v", err)
	}

	// delete-archive -config deletes it, removes its row from that list and logs in that log.
	var out bytes.Buffer
	if err := deleteArchiveWith(cfg, &scripted{isAttended: true, answers: []string{"Other Desktop"}}, dir, "", "", &out); err != nil {
		t.Fatalf("delete-archive -config: %v\n%s", err, out.String())
	}
	idx, _ := archindex.ReadFile(otherList)
	if _, err := os.Stat(filepath.Join(dir, ".mailarchive-manifest.json")); !os.IsNotExist(err) || len(idx.Entries) != 0 || !strings.Contains(string(readFile(t, otherLog)), `"finished"`) || e.logText() != "" {
		t.Fatalf("delete-archive -config: folder %v, rows %v, log beside the config %q, default log %q\n%s", err, idx.Entries, readFile(t, otherLog), e.logText(), out.String())
	}

	// The commands it prints carry -config: a "Delete again" hint.
	hints := terminalHints("click Delete again", dir, cfg)
	if len(hints) != 1 || !strings.Contains(hints[0], "mailarchive delete-archive -config "+encmark.CommandPath(cfg)+" -out ") {
		t.Fatalf("the hint does not carry -config: %q", hints)
	}
	for _, verb := range []string{"delete-archive", "unlist-archive", "add-archive"} {
		if code, stderr := runCLI(verb, "-h"); code != 0 || !strings.Contains(stderr, "-config") {
			t.Fatalf("%s -h does not name -config:\n%s", verb, stderr)
		}
	}
}

// readFile is the file's bytes (nil when it is not there).
func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, _ := os.ReadFile(p)
	return b
}
