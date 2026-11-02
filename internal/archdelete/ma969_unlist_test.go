package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// unlistRig is Remove from list's seams over a rig's fakes.
func unlistRig(t *testing.T, dir string) (*rig, UnlistDeps) {
	t.Helper()
	r := newRig(t, dir)
	r.ptr.p = Pointers{LastArchive: dir}
	return r, UnlistDeps{Deps: r.x.Deps, Scheduler: r.sched, Pointers: r.ptr, ForgetRow: r.x.ForgetRow}
}

// yes is Remove from list answered Yes to the question PlanUnlist asks now (its named
// entries passed on, as the Desktop and the command line pass the question shown).
func yes(t *testing.T, row string, x UnlistDeps) *UnlistResult {
	t.Helper()
	if q, err := PlanUnlist(row, x); err == nil {
		x.Named = q.Named
	}
	return Unlist(row, x)
}

// covers: MA-969, R36
func TestRemoveFromListForgetsTheRowKeepsTheFolderAndStopsOnlyAfterYes(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	def := schedule.DefaultNameFor(dir)
	r, x := unlistRig(t, dir)
	before := tree(t, dir)
	q, err := PlanUnlist(dir, x)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Question, "Its scheduled backup "+def+" will also be stopped. Remove from list? The folder and its files are kept.") {
		t.Fatalf("question %q", q.Question)
	}
	if len(r.sched.removed) != 0 {
		t.Fatal("the question stops nothing")
	}
	res := yes(t, dir, x)
	if !res.Removed || len(r.rows) != 1 || r.rows[0] != dir || !contains(r.sched.removed, def) {
		t.Fatalf("%v rows %q stopped %q", res.Lines, r.rows, r.sched.removed)
	}
	if !sameTree(before, tree(t, dir)) {
		t.Fatal("the folder and everything in it is kept byte-identical")
	}
	if r.ptr.p.LastArchive != "" {
		t.Fatal("last-archive.txt naming the folder is cleared")
	}
	// No schedule: the plain question.
	d2 := smallArchive(t, t.TempDir())
	r2, x2 := unlistRig(t, d2)
	r2.sched.state[schedule.DefaultNameFor(d2)] = schedule.NotInstalled
	if q, _ := PlanUnlist(d2, x2); q.Question != "Remove from list? The folder and its files are kept." {
		t.Fatalf("question %q", q.Question)
	}
}

// covers: MA-969, R36
func TestRemoveFromListWithTheDriveUnpluggedUsesThisComputersRecords(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	custom := "nightly-unplugged-test"
	jp := writeJob(t, custom, dir)
	r, x := unlistRig(t, dir)
	r.sched.state[custom] = schedule.Installed
	if err := os.RemoveAll(dir); err != nil { // the drive unplugged
		t.Fatal(err)
	}
	res := yes(t, dir, x)
	if !res.Removed || !contains(r.sched.removed, custom) || len(r.rows) != 1 {
		t.Fatalf("%v stopped %q", res.Lines, r.sched.removed)
	}
	if exists(jp) {
		t.Fatal("the stopped entry's job file is removed")
	}

	// The current archive, unreachable: Out is cleared. Reachable, it is refused.
	cur := smallArchive(t, t.TempDir())
	r, x = unlistRig(t, cur)
	x.Deps.Settings.Current = cur
	r.ptr.p.Out = cur
	before := tree(t, cur)
	res = yes(t, cur, x)
	assure.Refused(t, res.ExitCode(), strings.Join(res.Lines, "\n"), assure.Code(1), assure.Names("This is the archive in use."),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, cur)) && len(r.rows) == 0 && r.ptr.p.Out == cur }))
	if err := os.RemoveAll(cur); err != nil {
		t.Fatal(err)
	}
	res = yes(t, cur, x)
	if !res.Removed || r.ptr.p.Out != "" {
		t.Fatalf("%v out %q", res.Lines, r.ptr.p.Out)
	}
}

// covers: MA-969, R36
func TestRemoveFromListOfTheCurrentUnreachableArchiveStopsItsBackupForCapture(t *testing.T) {
	cur := filepath.Join(t.TempDir(), "gone")
	other := smallArchive(t, t.TempDir())
	r, x := unlistRig(t, cur)
	x.Deps.Settings = Settings{Current: cur, CaptureBackupFor: other}
	r.ptr.p = Pointers{Out: cur, BackupFor: other}
	captured := schedule.DefaultNameFor(other)
	r.sched.state[captured] = schedule.Installed
	q, err := PlanUnlist(cur, x)
	if err != nil || !strings.Contains(q.Question, captured) {
		t.Fatalf("the capture registered under BackupFor is named: %q %v", q.Question, err)
	}
	res := yes(t, cur, x)
	if !res.Removed || !contains(r.sched.removed, captured) || r.ptr.p.BackupFor != "" || r.ptr.p.Out != "" {
		t.Fatalf("%v stopped %q pointers %+v", res.Lines, r.sched.removed, r.ptr.p)
	}
}

// covers: MA-969, R36
func TestRemoveFromListOfAPausedArchiveOnANonEnglishSchedulerAndAPartialStop(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r, x := unlistRig(t, dir)
	x.Deps.GOOS = "windows"
	def := schedule.DefaultNameFor(dir)
	r.sched.state[def] = schedule.NotInstalled // paused: no task, no side-files, words not understood
	if res := yes(t, dir, x); !res.Removed || len(r.sched.removed) != 0 || len(r.rows) != 1 {
		t.Fatalf("%v", res.Lines)
	}

	// A second entry fails after the first was stopped.
	d2 := smallArchive(t, t.TempDir())
	r, x = unlistRig(t, d2)
	def = schedule.DefaultNameFor(d2)
	r.sched.state[def+"-verify"] = schedule.Installed
	r.sched.fail[def+"-verify"] = errors.New("Access is denied")
	res := yes(t, d2, x)
	want := "Could not stop the scheduled backup " + def + "-verify: Access is denied. Already stopped: " + def +
		" (set them up again in Settings, or click Remove from list again). The row is kept." +
		" To go on, make sure this computer's scheduler (" + schedule.SchedulerName(runtime.GOOS) + ") works for your account, or remove that scheduled backup by hand (" +
		schedule.RemoveByHand(def+"-verify", runtime.GOOS) + "), then click Remove from list again." // H1, MA-1022: the way on
	if res.Removed || len(r.rows) != 0 || len(res.Lines) == 0 || res.Lines[0] != want || r.ptr.saves != 0 {
		t.Fatalf("%q\nwant %q", res.Lines, want)
	}
	// The first fails: nothing named as already stopped.
	d3 := smallArchive(t, t.TempDir())
	r, x = unlistRig(t, d3)
	r.sched.fail[schedule.DefaultNameFor(d3)] = errors.New("Access is denied")
	if res := yes(t, d3, x); res.Removed || strings.Contains(strings.Join(res.Lines, " "), "Already stopped") {
		t.Fatalf("%q", res.Lines)
	}
}
