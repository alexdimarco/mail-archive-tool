package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/schedule"
)

// The terminal's side of the other actions on a delete only the folder's marker knows
// (docs/design-archive-delete.md §15.4; §15.10 slice 4; MA-1052).

// covers: MA-1052, R36
// add-archive lists a folder holding the delete marker, with or without its manifest (DM-17), and
// never makes it the current archive: "Added to Your archives: DIR", then "A delete of this archive
// did not finish: to finish it: mailarchive delete-archive -out DIR"; the folder unchanged, the
// Desktop's settings unwritten; delete-archive then finishes it, the person's own file kept. Positive
// twin first: a folder of the person's files without the marker is refused, nothing listed.
func TestMA1052_AddArchiveListsAMarkerFolderAndDeleteFinishesIt(t *testing.T) {
	e := newCLIDel(t)
	plain := filepath.Join(t.TempDir(), "Letters")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "letter.txt"), []byte("the person's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := addArchiveWith("", plain, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names(plain, "isn't a MailArchive archive"),
		assure.NoSideEffect(func() bool { return !e.listed(plain) }))

	withManifest := filepath.Join(t.TempDir(), "Begun elsewhere")
	e.archiveAt(withManifest)
	ma1048Plant(t, withManifest)
	filesGone := filepath.Join(t.TempDir(), "Files gone")
	if err := os.MkdirAll(filesGone, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{".mailarchive-lastrun.json": "{}", "notes.txt": "the person's own notes\n"} {
		if err := os.WriteFile(filepath.Join(filesGone, f), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ma1048Plant(t, filesGone)
	for _, dir := range []string{withManifest, filesGone} {
		before := tree(t, dir)
		var out bytes.Buffer
		if err := addArchiveWith("", dir, &out); err != nil {
			t.Fatalf("add-archive %s: %v", dir, err)
		}
		want := "Added to Your archives: " + dir + "\nA delete of this archive did not finish: to finish it: mailarchive delete-archive -out " + encmark.CommandPath(dir) + "\n"
		if out.String() != want || !e.listed(dir) || !sameTree(before, tree(t, dir)) {
			t.Fatalf("add-archive %s: listed %v, folder unchanged %v:\n%s", dir, e.listed(dir), sameTree(before, tree(t, dir)), out.String())
		}
		if _, err := os.Stat(e.settingsPath()); !os.IsNotExist(err) {
			t.Fatalf("the Desktop's settings were written: %v", err)
		}
		out.Reset()
		a := &scripted{isAttended: true, answers: []string{filepath.Base(dir)}}
		if err := deleteArchive(a, dir, "", "", &out); err != nil {
			t.Fatalf("delete-archive %s: %v\n%s", dir, err, out.String())
		}
		// Planned from the marker and not recorded here, its kind is not known: never "the unknown
		// archive" (§15.4).
		head := "MailArchive will permanently delete the plain archive in this folder (no Recycle Bin or Trash):"
		if dir == filesGone {
			head = "MailArchive will permanently delete the archive in this folder (no Recycle Bin or Trash):"
		}
		if !contains(a.said, head) || !contains(a.said, archdelete.MsgMarkerNoRecord) {
			t.Fatalf("delete-archive %s's plan: %q", dir, a.said)
		}
		if e.listed(dir) || exists(filepath.Join(dir, delmark.Name)) || !exists(filepath.Join(dir, "notes.txt")) {
			t.Fatalf("delete-archive %s: listed %v\n%s", dir, e.listed(dir), out.String())
		}
	}
}

// covers: MA-1052, R36
// `mailarchive schedule -install` refuses a folder holding the delete marker that this computer's
// deletion log knows nothing of (§15.4, DM-11; deferred from slice 2), run as the real program
// against stand-in scheduler programs: the marker-only words, "…so its schedule was not
// installed…", never "click Delete again", the terminal's way to finish it, exit 1, no entry and no
// schedule record made. Positive twin first: another folder, with the same empty log, is installed.
func TestMA1052_TerminalScheduleInstallRefusesAMarkerFolder(t *testing.T) {
	sandboxScheduler(t)
	half := filepath.Join(t.TempDir(), "Begun elsewhere")
	other := filepath.Join(t.TempDir(), "Other")
	for _, d := range []string{half, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".mailarchive-manifest.json"), []byte(`{"version":1,"records":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ma1048Plant(t, half)
	if logPath, err := archdelete.DefaultLogPath(); err != nil {
		t.Fatal(err)
	} else if u, err := archdelete.UnfinishedDelete(logPath, half, ""); err != nil || u != nil {
		t.Fatalf("precondition: this computer's log knows nothing of it: %v %v", u, err)
	}

	// Positive twin.
	if code, stdout, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", other, "-auto"); code != 0 || schedule.Query(schedule.DefaultNameFor(other)) != schedule.Installed {
		t.Fatalf("positive twin: schedule -install of another folder (%d): %s%s", code, stdout, stderr)
	}

	code, stdout, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", half, "-auto")
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names(delmark.OnlyMarkerWords("its schedule was not installed"), "To finish it at this terminal: mailarchive delete-archive -out"),
		assure.Forbid("click Delete again"),
		assure.NoSideEffect(func() bool {
			_, derr := os.Stat(filepath.Join(half, schedule.DescriptorName))
			return schedule.Query(schedule.DefaultNameFor(half)) != schedule.Installed && os.IsNotExist(derr)
		}))
	if t.Failed() {
		t.Logf("stdout: %s", stdout)
	}
}

// ma1052Started writes a started line for folder into this home's deletion log, as a delete begun
// here and stopped leaves it.
func ma1052Started(t *testing.T, e *cliDel, folder string) {
	t.Helper()
	line, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "ffffeeeeddddcccc", "folder": folder, "row": folder, "kind": "plain"})
	if err != nil {
		t.Fatal(err)
	}
	p := e.logPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}
