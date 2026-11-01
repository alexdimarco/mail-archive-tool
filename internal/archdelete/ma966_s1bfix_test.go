package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// The tests of slice 1b's adversarial-pass fixes to deletions.log and the unfinished
// delete (§5.3 steps 0, 7, 8, 10; §5.4).

// stopBefore is a BeforeStep that stops the delete right before step n, as the
// command line's re-check does when a Desktop appears.
func stopBefore(n int) func(int) error {
	return func(step int) error {
		if step == n {
			return errors.New("A MailArchive Desktop is running")
		}
		return nil
	}
}

// swapFolder moves dir aside (kept, so its identity cannot be reused) and makes a
// new empty folder at its path: another drive under the same letter, or the empty
// mount point a drive leaves when it is unmounted.
func swapFolder(t *testing.T, dir string) {
	t.Helper()
	if err := os.Rename(dir, dir+"-elsewhere"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-966, R36
func TestAnotherFolderAtTheSamePathIsNotTheUnfinishedDeletes(t *testing.T) {
	// del-s1b-A1-01: a delete stopped before any of the folder was removed; then
	// another folder is found at the same path (another USB drive under the same
	// letter) holding names a no-manifest re-run would remove. It is not this
	// delete's: no "Delete not finished", and Delete is refused as for any folder
	// that holds no archive, removing nothing.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.BeforeStep = stopBefore(4)
	if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished {
		t.Fatalf("%s", res.Message())
	}
	r.x.BeforeStep = nil
	// Positive twin first: the very folder, its manifest gone, is the delete's own.
	man := filepath.Join(dir, manifestName)
	saved, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(man); err != nil {
		t.Fatal(err)
	}
	if p := r.plan(t); p.Rerun != RerunNoAnchor {
		t.Fatalf("the delete's own folder: rerun %v", p.Rerun)
	}
	writeFile(t, man, string(saved))

	swapFolder(t, dir)
	for _, n := range []string{"README.txt", "index.html", ".mailarchive-lastrun.json"} {
		writeFile(t, filepath.Join(dir, n), "the person's own")
	}
	before := tree(t, dir)
	if u, err := UnfinishedDelete(r.x.Log, dir, ""); err != nil || u != nil {
		t.Fatalf("another folder at the path is not the unfinished delete's: %+v %v", u, err)
	}
	rc, msg, c := refusalOf(t, dir, r.x.Deps)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(msgNotArchive),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) && len(r.rows) == 0 }))
	if c != CaseNotArchive {
		t.Fatalf("case %s", c)
	}
	// The re-run's planner checks it again itself: a started line that records
	// another folder's identity, or none, does not plan a re-run of this folder.
	orig := &Refusal{Case: CaseNotArchive, Message: msgNotArchive}
	old, ok := folderIdentity(dir + "-elsewhere")
	if !ok {
		t.Fatal("no identity")
	}
	for _, id := range []Identity{old, {}} {
		if p, err := planUnfinished(dir, r.x.Deps, &Unfinished{Folder: dir, Kind: Plain, Identity: id}, orig); err != orig {
			t.Fatalf("started line identity %v: plan %+v, %v", id, p, err)
		}
	}
}

// covers: MA-966, R36
func TestAnEmptyMountPointIsNotAnEncryptedArchivesUnfinishedDelete(t *testing.T) {
	// del-s1b-A1-02: an encrypted delete stopped before any of its files were removed;
	// the drive is unmounted, leaving its mount point, an empty folder, at the path.
	// Delete must not plan a re-run there: that re-run would remove the empty folder
	// and make the keyring forget a vault still on the unplugged drive.
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	r.x.BeforeStep = stopBefore(4)
	if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished {
		t.Fatalf("%s", res.Message())
	}
	r.x.BeforeStep = nil
	// Positive twin first: the row is unfinished while the archive's own folder is there.
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u == nil {
		t.Fatal("the stopped delete is unfinished")
	}
	swapFolder(t, dir)
	rc, msg, _ := refusalOf(t, dir, r.x.Deps)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(msgNotArchive),
		assure.NoSideEffect(func() bool { return exists(dir) && len(r.kr.asked) == 0 && len(r.rows) == 0 }))
}

