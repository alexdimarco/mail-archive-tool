package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
)

// treeWithoutLock is tree without the archive lock file, which step 1 creates when
// it is missing and a stop leaves in place.
func treeWithoutLock(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := tree(t, dir)
	delete(m, lockName)
	return m
}

// covers: MA-964, R36
func TestDeleteBesideThePersonsFilesCompletesAndKeepsThem(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	persons := map[string]string{}
	for _, rel := range []string{"taxes.pdf", "Photos/beach.jpg", "Mailbox/Inbox/notes.txt"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		persons[rel] = string(b)
	}
	r := newRig(t, dir)
	p := r.plan(t)
	def := schedule.DefaultNameFor(dir)
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || res.ExitCode() != 0 {
		t.Fatalf("status %q exit %d:\n%s", res.Status, res.ExitCode(), res.Message())
	}
	for _, it := range assure.Reached(t, p.Remove, "the plan's names") {
		if exists(filepath.Join(dir, filepath.FromSlash(it.Rel))) {
			t.Errorf("%s was not removed", it.Rel)
		}
	}
	for rel, content := range persons {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || string(b) != content {
			t.Errorf("the person's %s changed: %v", rel, err)
		}
	}
	if exists(filepath.Join(dir, lockName)) || exists(filepath.Join(dir, "Mailbox", "Sent")) {
		t.Fatal("the lock file and the emptied store folders must go")
	}
	first := res.Lines[0]
	// The kept files named (MA-1038, the 1.5.3 re-walk's N7).
	if !strings.HasPrefix(first, "Deleted. ") || !strings.Contains(first, "The folder is kept: it holds 3 files that are not the archive's: Mailbox/Inbox/notes.txt, Photos, taxes.pdf.") {
		t.Fatalf("first line: %q", first)
	}
	for _, want := range []string{"Stopped the scheduled backup " + def, "Removed from Your archives", "What Delete does not reach: " + DoesNotReach, "Recorded in deletions.log"} {
		if !hasLine(res, want) {
			t.Errorf("missing line %q in:\n%s", want, res.Message())
		}
	}
	if len(r.rows) != 1 || r.rows[0] != dir {
		t.Fatalf("row removed: %q", r.rows)
	}
	if r.ptr.p != (Pointers{}) {
		t.Fatalf("Out and last-archive.txt name the folder and must be cleared: %+v", r.ptr.p)
	}
	if strings.Join(r.sched.removed, ",") != def {
		t.Fatalf("stopped: %q", r.sched.removed)
	}
}

// covers: MA-964, R36
func TestTheTypedNameIsComparedAfterNFCWithNoCaseFolding(t *testing.T) {
	dir := smallArchive(t, filepath.Join(t.TempDir(), "Caf\u00e9 mail"))
	r := newRig(t, dir)
	p := r.plan(t)
	// Positive twin first: the name typed decomposed (NFD) is the same name.
	if !NameMatches("Cafe\u0301 mail", p.Name) || !NameMatches(p.Name, p.Name) {
		t.Fatalf("the NFC-equal name must match %q", p.Name)
	}
	before := tree(t, dir)
	res := Execute(p, "CAF\u00c9 MAIL", false, r.x)
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1),
		assure.Names("does not match", p.Name, "nothing was deleted"),
		assure.NoSideEffect(func() bool {
			return sameTree(before, tree(t, dir)) && !exists(r.x.Log) && len(r.sched.removed) == 0 && r.ptr.saves == 0
		}))
	if res.Refusal == nil || res.Refusal.Case != CaseName || res.Status != StatusRefused || res.Record != nil {
		t.Fatalf("refusal %+v status %q", res.Refusal, res.Status)
	}
	res = Execute(p, "Cafe\u0301 mail", false, r.x)
	if res.Status != StatusComplete {
		t.Fatalf("the NFD-typed name must delete:\n%s", res.Message())
	}
}

