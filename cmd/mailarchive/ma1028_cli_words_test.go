package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1028, R36, R12
// The delete verbs' terminal words follow the 1.5.3 friction review's B12: a running Desktop's
// refusal says where Shut down is, with a final period; Your archives that other users can read
// is named in quotes and "readable by other users" is said once, with chmod 600; unlist-archive
// labels the folder it asks about; delete-archive -h says the key goes only with the last
// recorded copy. Positive twin first: a delete at the terminal with nothing in the way works.
func TestTheDeleteVerbsTerminalWords(t *testing.T) {
	e := newCLIDel(t)
	ok := e.plainArchive("Fine")
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Fine"}}, ok, "", "", io.Discard); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}

	dir := e.plainArchive("Archive W")
	before := tree(t, dir)
	release := e.desktopRunning("")
	err := deleteArchive(&scripted{isAttended: true}, dir, "", "", io.Discard)
	release()
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("Use the Desktop, or shut it down first: click Shut down at the top of MailArchive Desktop, then run this again."),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) }))

	// unlist-archive labels the folder (No: nothing changed).
	a := &scripted{isAttended: true, answers: []string{"n"}}
	if err := unlistArchive(a, dir, io.Discard); err == nil || len(a.said) == 0 || a.said[0] != "Folder: "+dir {
		t.Fatalf("unlist-archive's folder line: %v\n%s", err, a.transcript())
	}

	if code, stderr := runCLI("delete-archive", "-h"); code != 0 || !strings.Contains(stderr, "make this computer forget its record and key when this is the last copy it recorded") {
		t.Fatalf("delete-archive -h:\n%s", stderr)
	}

	// Your archives readable by other users (off Windows, where modes are not kept).
	if runtime.GOOS != "windows" {
		if err := os.Chmod(e.indexPath(), 0o644); err != nil {
			t.Fatal(err)
		}
		err = deleteArchive(&scripted{isAttended: true}, dir, "", "", io.Discard)
		os.Chmod(e.indexPath(), 0o600)
		msg := errText(err)
		assure.Refused(t, exitCode(err), msg, assure.Code(1),
			assure.Names(`Your archives ("`+e.indexPath()+`") cannot be read: other users of this computer can read it`, "nothing was changed", `Run chmod 600 "`+e.indexPath()+`", then try again.`),
			assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) && strings.Count(msg, "readable by other users") == 0 }))
	}
}

// covers: MA-1028, R37
// forget-archive says what forgetting a path changes without the word "protection", and its last
// line names the folder as well as the archive's ID (the 1.5.3 friction review's B18). Positive
// twin first: forgetting only a folder still says so.
func TestForgetArchiveNamesTheFolder(t *testing.T) {
	flowRecord(t)
	newArchive := func(name string) (string, string) {
		t.Helper()
		out := filepath.Join(t.TempDir(), name)
		mk := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "", "", keyAnswer}}
		u, err := createEncrypted(mk, out, noKeyFlags(), false, nil)
		if err != nil {
			t.Fatalf("make the archive: %v\n%s", err, mk.transcript())
		}
		u.Destroy()
		return out, recordedAt(t, out)
	}
	twin, _ := newArchive("Folder Only")
	a := &scripted{isAttended: true, answers: []string{"y", "n"}}
	if err := forgetArchive(a, twin); err != nil || !strings.Contains(a.transcript(), "This computer no longer protects "+twin) {
		t.Fatalf("the positive twin: %v\n%s", err, a.transcript())
	}

	out, id := newArchive("Whole")
	a = &scripted{isAttended: true, answers: []string{"y", "y", "y"}}
	if err := forgetArchive(a, out); err != nil {
		t.Fatalf("forget-archive: %v\n%s", err, a.transcript())
	}
	said := a.transcript()
	if !strings.Contains(said, "This computer forgot the encrypted archive "+id+", last recorded at "+out+".") ||
		strings.Contains(said, "protection") || !strings.Contains(said, "MailArchive will no longer stop backups into that path that lack the archive's key.") {
		t.Fatalf("forget-archive's words:\n%s", said)
	}
}
