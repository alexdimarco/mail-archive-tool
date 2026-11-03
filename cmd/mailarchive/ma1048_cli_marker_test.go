package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
)

// ma1048Plant writes a regular delete marker into dir by hand, as a delete stopped part way leaves
// it (design-archive-delete §15.1; nothing writes it until slice 3).
func ma1048Plant(t *testing.T, dir string) {
	t.Helper()
	body := `{"what":"` + delmark.What + `","version":1,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z","note":"` + delmark.Note + `"}`
	if err := os.WriteFile(filepath.Join(dir, delmark.Name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1048, R36
// The terminal's backups refuse a folder holding the delete marker when this computer's deletion
// log has no line for it (another computer or account began the delete; §15.3, MED-2) — export
// attended and -unattended, and `graph` — in the marker-only words (DM-11) with "(Folder: DIR)"
// and the terminal's way to finish it, never "Delete again"; and `pause` and `resume` refuse it in
// their words (§15.4, through ActionRefusal). The folder is unchanged. Run as the real program in
// this package's throwaway home, whose deletions.log is empty. Positive twin first: the folder
// beside it is backed up and paused.
func TestMA1048_TerminalBackupRefusesAFolderHoldingTheMarker(t *testing.T) {
	ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	half, other := filepath.Join(t.TempDir(), "Half deleted"), filepath.Join(t.TempDir(), "Other")
	for _, d := range []string{half, other} {
		if code, stderr := runCLI("-out", d, "-input", src); code != 0 {
			t.Fatalf("the archive %s: exit %d %s", d, code, stderr)
		}
	}
	if code, stderr := runCLI("-out", half, "-input", src, "-unattended", "-log", filepath.Join(half, "mailarchive-backup.log")); code != 0 {
		t.Fatalf("the earlier scheduled backup: exit %d %s", code, stderr)
	}
	ma1048Plant(t, half)

	// Positive twins.
	if code, stderr := runCLI("-out", other, "-input", src, "-unattended"); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, stderr)
	}

	before := ma1044Files(t, half)
	unchanged := func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }
	names := assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")", "To finish it at this terminal: mailarchive delete-archive -out "+encmark.CommandPath(half))
	forbid := assure.Forbid("Delete again", "click Delete again")

	code, stderr := runCLI("-out", half, "-input", src)
	assure.Refused(t, code, stderr, assure.Code(1), names, forbid, assure.NoSideEffect(unchanged))

	code, stderr = runCLI("-out", half, "-input", src, "-unattended", "-log", filepath.Join(half, "mailarchive-backup.log"))
	assure.Refused(t, code, stderr, assure.Code(1), names, forbid, assure.NoSideEffect(unchanged))

	code, stderr = runCLI("graph", "-auth", "device", "-out", half, "-tenant", "t", "-client-id", "c", "-unattended")
	assure.Refused(t, code, stderr, assure.Code(1), names, forbid, assure.NoSideEffect(unchanged))

	for _, verb := range []string{"pause", "resume"} {
		code, stderr = runCLI(verb, "-out", half)
		assure.Refused(t, code, stderr, assure.Code(1),
			assure.Names(delmark.OnlyMarkerWords("it was not "+verb+"d")+" (Folder: "+half+")", "To finish it at this terminal: mailarchive delete-archive -out"),
			forbid, assure.NoSideEffect(unchanged))
	}
}

// covers: MA-1049, R36
// `status` sees the delete marker (DM-15, §15.4): a folder holding it with no line in this
// computer's deletion log is RED, "a delete of this archive begun DATE did not finish, and this
// computer's deletion log has no record of it (it was begun on another computer or user account)
// — run mailarchive delete-archive -out DIR, or click Delete… on its row in MailArchive Desktop, to
// finish it." ("again" dropped: nothing was begun here); a dangling link at the name — which a
// stat that follows links reads as "not there" — is seen too, its date not known. Positive twin
// first: the same archive without it says nothing of the kind.
func TestMA1049_StatusSeesTheMarker(t *testing.T) {
	ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "Begun elsewhere")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, stderr)
	}

	// Positive twin.
	got, code := statusOf(t, dir)
	if code != 0 || strings.Contains(got, "did not finish") {
		t.Fatalf("positive twin:\n%s", got)
	}

	ma1048Plant(t, dir)
	began := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC).Local().Format("2006-01-02 15:04 -07:00")
	tail := ", and this computer's deletion log has no record of it (it was begun on another computer or user account) — run mailarchive delete-archive -out " + encmark.CommandPath(dir) + ", or click Delete… on its row in MailArchive Desktop, to finish it."
	got, code = statusOf(t, dir)
	if code != 0 || !strings.Contains(got, "RED") || !strings.Contains(got, "a delete of this archive begun "+began+" did not finish"+tail) {
		t.Fatalf("status of a folder holding the marker:\n%s", got)
	}

	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "nothing"), filepath.Join(dir, delmark.Name)); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("a link needs a privilege the run may not have")
		}
		t.Fatal(err)
	}
	// Amended 2026-11-03 (slice 2b, the slice-2 pass's LOW-4 (b)): a name that is not a regular file
	// is said as such, to remove by hand, never "run mailarchive delete-archive" (its Delete refuses
	// it); still exact.
	got, code = statusOf(t, dir)
	if code != 0 || !strings.Contains(got, "RED: this archive's folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device), so nothing is backed up into it — if no delete of this archive is under way, remove it by hand.") ||
		strings.Contains(got, "delete-archive") {
		t.Fatalf("status of a folder holding a dangling link at the marker's name:\n%s", got)
	}
}
