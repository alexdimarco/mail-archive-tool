package wizard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
	"mail-archive-tool/internal/state"
)

// syncJobAt writes a scheduled job for the rig's archive that syncs Thunderbird first.
func (g *localRunRig) syncJobAt(path string, inputs ...string) string {
	g.t.Helper()
	if err := job.Write(path, job.Job{Name: jobFileName(path), Out: g.out, Mode: "incremental", Inputs: inputs,
		ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		g.t.Fatal(err)
	}
	return path
}

// covers: MA-910, S65
// The sync email through the real scheduled entry point (RunHeadless; design-presync-
// attended §5.5, §5.6, C-35), the session scripted through the presyncjob seam (it starts
// nothing). Positive twin first: a scheduled run that did not fail, whose missed sync makes
// the streak stale, sends the one sync email; the next such run sends none. A failed run
// never sends it; the run that then sends the recovery email defers it, and the run after
// sends it — so no run sends two emails. A job without the sync sends none.
func TestTheScheduledRunSendsTheSyncEmailOncePerStreak(t *testing.T) {
	fake := &outcomeSession{}
	fake.set(presync.OutcomeOwnerActiveSkipped)
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	defer restore()
	r := presync.OverrideTablesForTest(nil, map[string]bool{runtime.GOOS: true})
	defer r()
	// Two misses four days ago and three days ago: this run's miss makes it stale.
	old := time.Now().Add(-4 * 24 * time.Hour)
	planted := state.SyncStreak{}.Miss("owner-active-skipped", old).Miss("owner-active-skipped", old.Add(time.Hour))
	count := func(g *localRunRig) int { return strings.Count(g.results(), backupalert.ResultSyncStale) }

	g := newLocalRunRig(t)
	if err := state.WriteSyncStreak(g.out, planted); err != nil {
		t.Fatal(err)
	}
	sj := g.syncJobAt(filepath.Join(g.dir, "sync.json"), g.mbox)
	if RunHeadless(sj) != 0 || count(g) != 1 || g.results() != backupalert.ResultSyncStale {
		t.Fatalf("positive twin: a stale scheduled run sent %q", g.results())
	}
	if RunHeadless(sj) != 0 || count(g) != 1 {
		t.Fatalf("the next stale run of the same streak sent again: %q", g.results())
	}

	f := newLocalRunRig(t)
	if err := state.WriteSyncStreak(f.out, planted.Miss("owner-active-skipped", time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	failJob := f.syncJobAt(filepath.Join(f.dir, "fail.json"), filepath.Join(f.dir, "gone-Inbox"))
	okJob := f.syncJobAt(filepath.Join(f.dir, "ok.json"), f.mbox)
	if RunHeadless(failJob) != 1 || f.results() != backupalert.ResultFailed {
		t.Fatalf("a failed run with a stale streak sent %q, want the failure email alone", f.results())
	}
	if RunHeadless(okJob) != 0 || f.results() != backupalert.ResultFailed+","+backupalert.ResultRecovered {
		t.Fatalf("the run that sends the recovery email sent %q, want the sync email deferred", f.results())
	}
	if RunHeadless(okJob) != 0 || f.results() != backupalert.ResultFailed+","+backupalert.ResultRecovered+","+backupalert.ResultSyncStale {
		t.Fatalf("the run after the recovery did not send the deferred sync email: %q", f.results())
	}

	n := newLocalRunRig(t)
	if err := state.WriteSyncStreak(n.out, planted.Miss("owner-active-skipped", time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	plain := n.jobAt(filepath.Join(n.dir, "plain.json"), n.mbox)
	if RunHeadless(plain) != 0 || len(n.sent) != 0 {
		t.Fatalf("a job without the sync sent %q", n.results())
	}
	if _, err := os.Lstat(filepath.Join(n.out, state.SyncStreakName)); !os.IsNotExist(err) {
		t.Fatalf("the sync-off scheduled run kept the streak (%v)", err)
	}
}
