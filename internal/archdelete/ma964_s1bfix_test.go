package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-964, R36
func TestStep3AndStep9ReachTheDesktopsOwnSettingsFolder(t *testing.T) {
	// del-s1b-A3-04: a Desktop started with -config keeps its list of archives and the
	// alert files beside that file. Step 3 removes this archive's alert files there
	// as well as in MailArchive's settings folder, and step 9's default removes the
	// row from that list; another archive's are kept.
	dir := smallArchive(t, t.TempDir())
	other := smallArchive(t, t.TempDir())
	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	settings := t.TempDir()
	def, otherDef := schedule.DefaultNameFor(dir), schedule.DefaultNameFor(other)
	var mine []string
	for _, d := range []string{cfg, settings} {
		for _, n := range []string{def + alertStreakSuffix, def + syncAlertSuffix} {
			p := filepath.Join(d, n)
			writeFile(t, p, "x")
			t.Cleanup(func() { os.Remove(p) })
			mine = append(mine, p)
		}
	}
	theirs := filepath.Join(settings, otherDef+alertStreakSuffix)
	writeFile(t, theirs, "x")
	list := filepath.Join(settings, archindex.Name)
	if err := archindex.WriteFile(list, archindex.Index{Entries: []archindex.Entry{{Path: dir}, {Path: other}}}); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, dir)
	r.x.SettingsDir = settings
	r.x.ForgetRow = nil
	res := r.run(t, false)
	if res.Status != StatusComplete || !res.RowRemoved || !hasLine(res, "Removed from Your archives") {
		t.Fatalf("%s", res.Message())
	}
	for _, p := range mine {
		if exists(p) {
			t.Errorf("%s must be removed", p)
		}
	}
	if !exists(theirs) {
		t.Error("another archive's alert file is kept")
	}
	idx, err := archindex.ReadFile(list)
	if err != nil || len(idx.Entries) != 1 || idx.Entries[0].Path != other {
		t.Fatalf("the Desktop's own list keeps only the other archive: %+v %v", idx.Entries, err)
	}
}

// covers: MA-964, R36
func TestStep9RemovesTheRowWhateverItsSpellingAndNeverClaimsAMissingOne(t *testing.T) {
	// del-s1b-A3-06: the list names the folder in another case (a command line's
	// -out d:\mail against the row D:\Mail): the row is removed. A list that does not
	// name the folder is left as it was, and the result does not say "Removed from
	// Your archives".
	settings := t.TempDir()
	list := filepath.Join(settings, archindex.Name)
	gone := filepath.Join(t.TempDir(), "Mail")
	keep := filepath.Join(t.TempDir(), "Other")
	if err := archindex.WriteFile(list, archindex.Index{Entries: []archindex.Entry{{Path: strings.ToUpper(gone)}, {Path: keep}}}); err != nil {
		t.Fatal(err)
	}
	if err := forgetRowIn(settings, gone, "windows"); err != nil {
		t.Fatal(err)
	}
	if idx, _ := archindex.ReadFile(list); len(idx.Entries) != 1 || idx.Entries[0].Path != keep {
		t.Fatalf("entries %+v", idx.Entries)
	}
	if err := forgetRowIn(settings, gone, "windows"); !errors.Is(err, ErrRowNotListed) {
		t.Fatalf("a row no longer listed: %v", err)
	}

	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.SettingsDir = settings
	r.x.ForgetRow = nil
	res := r.run(t, false)
	if res.Status != StatusComplete || res.RowRemoved || hasLine(res, "Removed from Your archives") || !hasLine(res, msgRowNotListed) {
		t.Fatalf("%s", res.Message())
	}
	if idx, _ := archindex.ReadFile(list); len(idx.Entries) != 1 {
		t.Fatalf("entries %+v", idx.Entries)
	}
}

// covers: MA-964, R36
func TestAFolderKeptByWhatCouldNotBeRemovedNamesIt(t *testing.T) {
	// Conformance 4: a store folder that stayed busy past its retries keeps the
	// archive folder; the result names what kept it.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	faultOn(t, nil, func(rel string) error {
		if rel == "Mailbox/Inbox" {
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, false)
	if res.Status != StatusComplete || res.FolderGone ||
		!strings.HasSuffix(res.Lines[0], "The folder is kept: it still holds 1 item that could not be removed: Mailbox.") {
		t.Fatalf("%s", res.Message())
	}
}

