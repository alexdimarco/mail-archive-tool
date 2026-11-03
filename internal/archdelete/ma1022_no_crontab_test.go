package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-1022, R36
// On a Linux computer with no crontab program a delete finishes (the 1.5.3 friction review's
// H1): the real host scheduler (HostScheduler, asking cron, with a PATH that holds no crontab)
// finds no entry to stop, so step 2 goes on, the delete completes, deletions.log says
// "finished complete" and the row is removed; no "Could not stop the scheduled backup". Before,
// step 2 stopped with "crontab -l failed…" every time, and the row stayed "Delete not finished".
// Positive twin first: the same delete with a scheduler that answers completes.
func TestADeleteFinishesOnLinuxWithNoCrontabProgram(t *testing.T) {
	twin := newRig(t, smallArchive(t, t.TempDir()))
	if res := twin.run(t, false); res.Status != StatusComplete {
		t.Fatalf("the positive twin: %s", res.Message())
	}

	t.Setenv("PATH", t.TempDir()) // no crontab here
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	r.x.Deps.GOOS = "linux"
	r.x.Scheduler = HostScheduler{GOOS: "linux"}
	res := r.run(t, false)
	// Nothing was there to stop, so nothing is said to be stopped.
	if res.Status != StatusComplete || res.ExitCode() != 0 || strings.Contains(res.Message(), "Could not stop") || len(r.rows) != 1 ||
		len(res.Stopped) != 0 || hasLine(res, "Stopped the scheduled") {
		t.Fatalf("a delete with no crontab program: status %q exit %d rows %v\n%s", res.Status, res.ExitCode(), r.rows, res.Message())
	}
	// Amended 2026-11-03 (design-archive-delete §15.2, DM-27, MA-1046): the `marked` line before step
	// 6's first removal.
	if ev := events(logLines(t, r.x.Log)); strings.Join(ev, ",") != "started,marked,folder removed,finished complete" {
		t.Fatalf("deletions.log: %q", ev)
	}
	if u, err := UnfinishedDelete(r.x.Log, dir, "linux"); u != nil || err != nil {
		t.Fatalf("the row must not be left Delete not finished: %+v %v", u, err)
	}
}

// covers: MA-1022, R36
// A crontab program that is there but cannot be run is not "no crontab" (the 1.5.3 friction
// fixes' adversarial pass, finding 2): a delete through the real host scheduler asking cron stops
// at step 2, its scheduled backup not taken as absent, the folder untouched and the row "Delete
// not finished", so a live cron entry is never left behind a deleted archive. Positive twin first:
// with no crontab program the delete completes (TestADeleteFinishesOnLinuxWithNoCrontabProgram's
// path, here again beside it).
func TestADeleteStopsAtStep2WhenCrontabCannotBeRun(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no crontab here
	twin := newRig(t, smallArchive(t, t.TempDir()))
	twin.x.Deps.GOOS = "linux"
	twin.x.Scheduler = HostScheduler{GOOS: "linux"}
	if res := twin.run(t, false); res.Status != StatusComplete {
		t.Fatalf("the positive twin: %s", res.Message())
	}

	bin := t.TempDir()
	name := "crontab"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte("this is not a program\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	dir := smallArchive(t, t.TempDir())
	before := tree(t, dir)
	r := newRig(t, dir)
	r.x.Deps.GOOS = "linux"
	r.x.Scheduler = HostScheduler{GOOS: "linux"}
	res := r.run(t, false)
	def := schedule.DefaultNameFor(dir)
	if res.Status != StatusPartial || !res.Unfinished || len(r.rows) != 0 || len(res.Stopped) != 0 ||
		!strings.HasPrefix(res.Lines[0], "Could not stop the scheduled backup "+def+": ") || !strings.Contains(res.Lines[0], "Nothing else was changed.") {
		t.Fatalf("a crontab that cannot be run: status %q unfinished %v rows %q\n%s", res.Status, res.Unfinished, r.rows, res.Message())
	}
	// Step 2 is before any file is removed: every file the folder held is still there.
	after := tree(t, dir)
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Fatalf("step 2's stop removed %s", p)
		}
	}
	if u, err := UnfinishedDelete(r.x.Log, dir, "linux"); u == nil || err != nil {
		t.Fatalf("the row must be left Delete not finished: %+v %v", u, err)
	}
}

// covers: MA-1022, R36
// A scheduled backup step 2 cannot stop says how to go on (H1): its own scheduler's name, how to
// remove that entry by hand on this platform, and Delete again — on each platform's words.
// Positive twin first: the scheduler that answers stops the entry and the delete completes.
func TestAStep2StopSaysHowToGoOn(t *testing.T) {
	twin := newRig(t, smallArchive(t, t.TempDir()))
	if res := twin.run(t, false); res.Status != StatusComplete || len(twin.sched.removed) != 1 {
		t.Fatalf("the positive twin: %s (removed %q)", res.Message(), twin.sched.removed)
	}
	for _, c := range []struct{ goos, sched, byHand string }{
		{"windows", "Task Scheduler", "schtasks /Delete /TN"},
		{"darwin", "launchd", "launchctl unload"},
		{"linux", "cron", "crontab -e: delete the lines under \"# mailarchive-backup "},
	} {
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		r.x.Deps.GOOS = c.goos
		def := schedule.DefaultNameFor(dir)
		r.sched.fail[def] = errors.New("the scheduler refused")
		res := r.run(t, false)
		line := res.Lines[0]
		for _, want := range []string{"Could not stop the scheduled backup " + def + ": the scheduler refused.", "Nothing else was changed.",
			"To go on, make sure this computer's scheduler (" + c.sched + ") works for your account, or remove that scheduled backup by hand (",
			c.byHand, def, "), then click Delete again."} {
			if !strings.Contains(line, want) {
				t.Errorf("%s: %q lacks %q", c.goos, line, want)
			}
		}
		if res.Status != StatusPartial || !res.Unfinished || strings.Contains(line, ":.") {
			t.Errorf("%s: status %q unfinished %v: %s", c.goos, res.Status, res.Unfinished, line)
		}
	}
}