// covers: MA-966, R36
func TestAFolderMadeAgainAtThePathIsANewArchiveNotAnUnfinishedDelete(t *testing.T) {
	// del-s1b-A1-06, A2-04, A3-03: a delete stopped after its `folder removed` line,
	// then a new archive made at the same path. The new archive's row is not "Delete
	// not finished", and Delete of it is an ordinary delete. The started line here
	// records no identity, so the `folder removed` rule alone tells them apart.
	dir := filepath.Join(t.TempDir(), "Archive")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, dir)
	crashAfterFolderRemoved(t, r.x.Log, dir, map[string]any{"kind": "plain"})
	// Positive twin first: with the folder still gone, the delete is unfinished.
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u == nil || !u.FolderRemoved {
		t.Fatalf("unfinished %+v", u)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	smallArchive(t, dir)
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u != nil {
		t.Fatalf("a new archive at the path is not the old delete's: %+v", u)
	}
	if p := r.plan(t); p.Rerun != RerunNone {
		t.Fatalf("rerun %v", p.Rerun)
	}

	// With the identity recorded (this build's started line): a delete stopped in
	// step 4, the folder replaced by a new archive at the same path.
	d2 := smallArchive(t, t.TempDir())
	r2 := newRig(t, d2)
	r2.x.BeforeStep = stopBefore(4)
	if res := r2.run(t, false); res.Status != StatusPartial {
		t.Fatalf("%s", res.Message())
	}
	if u, _ := UnfinishedDelete(r2.x.Log, d2, ""); u == nil || u.Identity == (Identity{}) {
		t.Fatalf("the started line records the folder's identity: %+v", u)
	}
	swapFolder(t, d2)
	smallArchive(t, d2)
	if u, _ := UnfinishedDelete(r2.x.Log, d2, ""); u != nil {
		t.Fatalf("a new archive at the path is not the old delete's: %+v", u)
	}
}

// covers: MA-966, R36
func TestOverlappingDeletesOfOneFolderKeepTheirOwnState(t *testing.T) {
	// del-s1b-A2-02, A3-02: a command-line delete holds the archive lock; a Delete
	// clicked meanwhile is refused (its started line and its `finished refused`
	// line); then the command line stops before step 8 because a Desktop appeared.
	// The row is "Delete not finished" with the folder removed, and Delete again
	// finishes it.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	var second *Result
	r.x.BeforeStep = func(step int) error {
		switch step {
		case 4:
			x2 := r.x
			x2.BeforeStep = nil
			p2 := mustPlan(t, dir, x2.Deps)
			second = Execute(p2, p2.Name, false, x2)
		case 8:
			return errors.New("A MailArchive Desktop is running")
		}
		return nil
	}
	res := r.run(t, false)
	if second == nil || second.Refusal == nil || second.Refusal.Case != CaseBusy {
		t.Fatalf("the overlapping delete is refused as busy: %+v", second)
	}
	if res.Status != StatusPartial || !res.Unfinished || exists(dir) {
		t.Fatalf("%s", res.Message())
	}
	if ev := strings.Join(events(logLines(t, r.x.Log)), ","); ev != "started,started,finished refused,folder removed,finished partial" {
		t.Fatalf("log %s", ev)
	}
	u, err := UnfinishedDelete(r.x.Log, dir, "")
	if err != nil || u == nil || !u.FolderRemoved {
		t.Fatalf("the stopped delete keeps its own state: %+v %v", u, err)
	}
	r.x.BeforeStep = nil
	p := r.plan(t)
	if p.Rerun != RerunFolderGone {
		t.Fatalf("rerun %v", p.Rerun)
	}
	if res := Execute(p, p.Name, false, r.x); res.Status != StatusComplete || len(r.rows) != 1 {
		t.Fatalf("Delete again: %s", res.Message())
	}
}

// covers: MA-966, R36
func TestEachLineBelongsToItsOwnDeleteByItsRunID(t *testing.T) {
	// del-s1b-A2-02: two deletes of one folder interleaved line by line (B started
	// while A ran, A's folder line written before B's refusal). Each line updates its
	// own delete: A is left unfinished with its folder removed.
	dir := filepath.Join(t.TempDir(), "Archive")
	log := filepath.Join(t.TempDir(), LogName)
	for _, l := range []map[string]any{
		{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "a", "folder": dir, "kind": "plain"},
		{"at": "2026-11-01T10:00:01Z", "event": "started", "run": "b", "folder": dir, "kind": "plain"},
		{"at": "2026-11-01T10:00:02Z", "event": "folder removed", "run": "a", "folder": dir},
		{"at": "2026-11-01T10:00:03Z", "event": "finished", "run": "b", "folder": dir, "outcome": "refused"},
		{"at": "2026-11-01T10:00:04Z", "event": "finished", "run": "a", "folder": dir, "outcome": "partial", "unfinished": true},
	} {
		appendRawLog(t, log, l)
	}
	u, err := UnfinishedDelete(log, dir, "")
	if err != nil || u == nil || !u.FolderRemoved {
		t.Fatalf("A's own state: %+v %v", u, err)
	}
}