// covers: MA-964, R36
func TestTheStepsRunInTheirOrder(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	def := schedule.DefaultNameFor(dir)
	r.sched.state[def+"-verify"] = schedule.Installed
	started := func() bool {
		ls := logLines(t, r.x.Log)
		return len(ls) > 0 && ls[0]["event"] == "started"
	}
	r.sched.onRemove = func(n string) {
		if !started() {
			t.Errorf("the schedule %s was stopped before the started line", n)
		}
	}
	var seen []string
	faultOn(t, &seen, func(string) error {
		if len(seen) == 1 {
			if !started() {
				t.Error("a file was removed before the started line")
			}
			if len(r.sched.removed) != 2 || r.ptr.saves != 1 {
				t.Errorf("schedules (%q) and settings (%d saves) come before the folder's files", r.sched.removed, r.ptr.saves)
			}
		}
		return nil
	})
	p := r.plan(t)
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	m := -1
	for i, s := range seen {
		if s == manifestName {
			m = i
		}
	}
	if m < 0 {
		t.Fatalf("the manifest was never removed: %q", seen)
	}
	for _, it := range p.Remove {
		if !contains(seen[:m+1], it.Rel) {
			t.Errorf("%s was not removed before the manifest", it.Rel)
		}
	}
	for _, s := range seen[m+1:] {
		if s != lockName && !contains(p.Folders, s) {
			t.Errorf("%s was removed after the manifest: only folders and the lock file may follow it", s)
		}
	}
}

// covers: MA-964, R36
func TestAFaultBeforeTheManifestKeepsARecognisedArchiveThatDeleteAgainFinishes(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	fault := true
	faultOn(t, nil, func(rel string) error {
		if fault && rel == stemA+".eml" {
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, false)
	if res.Status != StatusPartial || res.ExitCode() != 3 || !res.Unfinished {
		t.Fatalf("status %q exit %d unfinished %v:\n%s", res.Status, res.ExitCode(), res.Unfinished, res.Message())
	}
	if !strings.HasPrefix(res.Lines[0], "Not fully deleted. 1 file could not be removed: "+stemA+".eml.") ||
		!strings.Contains(res.Lines[0], "Close Outlook or any program using it, then click Delete again.") { // one file: "it" (MA-1028, B7)
		t.Fatalf("first line: %q", res.Lines[0])
	}
	if !exists(filepath.Join(dir, manifestName)) || !IsArchive(dir) || exists(filepath.Join(dir, filepath.FromSlash(stemA+".html"))) {
		t.Fatal("the manifest is kept (a recognised archive) and the rest is deleted")
	}
	if !exists(filepath.Join(dir, ".mailarchive-history.jsonl")) {
		t.Fatal("the late state files stay with the manifest")
	}
	if len(r.rows) != 0 {
		t.Fatal("the row stays")
	}
	if u, err := UnfinishedDelete(r.x.Log, dir, ""); err != nil || u == nil {
		t.Fatalf("the row is marked Delete not finished: %+v %v", u, err)
	}
	// Delete again, the file released: it finishes.
	fault = false
	res = r.run(t, false)
	if res.Status != StatusComplete || exists(filepath.Join(dir, manifestName)) || len(r.rows) != 1 {
		t.Fatalf("Delete again: %s", res.Message())
	}
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u != nil {
		t.Fatalf("a finished delete is not unfinished: %+v", u)
	}
}

// covers: MA-964, R36
func TestStep0ALogThatCannotBeWrittenDeletesNothing(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	// Positive twin first: the same delete with a writable log is logged.
	{
		twin := newRig(t, smallArchive(t, t.TempDir()))
		if res := twin.run(t, false); res.Status != StatusComplete || len(logLines(t, twin.x.Log)) == 0 {
			t.Fatalf("twin: %s", res.Message())
		}
	}
	r.x.Log = t.TempDir() // a folder: nothing can be appended to it
	before := tree(t, dir)
	res := r.run(t, false)
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1), assure.Names(msgLogFailed),
		assure.NoSideEffect(func() bool {
			return sameTree(before, tree(t, dir)) && len(r.sched.removed) == 0 && r.ptr.saves == 0 && len(r.rows) == 0
		}))
	if res.Refusal == nil || res.Refusal.Case != CaseLog || res.Record != nil {
		t.Fatalf("%+v", res.Refusal)
	}
}

