package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/state"
)

// growManifest adds n records to the real archive in dir through the state package, under its
// first store, as a backup of n more messages records them; it requires the manifest to end over
// 1 MiB (an archive of thousands of messages).
func growManifest(t *testing.T, dir string, n int) {
	t.Helper()
	man := filepath.Join(dir, ".mailarchive-manifest.json")
	m, err := state.Load(man)
	if err != nil {
		t.Fatal(err)
	}
	tok := ""
	for _, v := range m.Stores {
		if tok == "" || v < tok {
			tok = v
		}
	}
	if tok == "" {
		t.Fatalf("the archive in %s records no store", dir)
	}
	at := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		m.Add(fmt.Sprintf("%s\x00<grown-%08d@mail.example.com>", tok, i), state.Record{
			Path:        fmt.Sprintf("%s/Inbox/2024-03-01_0900_a-subject-slug-for-a-real-message_%08x.html", tok, i),
			Folder:      "Inbox",
			ExportedAt:  at,
			Fingerprint: fmt.Sprintf("%016x", i),
			FpScheme:    state.FpSchemeCurrent,
		})
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(man); err != nil || fi.Size() <= 1<<20 {
		t.Fatalf("the grown manifest must be over 1 MiB: %v %v", fi, err)
	}
}

// covers: MA-1034, MA-1023, R36, R12
// An archive of thousands of messages, made at the command line, is listed by add-archive and
// then deleted by delete-archive (the 1.5.3 re-walk's N1: add-archive refused any archive whose
// manifest was over 1 MiB, about 2,200 messages and up, so a command-line-only user still could
// not delete an archive of ordinary size). Positive twin first: the same archive before it grew.
func TestAddArchiveListsALargeArchiveThenItIsDeleted(t *testing.T) {
	e := newCLIDel(t)
	small := filepath.Join(t.TempDir(), "Small")
	e.archiveAt(small)
	if err := addArchiveWith("", small, io.Discard); err != nil || !e.listed(small) {
		t.Fatalf("the positive twin: %v (listed %v)", err, e.listed(small))
	}

	dir := filepath.Join(t.TempDir(), "Large Archive")
	e.archiveAt(dir)
	growManifest(t, dir, 6000)
	var out bytes.Buffer
	if err := addArchiveWith("", dir, &out); err != nil {
		t.Fatalf("add-archive refused a large archive: %v", err)
	}
	if !e.listed(dir) || !strings.Contains(out.String(), "Added to Your archives: "+dir) {
		t.Fatalf("add-archive did not list a large archive:\n%s", out.String())
	}
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Large Archive"}}, dir, "", "", &out); err != nil {
		t.Fatalf("delete-archive of the large archive: %v\n%s", err, out.String())
	}
	if e.listed(dir) || !strings.Contains(out.String(), "Deleted.") {
		t.Fatalf("not deleted:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".mailarchive-manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("the manifest is still there: %v", err)
	}
}

// covers: MA-1023, MA-1034, R36, R12
// add-archive's refusals of a folder with no archive, or with a record that cannot be read, use
// the terminal's words, never the Desktop's controls (the 1.5.3 re-walk's N1): the record case
// names `mailarchive status -out DIR`, not Settings → Archive location or Status & health. While
// a Desktop runs, the delete command add-archive prints says it is for when the Desktop is shut
// down, beside the Desktop's own Delete… (N11). Positive twins first: an archive's folder is
// listed with the delete command as it was, no Desktop running.
func TestAddArchiveSpeaksTheTerminalsWords(t *testing.T) {
	e := newCLIDel(t)
	good := filepath.Join(t.TempDir(), "Good")
	e.archiveAt(good)
	var out bytes.Buffer
	if err := addArchiveWith("", good, &out); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	want := "The folder and its files are unchanged. To delete it: mailarchive delete-archive -out " + encmark.CommandPath(good) + "\n"
	if !strings.Contains(out.String(), want) || strings.Contains(out.String(), "shut down") {
		t.Fatalf("the positive twin's words:\n%s", out.String())
	}

	newer := filepath.Join(t.TempDir(), "Newer")
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatal(err)
	}
	// A damaged record (amended 2026-11-13: it was a version-99 one, which now says the newer
	// format's own words, MA-1297; a damaged record is still "cannot be read").
	if err := os.WriteFile(filepath.Join(newer, ".mailarchive-manifest.json"), []byte(`{"version":6,"stores":{damaged`), 0o644); err != nil {
		t.Fatal(err)
	}
	listBefore, folderBefore := readFile(t, e.indexPath()), tree(t, newer)
	err := addArchiveWith("", newer, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names(newer+" holds an archive whose record (.mailarchive-manifest.json) cannot be read — run mailarchive status -out "+encmark.CommandPath(newer)+" to see why and how to repair it. Nothing was changed."),
		assure.NoSideEffect(func() bool {
			return bytes.Equal(listBefore, readFile(t, e.indexPath())) && sameTree(folderBefore, tree(t, newer)) && !e.listed(newer)
		}))
	empty := t.TempDir()
	err = addArchiveWith("", empty, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names(empty+" isn't a MailArchive archive — give the folder that holds the .mailarchive-manifest.json file. Nothing was changed."),
		assure.NoSideEffect(func() bool { return bytes.Equal(listBefore, readFile(t, e.indexPath())) && !e.listed(empty) }))
	for _, desk := range []string{"Settings", "Archive location", "Status & health", "choose the folder"} {
		if strings.Contains(errText(addArchiveWith("", newer, io.Discard)), desk) || strings.Contains(errText(err), desk) {
			t.Fatalf("add-archive names the Desktop's %q at a terminal", desk)
		}
	}

	// While a Desktop runs.
	stop := e.desktopRunning("")
	second := filepath.Join(t.TempDir(), "Second")
	e.archiveAt(second)
	out.Reset()
	if err := addArchiveWith("", second, &out); err != nil || !e.listed(second) {
		t.Fatalf("add-archive while a Desktop runs: %v", err)
	}
	// Amended 2026-11-02 (the MA-1034 adversarial pass, finding 5): the Desktop's Delete… first,
	// delete-archive a separate choice once the Desktop is shut down.
	want = "To delete it: click Delete… on its row in Your archives in MailArchive Desktop, or shut MailArchive Desktop down and run mailarchive delete-archive -out " + encmark.CommandPath(second) + ".\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("while a Desktop runs, add-archive says:\n%s\nwant %q", out.String(), want)
	}
	stop()
}