// covers: MA-966, R36
func TestARefusedDeleteAgainKeepsTheRowUnfinished(t *testing.T) {
	// Conformance 1: Delete again refused (a backup is running) changed nothing, so
	// the earlier stop's "Delete not finished" stands.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.BeforeStep = stopBefore(4)
	if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished {
		t.Fatalf("%s", res.Message())
	}
	r.x.BeforeStep = nil
	backup, err := lockfile.AcquireAs(filepath.Join(dir, lockName), "export")
	if err != nil {
		t.Fatal(err)
	}
	res := r.run(t, false)
	backup.Release()
	if res.Refusal == nil || res.Refusal.Case != CaseBusy {
		t.Fatalf("%s", res.Message())
	}
	if u, err := UnfinishedDelete(r.x.Log, dir, ""); err != nil || u == nil {
		t.Fatalf("a refused Delete again keeps the row unfinished: %+v %v", u, err)
	}
	if res := r.run(t, false); res.Status != StatusComplete {
		t.Fatalf("Delete again: %s", res.Message())
	}
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u != nil {
		t.Fatalf("finished: %+v", u)
	}
}

// covers: MA-966, R36
func TestTheDeletesLaterLinesAreItsOwnWhenTheRowIsSpelledOtherwise(t *testing.T) {
	// del-s1b-A2-03: the row names the folder by another spelling than the plan (a
	// mapped drive against its UNC path, an 8.3 name). A stop after the folder was
	// removed is read back under the row's spelling with its `folder removed` line,
	// so Delete again continues as the folder-gone re-run.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	row := filepath.Join(filepath.Dir(dir), "Row spelling")
	r.x.Row = row
	r.x.BeforeStep = stopBefore(8)
	if res := r.run(t, false); res.Status != StatusPartial || exists(dir) {
		t.Fatalf("%s", res.Message())
	}
	u, err := UnfinishedDelete(r.x.Log, row, "")
	if err != nil || u == nil || !u.FolderRemoved {
		t.Fatalf("under the row's spelling: %+v %v", u, err)
	}
	r.x.BeforeStep = nil
	p := mustPlan(t, row, r.x.Deps)
	if p.Rerun != RerunFolderGone {
		t.Fatalf("rerun %v", p.Rerun)
	}
	if res := Execute(p, p.Name, false, r.x); res.Status != StatusComplete || len(r.rows) != 1 || r.rows[0] != row {
		t.Fatalf("Delete again: %s (rows %q)", res.Message(), r.rows)
	}
	if u, _ := UnfinishedDelete(r.x.Log, row, ""); u != nil {
		t.Fatalf("finished under the row's spelling: %+v", u)
	}
}

// covers: MA-966, MA-968, R36
func TestAStopAfterStep8KnowsTheKeyIsForgottenOnDeleteAgain(t *testing.T) {
	// del-s1b-A2-01: step 8 forgot the record, then the delete stopped before step 9.
	// Delete again does not ask the keyring again (it no longer knows the spelling)
	// and does not say this computer still remembers the key: it removes the row.
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	r.x.BeforeStep = stopBefore(9)
	res := r.run(t, false)
	if res.Status != StatusPartial || res.KeyForgotten != "forgotten" || len(r.rows) != 0 || exists(dir) {
		t.Fatalf("%s", res.Message())
	}
	if ev := strings.Join(events(logLines(t, r.x.Log)), ","); ev != "started,folder kept,key forgotten,folder removed,finished partial" &&
		ev != "started,folder removed,key forgotten,finished partial" {
		t.Fatalf("log %s", ev)
	}
	r.kr.planErr = errors.New("is not a folder this computer recorded")
	// A re-run that itself stops before step 8 carries the forgotten key forward.
	r.x.BeforeStep = stopBefore(8)
	if res := r.run(t, false); res.Status != StatusPartial {
		t.Fatalf("%s", res.Message())
	}
	r.x.BeforeStep = nil
	p := r.plan(t)
	if p.Rerun != RerunFolderGone || !p.KeyForgottenBefore {
		t.Fatalf("rerun %v key forgotten before %v", p.Rerun, p.KeyForgottenBefore)
	}
	res = Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || res.KeySkipped || hasLine(res, msgKeySkipped) || len(r.rows) != 1 || len(r.kr.asked) != 1 {
		t.Fatalf("Delete again: %s (asked %q rows %q)", res.Message(), r.kr.asked, r.rows)
	}
}

