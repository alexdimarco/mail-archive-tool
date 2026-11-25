package wizard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/nowindow"
	"mail-archive-tool/internal/winsec"
)

// setLinkTo makes link a link to the folder target: a junction on Windows (cmd's mklink /J, which
// needs no privilege), a symbolic link elsewhere.
func setLinkTo(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := nowindow.Hide(exec.Command("cmd", "/c", "mklink", "/J", link, target)).CombinedOutput(); err != nil {
			t.Fatalf("could not make a junction %s -> %s: %v %s", link, target, err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// setFresh is a new, empty parent folder for a set.
func setFresh(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Mail archives")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// covers: MA-1496, MA-1495, MA-1494, S90
// Each new set folder is the owner's, and the plan names what it leaves (the adversarial pass's INS-01
// with CON-02; the lead's fold). On Windows a folder made with Mkdir takes its parent's access list,
// which outside the person's own folders lets other accounts change it, so a re-run never looked at
// the set's own archives and made "(2)" copies, and nothing said why. Each new folder is restricted to
// its owner (restrictSetFolder, winsec.RestrictFolderToOwner) right after it is made, before anything
// is written in it, and only new folders are; a restriction the drive cannot take leaves the archive
// going on. With the real restriction, each new folder reads as the person's own (setChildMine, the
// re-run's own test). The plan names every child it did not look at (a folder others can change, a
// link), so a " (2)" beside one of their names is never unexplained. Amended 2026-11-25 (round D, the
// lead's call revising the review's RV-05 fold; additive): the restriction is now
// winsec.RestrictFolderToOwnerAndAdmins, which keeps SYSTEM and Administrators beside the owner, so
// backup software that runs as SYSTEM can copy the archives; the real restriction below still reads
// as the person's own. Its access list is winsec's tests' (on Windows, the lab's).
func TestANewSetFolderIsTheOwnersAndThePlanNamesWhatItLeaves(t *testing.T) {
	if reflect.ValueOf(restrictSetFolder).Pointer() != reflect.ValueOf(winsec.RestrictFolderToOwnerAndAdmins).Pointer() {
		t.Fatal("the set's new folders are not restricted with winsec.RestrictFolderToOwnerAndAdmins (owner, SYSTEM and Administrators)")
	}
	ownSettings(t)
	rows, _ := setMail(t)
	parent := setFresh(t)
	var asked, notEmpty []string
	real := restrictSetFolder
	t.Cleanup(func() { restrictSetFolder = real })
	restrictSetFolder = func(p string) error {
		asked = append(asked, p)
		if ents, err := os.ReadDir(p); err != nil || len(ents) != 0 {
			notEmpty = append(notEmpty, p)
		}
		return nil
	}
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err := makeSet(rows, "darwin")
	if err != nil || len(made) != 5 {
		t.Fatalf("the set: %+v %v", made, err)
	}
	var want []string
	for _, m := range made {
		want = append(want, m.Folder)
	}
	if !reflect.DeepEqual(asked, want) || len(notEmpty) != 0 {
		t.Fatalf("each new folder restricted once, right after it is made: asked %q, want %q; not empty then: %q", asked, want, notEmpty)
	}

	// A re-run: only the folder made anew is restricted, never an archive brought up to date.
	lf := filepath.Join(parent, rows[3].name)
	if err := os.RemoveAll(lf); err != nil {
		t.Fatal(err)
	}
	asked = nil
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	if made, err = makeSet(rows, "darwin"); err != nil || !reflect.DeepEqual(asked, []string{lf}) {
		t.Fatalf("the re-run restricted %q (%+v %v), want only %s", asked, made, err, lf)
	}

	// A restriction the drive cannot take (a FAT or exFAT drive keeps no access list): the archive
	// goes on as Mkdir made its folder.
	restrictSetFolder = func(string) error { return errors.New("this drive keeps no access list") }
	s = &flowScript{folders: []string{setFresh(t)}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err = makeSet(rows, "darwin")
	for _, m := range made {
		if err != nil || m.Outcome != MadeFinished {
			t.Errorf("a restriction refused stopped an archive: %+v %v", m, err)
		}
	}

	// The real restriction: each new folder reads as the person's own, as a re-run tests it.
	restrictSetFolder = real
	s = &flowScript{folders: []string{setFresh(t)}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err = makeSet(rows, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range made {
		f, err := os.Open(m.Folder)
		if err != nil {
			t.Fatal(err)
		}
		fi, err := f.Stat()
		mine := err == nil && setChildMine(f, fi)
		f.Close()
		if !mine {
			t.Errorf("%s is not the person's own after it was made", m.Folder)
		}
	}

	// The plan names every child it did not look at.
	if runtime.GOOS == "windows" {
		return // a folder others can change is made with an access list here: the lab's step (MA-1503)
	}
	p2 := setFresh(t)
	ann := archiveHolding(t, filepath.Join(p2, rows[0].name), idOf(rows[0]))
	if err := os.Chmod(ann, 0o775); err != nil {
		t.Fatal(err)
	}
	setLinkTo(t, t.TempDir(), filepath.Join(p2, "Notes link"))
	plan, err := planSet(p2, rows, "darwin")
	if err != nil || plan.items[0].outcome != outNew || plan.items[0].folder != rows[0].name+" (2)" {
		t.Fatalf("a folder others can change is not looked at: %v %q", err, outcomesOf(plan))
	}
	text := setPlanText(plan)
	if !strings.Contains(text, "\nLeft as they are, not looked at (each a link, or a folder another account owns or can change): Notes link, "+rows[0].name+"\n") {
		t.Errorf("the plan does not name the folders it did not look at:\n%s", text)
	}
	if err := os.Remove(filepath.Join(p2, "Notes link")); err != nil {
		t.Fatal(err)
	}
	plan, _ = planSet(p2, rows, "darwin")
	if text := setPlanText(plan); !strings.Contains(text, "\nLeft as it is, not looked at (a link, or a folder another account owns or can change): "+rows[0].name+"\n") {
		t.Errorf("the plan's line for one folder:\n%s", text)
	}
}

// covers: MA-1496, MA-1494, S90
// A set never writes into a folder swapped after the plan (the adversarial pass's INS-02; the lead's
// fold). The plan reads each child through the parent's handle, but the export wrote by path, so a
// link put at an archive's name after the plan was followed and the whole mailbox written into its
// target. Each archive's folder is now proved again through the parent's handle (setChildSame: the
// same parent, a real directory, the same folder) right before its log opens and its export starts;
// one that is not the same fails that archive, naming its folder, and nothing is written. Positive
// twin first: a re-run with nothing swapped brings the archive up to date. Then an archive brought up
// to date swapped for a link after the plan, and a new folder swapped right after it is made: each
// fails naming its folder, the link's target stays empty (no log, no mail), and the real folder moved
// aside is as it was.
func TestASetNeverWritesIntoAFolderSwappedAfterThePlan(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	parent := setFresh(t)
	(&setScheduler{}).install(t)
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	if _, err := makeSet(rows, "darwin"); err != nil {
		t.Fatal(err)
	}
	ann := filepath.Join(parent, rows[0].name)

	// Positive twin: nothing swapped, the archive is brought up to date.
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	ch, err := chooseSet(rows, "darwin")
	if err != nil || ch.plan.items[0].outcome != outContinue {
		t.Fatalf("positive twin: the plan %q %v", outcomesOf(ch.plan), err)
	}
	made, err := runSet(ch, "darwin")
	if err != nil || made[0].Outcome != MadeFinished || made[0].Folder != ann {
		t.Fatalf("positive twin: %+v %v", made, err)
	}

	// An archive brought up to date, swapped for a link after the plan.
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	if ch, err = chooseSet(rows, "darwin"); err != nil || ch.plan.items[0].outcome != outContinue {
		t.Fatalf("the plan %q %v", outcomesOf(ch.plan), err)
	}
	drop := t.TempDir() // another account's folder, stood in
	moved := ann + " moved by another account"
	if err := os.Rename(ann, moved); err != nil {
		t.Fatal(err)
	}
	before := treeOf(t, moved)
	setLinkTo(t, drop, ann)
	made, err = runSet(ch, "darwin")
	code, msg := 0, ""
	if err == nil && made[0].Outcome == MadeFailed && made[1].Outcome == MadeFinished {
		code, msg = 1, made[0].Reason
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(ann, "is no longer the folder MailArchive checked", "nothing was written into it"),
		assure.NoSideEffect(func() bool {
			ents, _ := os.ReadDir(drop)
			return len(ents) == 0 && reflect.DeepEqual(treeOf(t, moved), before)
		}))
	if sum := s.summaryOf(t); !strings.Contains(sum, rows[0].name+" (brought up to date): Did not finish: "+made[0].Reason) {
		t.Errorf("the summary does not name the folder:\n%s", sum)
	}
	// Added 2026-11-25 (the re-verify of rounds A and B, RF-06): nothing was written into the folder
	// there now, so its line ends at the reason, never "The mail copied so far is kept.".
	if sum := s.summaryOf(t); !strings.Contains(sum, "\n"+rows[0].name+" (brought up to date): Did not finish: "+made[0].Reason+".\n") {
		t.Errorf("RF-06: the swapped archive's line says more than its reason:\n%s", sum)
	}

	// A new folder swapped right after it is made (in its restriction step).
	lf := filepath.Join(parent, rows[3].name)
	if err := os.RemoveAll(lf); err != nil {
		t.Fatal(err)
	}
	drop2 := t.TempDir()
	real := restrictSetFolder
	t.Cleanup(func() { restrictSetFolder = real })
	restrictSetFolder = func(p string) error {
		if p == lf {
			if err := os.Rename(lf, lf+" moved"); err != nil {
				t.Error(err)
			}
			setLinkTo(t, drop2, lf)
		}
		return nil
	}
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err = makeSet(rows, "darwin")
	var lfMade Made
	for _, m := range made {
		if m.Name == rows[3].name {
			lfMade = m
		}
	}
	code, msg = 0, ""
	if err == nil && lfMade.Outcome == MadeFailed && lfMade.New {
		code, msg = 1, lfMade.Reason
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(lf, "is no longer the folder MailArchive checked"),
		assure.NoSideEffect(func() bool {
			ents, _ := os.ReadDir(drop2)
			kept, _ := os.ReadDir(lf + " moved")
			return len(ents) == 0 && len(kept) == 0
		}))
	// Added 2026-11-25 (RF-06): nothing was written into the folder there now, so the line ends at its
	// reason, never "The mail copied so far is kept.".
	if sum := s.summaryOf(t); !strings.Contains(sum, "\n"+rows[3].name+": Did not finish: "+lfMade.Reason+".\n") {
		t.Errorf("RF-06: the swapped new folder's line:\n%s", sum)
	}

	// Added 2026-11-25 (RF-06): a new folder swapped in the instant after it is made, before its
	// identity is read (stood in through setMadeChildID, fault injection: no test can time it). It was
	// made, so its line never says "Not made, so nothing was copied:"; it says "Stopped before copying:
	// REASON." (the re-verifier's words). Nothing is written into it.
	restrictSetFolder = real
	if err := os.RemoveAll(lf); err != nil {
		t.Fatal(err)
	}
	realID := setMadeChildID
	t.Cleanup(func() { setMadeChildID = realID })
	setMadeChildID = func(parent, name string) os.FileInfo {
		if name == rows[3].name {
			return nil
		}
		return realID(parent, name)
	}
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err = makeSet(rows, "darwin")
	lfMade = Made{}
	for _, m := range made {
		if m.Name == rows[3].name {
			lfMade = m
		}
	}
	code, msg = 0, ""
	if err == nil && lfMade.Outcome == MadeFailed && !lfMade.New {
		code, msg = 1, s.summaryOf(t)
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(rows[3].name+": Stopped before copying: "+setChangedReason(lf)+"."),
		assure.Forbid(rows[3].name+": "+setNotMadeLead, rows[3].name+": Did not finish"),
		assure.NoSideEffect(func() bool {
			ents, err := os.ReadDir(lf)
			return err == nil && len(ents) == 0
		}))
}
