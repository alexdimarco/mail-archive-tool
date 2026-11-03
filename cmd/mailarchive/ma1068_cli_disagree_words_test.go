package main

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/health"
)

// covers: MA-1068, R36
// DM-27's words at the terminal (design-archive-delete §15.4; the slice-5 adversarial pass's LOW-4;
// lead's words, owner-delegated 2026-11-02), as the real program in this package's throwaway home:
// this computer's deletion log has a delete of the folder that wrote its `marked` line, and the
// folder holds its archive with no note (a rotation drive, or the note removed by hand). A backup is
// refused, exit 1, in DM-27's words with "(Folder: DIR)" and the terminal's way to finish it; `status`
// says DM-27's line. The folder is byte-identical. Positive twin first: the same started line with no
// `marked` line keeps the log's own words, "click Delete again", and its terminal Delete again.
func TestMA1068_TheTerminalNamesTheDriveTheDeleteBeganOn(t *testing.T) {
	logPath := ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "Rotation")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, stderr)
	}
	const run = "c1068aaaaaaaaaaa"
	ma1044Line(t, logPath, map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": run, "folder": dir, "row": dir, "kind": "plain"})
	// unchanged is the folder as it is now, for the refused run that follows (`status` opens the
	// search index, which touches its shared-memory file, so each run takes its own snapshot).
	unchanged := func() assure.RefuseOption {
		before := ma1044Files(t, dir)
		return assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, dir)) })
	}

	// Positive twin: no `marked` line.
	same := unchanged()
	code, stderr := runCLI("-out", dir, "-input", src)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(ma1044Words+" (Folder: "+dir+")", "At this terminal, Delete again is: mailarchive delete-archive -out"), same)
	if got, code := statusOf(t, dir); code != 0 || !strings.Contains(got, health.DeleteUnfinishedLine(dir, "")) {
		t.Fatalf("positive twin, status:\n%s", got)
	}

	ma1044Line(t, logPath, map[string]any{"at": "2026-11-02T10:00:01Z", "event": "marked", "run": run, "folder": dir, "first_run": run})
	same = unchanged()
	code, stderr = runCLI("-out", dir, "-input", src)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names(archdelete.DisagreesWords("it was not backed up")+" (Folder: "+dir+")", "To finish it at this terminal: mailarchive delete-archive -out "+encmark.CommandPath(dir)), same)
	if strings.Contains(stderr, "click Delete again") {
		t.Errorf("the refusal still says click Delete again:\n%s", stderr)
	}
	got, code := statusOf(t, dir)
	if code != 0 || !strings.Contains(got, "RED: "+health.DeleteDisagreesLine(dir, "")) || !strings.Contains(got, "plug in the drive the delete began on") {
		t.Fatalf("status:\n%s", got)
	}
}