// covers: MA-968, R36
func TestStep8ForgetsOnlyTheArchivesOwnRecord(t *testing.T) {
	// del-s1b-A1-05: the record found at the spelling must carry the plan's archive ID.
	dir := encryptedClean(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	// Positive twin first: the archive's own record is forgotten.
	if res := r.run(t, false); res.KeyForgotten != "forgotten" || len(r.kr.forgot) != 1 {
		t.Fatalf("%s", res.Message())
	}
	d2 := encryptedClean(t)
	r2 := newRig(t, d2)
	r2.x.Deps.Record = recordedAt(d2)
	r2.kr.plan.ID = "fedcba9876543210fedcba9876543210" // another archive recorded at this spelling
	res := r2.run(t, false)
	if !res.KeySkipped || !hasLine(res, msgKeySkipped) || len(r2.kr.forgot) != 0 || len(r2.rows) != 0 {
		t.Fatalf("another archive's record is never forgotten: %s (forgot %v)", res.Message(), r2.kr.forgot)
	}
}

// covers: MA-965, R36
func TestATornLastLineDoesNotSwallowTheNextLine(t *testing.T) {
	// del-s1b-A2-05: a power cut left the log's last line torn, with no line break.
	// The next delete's started line is still read back, so its stop marks the row.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	writeFile(t, r.x.Log, `{"at":"2026-11-01T09:00:00Z","event":"started","folder":"/elsewhere"}`+"\n"+`{"at":"2026-11-01T09:00:01Z","ev`)
	if err := os.Chmod(r.x.Log, 0o600); err != nil {
		t.Fatal(err)
	}
	r.x.BeforeStep = stopBefore(4)
	if res := r.run(t, false); res.Status != StatusPartial {
		t.Fatalf("%s", res.Message())
	}
	lines, err := readLog(r.x.Log)
	if err != nil {
		t.Fatal(err)
	}
	var ev []string
	for _, l := range lines {
		ev = append(ev, l.Event+" "+l.Outcome)
	}
	if strings.Join(ev, ",") != "started ,started ,finished partial" {
		t.Fatalf("lines read back: %q", ev)
	}
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u == nil {
		t.Fatal("the stopped delete marks its row")
	}
}

// covers: MA-966, R36
func TestADeletionLogThatCannotBeReadIsNamedInTheRefusal(t *testing.T) {
	// del-s1b-A3-10: on macOS and Linux a log readable by others (restored from a
	// backup) is refused by the secure read. Plan says so instead of silently
	// refusing as if no delete were unfinished.
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no mode bits for the secure read to refuse")
	}
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.BeforeStep = stopBefore(8)
	if res := r.run(t, false); res.Status != StatusPartial || exists(dir) {
		t.Fatalf("%s", res.Message())
	}
	// Positive twin first: readable, Delete again is the folder-gone re-run.
	if p := r.plan(t); p.Rerun != RerunFolderGone {
		t.Fatalf("rerun %v", p.Rerun)
	}
	if err := os.Chmod(r.x.Log, 0o644); err != nil {
		t.Fatal(err)
	}
	rc, msg, c := refusalOf(t, dir, r.x.Deps)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(msgUnreachable, "MailArchive's deletion log could not be read", "chmod 600"),
		assure.NoSideEffect(func() bool { return len(r.rows) == 0 && !exists(dir) }))
	if c != CaseUnreachable {
		t.Fatalf("case %s", c)
	}
}

// covers: MA-966, R36
func TestANoManifestReRunSaysWhatIsLeftAndHowToGoOn(t *testing.T) {
	// del-s1b-A2-08: never "0 files". Only the lock file left: Delete again retries
	// it (deleting the folder by hand would lose its `folder removed` line). Only
	// empty folders left: they are named as folders.
	noAnchor := func(t *testing.T) *rig {
		t.Helper()
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": dir, "row": dir, "kind": "plain", "identity": idOf(t, dir)})
		if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(stemA+".html"))); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "search.db"), "index")
		return r
	}
	r := noAnchor(t)
	if err := os.RemoveAll(filepath.Join(r.dir, "Mailbox")); err != nil {
		t.Fatal(err)
	}
	faultOn(t, nil, func(rel string) error {
		if rel == lockName {
			return inUseErr(rel)
		}
		return nil
	})
	p := r.plan(t)
	res := Execute(p, p.Name, false, r.x)
	if p.Rerun != RerunNoAnchor || res.Status != StatusPartial || strings.Contains(res.Message(), "0 files") ||
		res.Lines[0] != "Not fully deleted: one MailArchive lock file (.mailarchive.lock) could not be removed. Click Delete again; if it stays, delete that file by hand (not the folder), then click Delete again." {
		t.Fatalf("%s", res.Message())
	}

	r = noAnchor(t) // Mailbox/Inbox is left, empty
	p = r.plan(t)
	res = Execute(p, p.Name, false, r.x)
	if res.Status != StatusPartial || strings.Contains(res.Message(), "0 files") ||
		res.Lines[0] != "Not fully deleted: the folder still holds 2 folders the delete can no longer identify. Delete them by hand, or use Remove from list." {
		t.Fatalf("%s", res.Message())
	}
}