// covers: MA-964, R36
func TestStep1ALockHeldOrAChangedArchiveRefusesUnderTheLock(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	l, err := lockfile.AcquireAs(filepath.Join(dir, lockName), "export")
	if err != nil {
		t.Fatal(err)
	}
	before := tree(t, dir)
	res := r.run(t, false)
	l.Release()
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1), assure.Names(msgBusy),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) && len(r.sched.removed) == 0 }))
	if res.Status != StatusRefusedAfterStart || !strings.Contains(string(res.Record), "Status: refused after start\n") {
		t.Fatalf("status %q record:\n%s", res.Status, res.Record)
	}
	if ev := events(logLines(t, r.x.Log)); strings.Join(ev, ",") != "started,finished refused" {
		t.Fatalf("log: %q", ev)
	}
	// The archive changed since its plan: a backup recorded another message.
	p := r.plan(t)
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}, {Path: stemB + ".html"}})
	before = treeWithoutLock(t, dir)
	res = Execute(p, p.Name, false, r.x)
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1), assure.Names(msgChanged),
		assure.NoSideEffect(func() bool { return sameTree(before, treeWithoutLock(t, dir)) && len(r.sched.removed) == 0 }))
	if res.Refusal == nil || res.Refusal.Case != CaseChanged {
		t.Fatalf("%+v", res.Refusal)
	}
}

// covers: MA-964, R36
func TestStep2AStopNamesWhatWasAlreadyStoppedAndChangesNothingElse(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	def := schedule.DefaultNameFor(dir)
	r.sched.state[def+"-verify"] = schedule.Installed
	r.sched.fail[def+"-verify"] = errors.New("Access is denied")
	before := treeWithoutLock(t, dir)
	res := r.run(t, false)
	want := "Could not stop the scheduled backup " + def + "-verify: Access is denied. Already stopped: " + def +
		" (set them up again in Settings, or click Delete again). Nothing else was changed." +
		" To go on, make sure this computer's scheduler (" + schedule.SchedulerName(runtime.GOOS) + ") works for your account, or remove that scheduled backup by hand (" +
		schedule.RemoveByHand(def+"-verify", runtime.GOOS) + "), then click Delete again." // H1, MA-1022: the way on
	if res.Status != StatusPartial || res.ExitCode() != 3 || !res.Unfinished || res.Lines[0] != want {
		t.Fatalf("status %q unfinished %v:\n%s\nwant first line %q", res.Status, res.Unfinished, res.Message(), want)
	}
	if !sameTree(before, treeWithoutLock(t, dir)) || r.ptr.saves != 0 || len(r.rows) != 0 {
		t.Fatal("nothing else may change")
	}
	if hasLine(res, "Stopped the scheduled backup "+def+"-verify") {
		t.Fatal("an entry the scheduler would not remove is never reported stopped")
	}
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u == nil {
		t.Fatal("a stop after step 2 began marks the row Delete not finished")
	}
	// The first entry fails: nothing is named as already stopped.
	r2 := newRig(t, smallArchive(t, t.TempDir()))
	r2.sched.fail[schedule.DefaultNameFor(r2.dir)] = errors.New("Access is denied")
	res = r2.run(t, false)
	if res.Status != StatusPartial || strings.Contains(res.Message(), "Already stopped") {
		t.Fatalf("%s", res.Message())
	}
}

