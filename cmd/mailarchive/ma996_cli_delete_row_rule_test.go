package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/schedule"
)

// unlistForTest takes p out of Your archives, keeping its folder.
func (e *cliDel) unlistForTest(p string) {
	e.t.Helper()
	idx, err := archindex.ReadFile(e.indexPath())
	if err != nil {
		e.t.Fatal(err)
	}
	if err := archindex.WriteFile(e.indexPath(), archindex.Forget(idx, p)); err != nil {
		e.t.Fatal(err)
	}
}

// covers: MA-996, R36, R12
// delete-archive deletes only a row of Your archives (design-archive-delete §3.1's row rule,
// §3.3): a folder that is not a row — never listed, a mistyped path, or one seen from an
// elevated or another account's terminal — is refused, named, before the plan; a plan refusal
// of a row names its folder, and a row with no archive left is told the terminal's Remove from
// list, which then works.
func TestDeleteArchiveDeletesOnlyARowOfYourArchives(t *testing.T) {
	e := newCLIDel(t)

	// Positive twin: a row, given through another spelling of it (a link, off Windows), is
	// deleted and its row removed.
	r := e.plainArchive("Archive R")
	spelling := r
	if runtime.GOOS != "windows" {
		spelling = filepath.Join(t.TempDir(), "MyMail")
		if err := os.Symlink(r, spelling); err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive R"}}, spelling, "", "", io.Discard); err != nil {
		t.Fatalf("delete-archive of a row: %v", err)
	}
	if e.listed(r) {
		t.Fatal("the row is still in Your archives")
	}

	// A folder that is not a row: refused before the plan, named, nothing changed or logged.
	u := e.plainArchive("Archive U")
	e.unlistForTest(u)
	uEntry := schedule.DefaultNameFor(u)
	e.sched.installed[uEntry] = true
	beforeU, logBefore := tree(t, u), e.logText()
	a := &scripted{isAttended: true, answers: []string{"Archive U"}}
	var out bytes.Buffer
	err := deleteArchive(a, u, "", "", &out)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names(u, "is not in Your archives", "nothing was deleted", "not one run as administrator or as another user", "Add another archive"),
		assure.NoSideEffect(func() bool {
			return sameTree(beforeU, tree(t, u)) && e.logText() == logBefore && e.sched.installed[uEntry] && len(a.prompts) == 0 && out.Len() == 0
		}))

	// A mistyped path is refused naming what was looked up.
	typo := filepath.Join(filepath.Dir(u), "Archvie U")
	err = deleteArchive(&scripted{isAttended: true}, typo, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names(typo, "is not in Your archives"),
		assure.NoSideEffect(func() bool { return e.logText() == logBefore }))

	// A row whose folder holds no archive: the refusal names the folder and the terminal's
	// Remove from list, and that removes the row (no dead end).
	n := filepath.Join(t.TempDir(), "Not an archive")
	if err := os.MkdirAll(n, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(n, "letter.txt"), []byte("the person's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.list(n)
	beforeN := tree(t, n)
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Not an archive"}}, n, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("no longer holds an archive", "(Folder: "+n+")", "At this terminal, Remove from list is: mailarchive unlist-archive -out"),
		assure.NoSideEffect(func() bool { return sameTree(beforeN, tree(t, n)) && e.listed(n) && e.logText() == logBefore }))
	if err := unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, n, io.Discard); err != nil {
		t.Fatalf("unlist-archive of the row the refusal named: %v", err)
	}
	if e.listed(n) || !sameTree(beforeN, tree(t, n)) {
		t.Fatal("Remove from list at the terminal did not remove the row, or changed the folder")
	}

	// The Desktop's current archive not in its list yet (the Desktop adds it when it next draws
	// its page, as right after the wizard): a row all the same, matched in memory with no file
	// written; deleted, its "row" gone, and the Desktop draws it no more (del-s4 residual 3).
	c := e.plainArchive("Archive C")
	e.unlistForTest(c)
	if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = c }); err != nil {
		t.Fatal(err)
	}
	listBefore, _ := os.ReadFile(e.indexPath())
	settingsBefore, _ := os.ReadFile(e.settingsPath())
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true}, c, filepath.Join(t.TempDir(), "plan.txt"), "", &out); err != nil {
		t.Fatalf("the plan of the Desktop's current archive, not in its list yet: %v\n%s", err, out.String())
	}
	listAfter, _ := os.ReadFile(e.indexPath())
	settingsAfter, _ := os.ReadFile(e.settingsPath())
	if !bytes.Equal(listBefore, listAfter) || !bytes.Equal(settingsBefore, settingsAfter) || e.listed(c) {
		t.Fatal("matching the current archive as a row wrote the list or the settings")
	}
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive C"}}, c, "", "", &out); err != nil {
		t.Fatalf("delete-archive of the Desktop's current archive, not in its list yet: %v\n%s", err, out.String())
	}
	if r := out.String(); !strings.Contains(r, "Deleted.") || !strings.Contains(r, "Removed from Your archives") || strings.Contains(r, "No row for this folder was found") ||
		e.listed(c) || desktop.LoadSettings(e.settingsPath()).Out != "" {
		t.Fatalf("the current archive's delete:\n%s", r)
	}
}
