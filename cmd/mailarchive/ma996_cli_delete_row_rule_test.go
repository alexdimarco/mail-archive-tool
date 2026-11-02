package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	jobfile "mail-archive-tool/internal/job"
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
	// A name that does not match changes nothing either: the list is written only by a delete
	// about to change something.
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive X"}}, c, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("does not match"),
		assure.NoSideEffect(func() bool {
			l, _ := os.ReadFile(e.indexPath())
			s, _ := os.ReadFile(e.settingsPath())
			return bytes.Equal(listBefore, l) && bytes.Equal(settingsBefore, s)
		}))
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive C"}}, c, "", "", &out); err != nil {
		t.Fatalf("delete-archive of the Desktop's current archive, not in its list yet: %v\n%s", err, out.String())
	}
	if r := out.String(); !strings.Contains(r, "Deleted.") || !strings.Contains(r, "Removed from Your archives") || strings.Contains(r, "No row for this folder was found") ||
		e.listed(c) || desktop.LoadSettings(e.settingsPath()).Out != "" {
		t.Fatalf("the current archive's delete:\n%s", r)
	}

	// Such a row whose delete stops part way, after step 4 cleared the settings that drew it, is a
	// row still: the delete listed it before it changed anything, as the Desktop's tidy-up would
	// have, so it is "Delete not finished" and running the delete again finishes it (the residual
	// fixes' adversarial pass, finding 1).
	d := e.plainArchive("Archive D")
	os.Remove(filepath.Join(d, "notes.txt")) // nothing of the person's: the folder goes
	e.unlistForTest(d)
	if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = d }); err != nil {
		t.Fatal(err)
	}
	// The list cannot be written then (its folder made read-only, off Windows): refused before
	// anything changed, the row still drawn by the settings.
	if runtime.GOOS != "windows" {
		listBefore, _ := os.ReadFile(e.indexPath())
		beforeD := tree(t, d)
		beforeDeleteStep = func(step int) {
			if step == 2 {
				os.Chmod(e.configDir(), 0o500)
			}
		}
		out.Reset()
		err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive D"}}, d, "", "", &out)
		beforeDeleteStep = func(int) {}
		os.Chmod(e.configDir(), 0o700)
		afterD := tree(t, d)
		delete(afterD, filepath.Join(d, ".mailarchive.lock"))
		assure.Refused(t, exitCode(err), out.String(), assure.Code(1),
			assure.Names("Your archives ("+e.indexPath()+") could not be saved", "nothing was deleted"),
			assure.NoSideEffect(func() bool {
				l, _ := os.ReadFile(e.indexPath())
				return bytes.Equal(listBefore, l) && sameTree(beforeD, afterD) && desktop.LoadSettings(e.settingsPath()).Out == d
			}))
	}
	var release func()
	beforeDeleteStep = func(step int) {
		if step == 8 {
			release = e.desktopRunning("2")
		}
	}
	out.Reset()
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive D"}}, d, "", "", &out)
	beforeDeleteStep = func(int) {}
	logPath, lerr := archdelete.DefaultLogPath()
	if lerr != nil {
		t.Fatal(lerr)
	}
	unf, uerr := archdelete.UnfinishedDelete(logPath, d, runtime.GOOS)
	if exitCode(err) != 3 || !e.listed(d) || desktop.LoadSettings(e.settingsPath()).Out != "" || uerr != nil || unf == nil {
		t.Fatalf("a drawn row's delete stopped before step 8: exit %d (%v), listed %v, unfinished %v (%v)\n%s", exitCode(err), err, e.listed(d), unf != nil, uerr, out.String())
	}
	release()
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive D"}}, d, "", "", &out); err != nil {
		t.Fatalf("delete-archive again of a drawn row stopped part way: %v\n%s", err, out.String())
	}
	if e.listed(d) || !strings.Contains(out.String(), "Removed from Your archives") {
		t.Fatalf("delete-archive again did not finish:\n%s", out.String())
	}

	// The archive the wizard made last, not in the list yet, is removed from Your archives at the
	// terminal: listed first, then removed, and the wizard's note of it cleared.
	w := e.plainArchive("Archive W")
	e.unlistForTest(w)
	lastFile, err := jobfile.LastArchiveFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lastFile, []byte(w+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, w, &out); err != nil {
		t.Fatalf("unlist-archive of the wizard's last archive, not in the list yet: %v\n%s", err, out.String())
	}
	if r := out.String(); !strings.Contains(r, "Removed from Your archives") || e.listed(w) || jobfile.ReadLastArchive(lastFile) != "" {
		t.Fatalf("Remove from list of the wizard's last archive:\n%s", r)
	}

	// An archive the Desktop draws is another known archive to a delete (§7), as on the Desktop:
	// a listed folder that holds it is refused at the plan, nothing logged and no name asked
	// (finding 4's sibling).
	p := e.plainArchive("Archive P")
	inner := filepath.Join(p, "Inner")
	e.archiveAt(inner)
	setOut := func(out string) {
		t.Helper()
		if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = out }); err != nil {
			t.Fatal(err)
		}
	}
	setOut(inner)
	beforeP, logBefore := tree(t, p), e.logText()
	a = &scripted{isAttended: true, answers: []string{"Archive P"}}
	err = deleteArchive(a, p, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("will not delete this folder", inner),
		assure.NoSideEffect(func() bool {
			return sameTree(beforeP, tree(t, p)) && e.listed(p) && !e.listed(inner) && e.logText() == logBefore && !prompted(a, "Type the folder's name")
		}))
	// Drawn only once the name is being typed: Execute's own plan under the lock refuses it.
	setOut("")
	h := &hooked{scripted: &scripted{isAttended: true, answers: []string{"Archive P"}}, at: "Type the folder's name", hook: func() { setOut(inner) }}
	out.Reset()
	err = deleteArchive(h, p, "", "", &out)
	afterP := tree(t, p)
	delete(afterP, filepath.Join(p, ".mailarchive.lock")) // the delete's own lock, released
	if exitCode(err) != 1 || !strings.Contains(out.String(), "will not delete this folder") || !sameTree(beforeP, afterP) || !e.listed(p) {
		t.Fatalf("an archive the Desktop draws from the name on: exit %d (%v)\n%s", exitCode(err), err, out.String())
	}
}