// covers: MA-964, R36
func TestStep2WindowsRuleCountsAnEntryAbsentOnlyWhenItsSideFilesAgree(t *testing.T) {
	newWin := func(t *testing.T) (*rig, string) {
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		r.x.Deps.GOOS = "windows"
		def := schedule.DefaultNameFor(dir)
		r.sched.state[def] = schedule.NotInstalled // a paused archive: Pause removed the task and its side-files
		return r, def
	}
	// A paused archive on a Windows whose "missing" message is not English: the
	// query says not installed, schtasks' words are not understood, and neither
	// side-file is there — absent, and the delete completes.
	r, _ := newWin(t)
	res := r.run(t, false)
	if res.Status != StatusComplete || len(r.sched.removed) != 0 {
		t.Fatalf("paused, non-English: %s (removed %q)", res.Message(), r.sched.removed)
	}
	// The query says not installed but a side-file is there and schtasks' words are
	// not understood: not absent, so RemoveLiveEntry is asked.
	r, def := newWin(t)
	r.sched.sideFiles[def] = true
	if res := r.run(t, false); res.Status != StatusComplete || !contains(r.sched.removed, def) {
		t.Fatalf("side-file there: %s (removed %q)", res.Message(), r.sched.removed)
	}
	// schtasks says missing: absent, and its side-files are removed.
	r, def = newWin(t)
	r.sched.sideFiles[def], r.sched.saysMissing = true, true
	if res := r.run(t, false); res.Status != StatusComplete || len(r.sched.removed) != 0 || !contains(r.sched.sideRemoved, def) {
		t.Fatalf("says missing: %s (removed %q, side-files removed %q)", res.Message(), r.sched.removed, r.sched.sideRemoved)
	}
	// A stopped or denied Task Scheduler: the delete stops at step 2 with its error
	// and never reports the schedule stopped.
	r, def = newWin(t)
	r.sched.state[def] = schedule.SchedulerUnavailable
	r.sched.fail[def] = errors.New("The Task Scheduler service is not running")
	res = r.run(t, false)
	if res.Status != StatusPartial || !strings.Contains(res.Lines[0], "The Task Scheduler service is not running") || hasLine(res, "Stopped the scheduled backup") {
		t.Fatalf("stopped Task Scheduler: %s", res.Message())
	}
}

// covers: MA-964, R36
func TestStep3AndStep4StopWithTheirWords(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	// A per-name file that cannot be removed: a folder holding a file.
	stuck := filepath.Join(cfg, schedule.DefaultNameFor(dir)+".jobfail.log")
	writeFile(t, filepath.Join(stuck, "x"), "x")
	t.Cleanup(func() { os.RemoveAll(stuck) })
	before := treeWithoutLock(t, dir)
	res := r.run(t, false)
	if res.Status != StatusPartial || !res.Unfinished || !strings.HasPrefix(res.Lines[0], "Could not remove "+stuck+" in MailArchive's settings folder") {
		t.Fatalf("step 3: %s", res.Message())
	}
	if !sameTree(before, treeWithoutLock(t, dir)) || r.ptr.saves != 0 {
		t.Fatal("step 3's stop changes nothing after it")
	}
	os.RemoveAll(stuck)

	// Step 4: the settings cannot be saved.
	r = newRig(t, smallArchive(t, t.TempDir()))
	r.ptr.saveErr = errors.New("disk full")
	before = treeWithoutLock(t, r.dir)
	res = r.run(t, false)
	if res.Status != StatusPartial || res.Lines[0] != msgSettingsFailed || !sameTree(before, treeWithoutLock(t, r.dir)) {
		t.Fatalf("step 4: %s", res.Message())
	}
}

// covers: MA-964, R36
func TestStep3RemovesEachNamesSettingsFilesAndNoOtherArchives(t *testing.T) {
	// The names step 3 removes are those the alerts write (kept in step with
	// backupalert, which this package cannot import).
	if alertStreakSuffix != backupalert.StreakSuffix || syncAlertSuffix != backupalert.SyncAlertSuffix {
		t.Fatal("the alert files' names drifted from backupalert's")
	}
	dir := smallArchive(t, t.TempDir())
	other := smallArchive(t, t.TempDir())
	if alertKey(dir) != backupalert.KeyFor(dir) {
		t.Fatal("the alert key drifted from backupalert.KeyFor")
	}
	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	def, otherDef := schedule.DefaultNameFor(dir), schedule.DefaultNameFor(other)
	mine := []string{def + ".jobfail.log", def + ".older.jobfail.log", def + ".alert-streak", def + ".sync-alert", lastFailureName}
	theirs := []string{otherDef + ".jobfail.log", otherDef + ".alert-streak", otherDef + ".sync-alert"}
	for _, n := range append(append([]string(nil), mine...), theirs...) {
		writeFile(t, filepath.Join(cfg, n), "x")
		t.Cleanup(func() { os.Remove(filepath.Join(cfg, n)) })
	}
	writeFile(t, filepath.Join(cfg, lastFailureName), `{"version":1,"name":"`+def+`"}`)
	if err := os.Chmod(filepath.Join(cfg, lastFailureName), 0o600); err != nil {
		t.Fatal(err)
	}
	jp := writeJob(t, def, dir)
	r := newRig(t, dir)
	if res := r.run(t, false); res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	for _, n := range append(mine, filepath.Base(jp)) {
		if exists(filepath.Join(cfg, n)) {
			t.Errorf("%s must be removed", n)
		}
	}
	for _, n := range theirs {
		if !exists(filepath.Join(cfg, n)) {
			t.Errorf("another archive's %s must be kept", n)
		}
	}
	// last-failure.json naming another schedule is kept.
	writeFile(t, filepath.Join(cfg, lastFailureName), `{"version":1,"name":"`+otherDef+`"}`)
	os.Chmod(filepath.Join(cfg, lastFailureName), 0o600)
	r = newRig(t, smallArchive(t, t.TempDir()))
	if res := r.run(t, false); res.Status != StatusComplete || !exists(filepath.Join(cfg, lastFailureName)) {
		t.Fatalf("another schedule's last failure is kept: %s", res.Message())
	}
}

