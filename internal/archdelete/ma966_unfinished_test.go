package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// crashAfterFolderRemoved leaves deletions.log as a delete that crashed after its
// `folder removed` line: the started line (with extra fields), the folder gone,
// the folder removed line, no finished line.
func crashAfterFolderRemoved(t *testing.T, log, folder string, started map[string]any) {
	t.Helper()
	line := map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": folder, "row": folder}
	for k, v := range started {
		line[k] = v
	}
	appendRawLog(t, log, line)
	if err := os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	appendRawLog(t, log, map[string]any{"at": "2026-11-01T10:00:01Z", "event": "folder removed", "folder": folder})
}

// covers: MA-966, R36
func TestAPlainDeleteThatCrashedAfterFolderRemovedFinishesOnDeleteAgain(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	// Positive twin first: without a log line, a gone folder is unreachable.
	if _, msg, c := refusalOf(t, filepath.Join(t.TempDir(), "never"), r.x.Deps); c != CaseUnreachable {
		t.Fatalf("%s", msg)
	}
	crashAfterFolderRemoved(t, r.x.Log, dir, map[string]any{"kind": "plain"})
	u, err := UnfinishedDelete(r.x.Log, dir, "")
	if err != nil || u == nil || !u.FolderRemoved {
		t.Fatalf("unfinished %+v %v", u, err)
	}
	p := r.plan(t)
	if p.Rerun != RerunFolderGone {
		t.Fatalf("rerun %v", p.Rerun)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || len(r.rows) != 1 || !hasLine(res, "skipped") {
		t.Fatalf("%s (rows %q)", res.Message(), r.rows)
	}
	if strings.Contains(strings.ToLower(res.Message()), "key") || len(r.kr.asked) != 0 {
		t.Fatalf("a plain archive's re-run never mentions a key: %s", res.Message())
	}
	if exists(dir) {
		t.Fatal("the folder is not re-created")
	}
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u != nil {
		t.Fatal("the re-run finished the delete")
	}
}

// covers: MA-966, R36
func TestAnEncryptedDeleteReachedThroughAnotherSpellingFinishesWithTheStartedLinesRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Vault")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, dir)
	other := filepath.Join(t.TempDir(), "link-to-vault")
	crashAfterFolderRemoved(t, r.x.Log, dir, map[string]any{"kind": "encrypted", "record_id": testArchiveID, "record_spelling": other})
	r.kr.folder = ""
	p := r.plan(t)
	if p.Rerun != RerunFolderGone || !p.Record.Recorded || p.Record.Spelling != other {
		t.Fatalf("rerun %v record %+v", p.Rerun, p.Record)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || res.KeyForgotten != "forgotten" || len(r.rows) != 1 {
		t.Fatalf("%s", res.Message())
	}
	if len(r.kr.asked) != 1 || r.kr.asked[0] != other || len(r.kr.forgot) != 1 || !r.kr.forgot[0] {
		t.Fatalf("step 8 must forget the record and key at the started line's spelling: asked %q forgot %v", r.kr.asked, r.kr.forgot)
	}
	// Neither an ID and Spelling nor "not recorded": step 8 is skipped and the row kept.
	d2 := filepath.Join(t.TempDir(), "Vault2")
	if err := os.Mkdir(d2, 0o755); err != nil {
		t.Fatal(err)
	}
	r2 := newRig(t, d2)
	crashAfterFolderRemoved(t, r2.x.Log, d2, map[string]any{"kind": "encrypted"})
	p = r2.plan(t)
	res = Execute(p, p.Name, false, r2.x)
	if !res.KeySkipped || !hasLine(res, msgKeySkipped) || len(r2.rows) != 0 || len(r2.kr.asked) != 0 {
		t.Fatalf("%s", res.Message())
	}
}

// covers: MA-966, R36
func TestACrashInStep6ThenTheDriveUnpluggedIsRefusedAndTheKeyringUntouched(t *testing.T) {
	dir := encryptedClean(t)
	r := newRig(t, dir)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": dir, "row": dir,
		"kind": "encrypted", "record_id": testArchiveID, "record_spelling": dir})
	// The drive unplugged: the folder is not reachable, and the log has no folder
	// removed line.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	u, _ := UnfinishedDelete(r.x.Log, dir, "")
	if u == nil || u.FolderRemoved {
		t.Fatalf("unfinished %+v", u)
	}
	rc, msg, c := refusalOf(t, dir, r.x.Deps)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("Reconnect the drive or folder first"),
		assure.NoSideEffect(func() bool { return len(r.kr.asked) == 0 && len(r.rows) == 0 }))
	if c != CaseUnreachable {
		t.Fatalf("case %s", c)
	}
}

// covers: MA-966, R36
func TestAFolderWithNoManifestAndMailLeftIsNeverComplete(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": dir, "row": dir, "kind": "plain"})
	// The crash left mail and an index beside a removed manifest.
	if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "search.db"), "index")
	writeFile(t, filepath.Join(dir, ".mailarchive-lastrun.json"), "{}")
	// Positive twin first: with no unfinished delete the folder is not an archive.
	{
		deps := r.x.Deps
		deps.Log = ""
		if _, _, c := refusalOf(t, dir, deps); c != CaseNotArchive {
			t.Fatalf("case %s", c)
		}
	}
	p := r.plan(t)
	if p.Rerun != RerunNoAnchor || !contains(removeRels(p), "search.db") || contains(removeRels(p), stemA+".html") {
		t.Fatalf("rerun %v remove %q", p.Rerun, removeRels(p))
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusPartial || res.ExitCode() != 3 || !strings.Contains(res.Lines[0], "the folder still holds 1 file the delete can no longer identify") {
		t.Fatalf("%s", res.Message())
	}
	if exists(filepath.Join(dir, "search.db")) || !exists(filepath.Join(dir, filepath.FromSlash(stemA+".html"))) {
		t.Fatal("what §4 can still name is removed; the rest is kept")
	}
	if ev := events(logLines(t, r.x.Log)); ev[len(ev)-1] != "finished partial" {
		t.Fatalf("log %q", ev)
	}
}

