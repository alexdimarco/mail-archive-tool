package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
)

// covers: MA-1049, R36
// `status` says what the marker's LOW-4 cases are (the slice-2 pass; lead's words, owner-delegated
// 2026-11-02), as the real program: a folder at the marker's name is something MailArchive did not
// write, to remove by hand — never "run mailarchive delete-archive", whose Delete refuses it; and
// with this computer's deletion log unreadable, the line says the log could not be read and why,
// never that it "has no record of it" — and the terminal's refusal of an action then (`verify`)
// says the same with its own way to finish it, nothing written. Positive twin first: a regular
// marker with the log read gives the marker-only line.
func TestMA1049_StatusSaysWhatIsAtTheMarkersName(t *testing.T) {
	logPath := ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "Begun elsewhere")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, stderr)
	}
	ma1048Plant(t, dir)
	began := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC).Local().Format("2006-01-02 15:04 -07:00")
	cmd := encmark.CommandPath(dir)

	// Positive twin.
	if got, code := statusOf(t, dir); code != 0 || !strings.Contains(got, "this computer's deletion log has no record of it") {
		t.Fatalf("positive twin:\n%s", got)
	}

	// The log cannot be read.
	os.RemoveAll(logPath)
	if err := os.Mkdir(logPath, 0o700); err != nil {
		t.Fatal(err)
	}
	cause := "MailArchive's deletion log " + logPath + " must be a regular file (not a symlink, pipe or device)"
	got, code := statusOf(t, dir)
	if code != 0 || !strings.Contains(got, "RED: a delete of this archive begun "+began+" did not finish, and MailArchive could not read this computer's deletion log ("+cause+") — check that file, then run mailarchive delete-archive -out "+cmd+", or click Delete… on its row in MailArchive Desktop, to finish it.") ||
		strings.Contains(got, "has no record") {
		t.Fatalf("status with the log unreadable:\n%s", got)
	}
	// The terminal's refusal says it too, with its own way to finish (verify, as any action).
	before := ma1044Files(t, dir)
	vcode, stderr := runCLI("verify", "-out", dir)
	assure.Refused(t, vcode, stderr, assure.Code(1),
		assure.Names(delmark.LogUnreadableWords("it was not checked", cause)+" (Folder: "+dir+")", "To finish it at this terminal: mailarchive delete-archive -out "+cmd),
		assure.Forbid("has no record"), assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, dir)) }))
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}

	// A folder at the marker's name.
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, delmark.Name), 0o755); err != nil {
		t.Fatal(err)
	}
	got, code = statusOf(t, dir)
	if code != 0 || !strings.Contains(got, "RED: this archive's folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device), so nothing is backed up into it — if no delete of this archive is under way, remove it by hand.") ||
		strings.Contains(got, "delete-archive") {
		t.Fatalf("status of a folder at the marker's name:\n%s", got)
	}
}