// covers: MA-964, R36
func TestDeletingTheCurrentArchiveStopsItsCaptureAndClearsBackupFor(t *testing.T) {
	// §5.1 (c), §5.3 step 4 [C4]: the current archive's capture registered under
	// another folder X's name is stopped too, and BackupFor is cleared, as it always
	// is when the current archive is deleted; X itself is untouched.
	dir := smallArchive(t, t.TempDir())
	x := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.Deps.Settings = Settings{Current: dir, CaptureBackupFor: x}
	r.ptr.p = Pointers{Out: dir, BackupFor: x, LastArchive: x}
	captured := schedule.DefaultNameFor(x)
	r.sched.state[captured] = schedule.Installed
	before := tree(t, x)
	res := r.run(t, false)
	if res.Status != StatusComplete || !contains(r.sched.removed, captured) {
		t.Fatalf("%s (stopped %q)", res.Message(), r.sched.removed)
	}
	if r.ptr.p != (Pointers{LastArchive: x}) {
		t.Fatalf("Out and BackupFor cleared, last-archive.txt naming another folder kept: %+v", r.ptr.p)
	}
	if !sameTree(before, tree(t, x)) {
		t.Fatal("the folder the capture was registered under is not touched")
	}
	// Not the current archive, BackupFor naming it (a run-job's, which counts as
	// unset for the capture): cleared, and Out kept.
	d2 := smallArchive(t, t.TempDir())
	r = newRig(t, d2)
	r.ptr.p = Pointers{Out: x, BackupFor: d2}
	if res := r.run(t, false); res.Status != StatusComplete || r.ptr.p != (Pointers{Out: x}) {
		t.Fatalf("%s %+v", res.Message(), r.ptr.p)
	}
}

// covers: MA-964, R36
func TestALockReleasedAndFinishedWrittenAfterAPanic(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.sched.panicOn = schedule.DefaultNameFor(dir)
	res := r.run(t, false)
	if res.Status != StatusPartial || !strings.Contains(res.Message(), "stopped unexpectedly") || strings.Contains(strings.ToLower(res.Message()), "nothing was changed") {
		t.Fatalf("after a panic in step 2: %s", res.Message())
	}
	if lockfile.Held(filepath.Join(dir, lockName)) {
		t.Fatal("the archive lock is released after a panic")
	}
	ev := events(logLines(t, r.x.Log))
	if len(ev) == 0 || ev[len(ev)-1] != "finished partial" {
		t.Fatalf("log: %q", ev)
	}
	if res.Record == nil {
		t.Fatal("the deletion record is built after a panic too")
	}

	// A panic before step 2 (an encrypted archive's lock on the page): the mark is
	// cleared, the delete is refused after start, never "nothing was changed".
	enc := encryptedClean(t)
	r = newRig(t, enc)
	r.x.Deps.Record = recordedAt(enc)
	r.keys.err = errPanic
	res = r.run(t, false)
	if res.Status != StatusRefusedAfterStart || !strings.Contains(res.Message(), "stopped unexpectedly") || len(r.keys.marked) != 0 {
		t.Fatalf("after a panic in step 1 (b): %s (marked %v)", res.Message(), r.keys.marked)
	}
	if strings.Contains(strings.ToLower(res.Message()), "nothing was changed") {
		t.Fatal("never 'nothing was changed' after step 0")
	}
}