// covers: MA-966, R36
func TestAStopBeforeStep8LeavesTheRowUnfinishedAndDeleteAgainFinishes(t *testing.T) {
	// The command line's re-check finds a Desktop before step 8, after the folder
	// was removed: not fully deleted, naming the steps done, the row unfinished; the
	// Desktop's Delete again finishes the row [R4:NF-R4-CLI-STOP-DEADEND].
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	desktop := errors.New("A MailArchive Desktop is running")
	r.x.BeforeStep = func(step int) error {
		if step == 8 {
			return desktop
		}
		return nil
	}
	res := r.run(t, false)
	if res.Status != StatusPartial || !res.Unfinished || exists(dir) || len(r.rows) != 0 ||
		!strings.HasPrefix(res.Lines[0], "A MailArchive Desktop is running. The delete stopped before step 8. Steps done: archive locked") {
		t.Fatalf("%s", res.Message())
	}
	r.x.BeforeStep = nil
	p := r.plan(t)
	if p.Rerun != RerunFolderGone {
		t.Fatalf("rerun %v", p.Rerun)
	}
	if res := Execute(p, p.Name, false, r.x); res.Status != StatusComplete || len(r.rows) != 1 {
		t.Fatalf("Delete again: %s", res.Message())
	}

	// Before step 4: nothing of the folder was removed; Delete again deletes it.
	d2 := smallArchive(t, t.TempDir())
	r = newRig(t, d2)
	r.x.BeforeStep = func(step int) error {
		if step == 4 {
			return desktop
		}
		return nil
	}
	res = r.run(t, false)
	if res.Status != StatusPartial || !res.Unfinished || !IsArchive(d2) || r.ptr.saves != 0 {
		t.Fatalf("%s", res.Message())
	}
	r.x.BeforeStep = nil
	if res := r.run(t, false); res.Status != StatusComplete {
		t.Fatalf("Delete again: %s", res.Message())
	}
}

// covers: MA-966, R36
func TestANoManifestFolderLeftEmptyByItsReRunIsFinished(t *testing.T) {
	// For the operator (§5.4 "never complete"): a folder that held only names §4 can
	// still name is removed by the re-run, and with the folder gone nothing is left
	// that the delete cannot identify, so the re-run finishes as the folder-gone
	// re-run does (keyring, row) rather than asking for Delete again.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": dir, "row": dir, "kind": "plain"})
	if err := os.RemoveAll(filepath.Join(dir, "Mailbox")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "search.db"), "index")
	p := r.plan(t)
	res := Execute(p, p.Name, false, r.x)
	if p.Rerun != RerunNoAnchor || res.Status != StatusComplete || exists(dir) || len(r.rows) != 1 {
		t.Fatalf("rerun %v: %s", p.Rerun, res.Message())
	}
}

// covers: MA-966, R36
func TestAStopAfterFolderRemovedThenDeleteAgainFinishes(t *testing.T) {
	// A command-line delete stopped after the folder was removed (a Desktop appeared
	// before step 8): its finished partial line marks the row unfinished, and Delete
	// again finishes the keyring and the row.
	dir := filepath.Join(t.TempDir(), "Vault")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, dir)
	crashAfterFolderRemoved(t, r.x.Log, dir, map[string]any{"kind": "encrypted", "record_id": testArchiveID, "record_spelling": dir})
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:02Z", "event": "finished", "folder": dir, "outcome": "partial", "unfinished": true})
	u, _ := UnfinishedDelete(r.x.Log, dir, "")
	if u == nil || !u.FolderRemoved {
		t.Fatalf("a finished partial written after step 2 began is unfinished: %+v", u)
	}
	p := r.plan(t)
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || res.KeyForgotten != "forgotten" || len(r.rows) != 1 {
		t.Fatalf("%s", res.Message())
	}
	// And a re-run that itself crashes keeps the folder-gone state for the next.
	d2 := filepath.Join(t.TempDir(), "Plain")
	if err := os.Mkdir(d2, 0o755); err != nil {
		t.Fatal(err)
	}
	r2 := newRig(t, d2)
	crashAfterFolderRemoved(t, r2.x.Log, d2, map[string]any{"kind": "plain"})
	r2.sched.state[schedule.DefaultNameFor(d2)] = schedule.Installed
	r2.sched.panicOn = schedule.DefaultNameFor(d2)
	p = r2.plan(t)
	if res := Execute(p, p.Name, false, r2.x); res.Status != StatusPartial {
		t.Fatalf("%s", res.Message())
	}
	if u, _ := UnfinishedDelete(r2.x.Log, d2, ""); u == nil || !u.FolderRemoved {
		t.Fatalf("the re-run's own started line keeps the folder removed state: %+v", u)
	}
}