// covers: MA-964, R36
func TestTheResultListsTheStepsDone(t *testing.T) {
	// Conformance 5: Result.Steps holds the steps done, in the words "Steps done"
	// uses, for slices 3 and 4.
	r := newRig(t, smallArchive(t, t.TempDir()))
	res := r.run(t, false)
	if res.Status != StatusComplete || len(res.Steps) == 0 || res.Steps[0] != "archive locked" ||
		!contains(res.Steps, "their settings files removed") || !contains(res.Steps, "removed from Your archives") {
		t.Fatalf("steps %q", res.Steps)
	}
	r = newRig(t, smallArchive(t, t.TempDir()))
	r.x.BeforeStep = stopBefore(4)
	res = r.run(t, false)
	if !strings.Contains(res.Lines[0], "Steps done: "+strings.Join(res.Steps, ", ")+".") {
		t.Fatalf("steps %q, %s", res.Steps, res.Lines[0])
	}
}

// covers: MA-970, R36
func TestALockOnThePageThatFailsRefusesWithItsWords(t *testing.T) {
	// Conformance 7: step 1 (b) fails without a panic.
	dir := encryptedClean(t)
	r := encRig(t, dir)
	r.keys.err = errors.New("the registry is busy")
	before := treeWithoutLock(t, dir)
	res := r.run(t, false)
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1),
		assure.Names("MailArchive could not lock this archive on this page (the registry is busy), so nothing was deleted. Click Delete again.", lockedLine),
		assure.NoSideEffect(func() bool {
			return sameTree(before, treeWithoutLock(t, dir)) && len(r.sched.removed) == 0 && len(r.keys.marked) == 0
		}))
}

// covers: MA-970, R36
func TestAScratchFolderThatCannotBeFoundRefusesWithItsWords(t *testing.T) {
	// Conformance 7: step 1 (c) cannot tell where the scratch folder is.
	dir := encryptedClean(t)
	r := encRig(t, dir)
	r.x.ScratchDir = func(string, string) (string, error) { return "", errors.New("no profile folder") }
	before := treeWithoutLock(t, dir)
	res := r.run(t, false)
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1),
		assure.Names("MailArchive's working folder for this archive cannot be found (no profile folder), so nothing was deleted. Click Delete again.", lockedLine),
		assure.NoSideEffect(func() bool { return sameTree(before, treeWithoutLock(t, dir)) && len(r.sched.removed) == 0 }))
}

// covers: MA-964, R36
func TestAFolderThatCannotBeOpenedAtStep6StopsWithItsWords(t *testing.T) {
	// Conformance 7: the folder is gone from its path when step 6 opens it (the drive
	// went away): not fully deleted, the row unfinished.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.BeforeStep = func(step int) error {
		if step == 4 {
			if err := os.Rename(dir, dir+"-away"); err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("Windows does not rename a folder holding an open lock: %v", err)
				}
				t.Fatal(err)
			}
		}
		return nil
	}
	res := r.run(t, false)
	if res.Status != StatusPartial || !res.Unfinished || !strings.HasPrefix(res.Lines[0], "Not fully deleted: the archive's folder could not be opened (") ||
		!strings.HasSuffix(res.Lines[0], "Reconnect the drive, then click Delete again.") {
		t.Fatalf("%s", res.Message())
	}
	if !exists(filepath.Join(dir+"-away", manifestName)) {
		t.Fatal("nothing of the folder is removed")
	}
}

// covers: MA-966, R36
func TestAFolderRemovedLineWithItsParentUnreachableIsRefused(t *testing.T) {
	// Conformance 7 (§5.4): the log has the folder's `folder removed` line, but the
	// folder's parent cannot be reached either (the drive unplugged): "Reconnect".
	parent := filepath.Join(t.TempDir(), "Drive")
	dir := filepath.Join(parent, "Archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, dir)
	crashAfterFolderRemoved(t, r.x.Log, dir, map[string]any{"kind": "plain"})
	// Positive twin first: the parent reachable, the re-run continues.
	if p := r.plan(t); p.Rerun != RerunFolderGone {
		t.Fatalf("rerun %v", p.Rerun)
	}
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	rc, msg, c := refusalOf(t, dir, r.x.Deps)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(msgUnreachable),
		assure.NoSideEffect(func() bool { return len(r.rows) == 0 && len(r.sched.removed) == 0 }))
	if c != CaseUnreachable {
		t.Fatalf("case %s", c)
	}
}
