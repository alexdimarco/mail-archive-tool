package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// recordRemovals replaces the host scheduler for the test: the entries named in live are installed,
// a query says so, and a removal is recorded and takes the entry out (a removal of a missing entry
// succeeds, as the real one does on every system).
func recordRemovals(t *testing.T, live ...string) *[]string {
	t.Helper()
	var removed []string
	installed := map[string]bool{}
	for _, n := range live {
		installed[n] = true
	}
	origR, origQ := removeLiveEntry, queryLiveEntry
	removeLiveEntry = func(s schedule.Spec) error { removed = append(removed, s.Name); delete(installed, s.Name); return nil }
	queryLiveEntry = func(name string) schedule.State {
		if installed[name] {
			return schedule.Installed
		}
		return schedule.NotInstalled
	}
	t.Cleanup(func() { removeLiveEntry, queryLiveEntry = origR, origQ })
	return &removed
}

// seedDueStreak writes a source streak that already spans more than the auto-pause threshold.
func seedDueStreak(t *testing.T, out string) {
	t.Helper()
	then := time.Now().UTC().Add(-31 * 24 * time.Hour)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 5, FirstUnreachable: then, LastUnreachable: then.Add(30*24*time.Hour + time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-890, R37
// AutoPauseIfDue pauses only when it stops this archive's own backup entry, and only after a run
// that itself found the source unreachable (the part A pass). Positive twin first: a due streak, a
// bound descriptor and an unreachable run pause, removing that entry. With the streak due but a
// run that said nothing about the source, nothing is removed and nothing is reported paused; with
// no descriptor (nothing bound to stop) nothing is reported paused and the error says why.
func TestAutoPauseReportsOnlyWhatItStopped(t *testing.T) {
	out := t.TempDir()
	name := schedule.DefaultNameFor(out)
	removed := recordRemovals(t, name)
	if err := schedule.WriteDescriptor(out, schedule.Descriptor{Name: name, Interval: "hourly", At: "00:10", Job: []string{"mailarchive", "-out", out}}); err != nil {
		t.Fatal(err)
	}
	seedDueStreak(t, out)
	if paused, err := AutoPauseIfDue(out, true); err != nil || !paused || len(*removed) != 1 || (*removed)[0] != name {
		t.Fatalf("the positive twin: paused=%v err=%v removed %v", paused, err, *removed)
	}

	*removed = nil
	if paused, err := AutoPauseIfDue(out, false); err != nil || paused || len(*removed) != 0 {
		t.Fatalf("a run that said nothing about the source: paused=%v err=%v removed %v", paused, err, *removed)
	}

	bare := t.TempDir()
	seedDueStreak(t, bare)
	paused, err := AutoPauseIfDue(bare, true)
	if paused || err == nil || !strings.Contains(err.Error(), "nothing was paused") || len(*removed) != 0 {
		t.Fatalf("no descriptor: paused=%v err=%v removed %v", paused, err, *removed)
	}
}

// covers: MA-890, R37
// AutoPauseIfDue reports paused only for an entry the scheduler said was running (the part A
// re-verification, 2026-10-28): the removal of a missing entry succeeds on every system, so an
// archive whose entry an earlier auto-pause already stopped (another entry captures into it, or
// its job is run by hand) is not "paused" again — no auto-pause email, and the log does not say
// the schedule was stopped. The removal is still made (it cleans up side files). A scheduler that
// can't answer is not "installed", so it never reports a pause. And the verify is stopped only
// with a backup this pause stopped: a "nothing was paused" result never leaves the verify stopped
// with no email. Positive twin first: a running entry and its verify are both stopped.
func TestAutoPauseCountsOnlyAnEntryThatWasRunning(t *testing.T) {
	out := t.TempDir()
	name := schedule.DefaultNameFor(out)
	vname := name + "-verify"
	if err := schedule.WriteDescriptor(out, schedule.Descriptor{Name: name, Interval: "daily", At: "02:00", Job: []string{"mailarchive", "-out", out}}); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteVerifyDescriptor(out, schedule.Descriptor{Name: vname, Interval: "weekly", At: "05:00", Job: []string{"verify", "-out", out, "-unattended"}}); err != nil {
		t.Fatal(err)
	}
	seedDueStreak(t, out)
	removed := recordRemovals(t, name, vname)
	if paused, err := AutoPauseIfDue(out, true); err != nil || !paused || strings.Join(*removed, ",") != name+","+vname {
		t.Fatalf("the positive twin: paused=%v err=%v removed %v", paused, err, *removed)
	}

	// Already stopped: the removal is made, nothing is reported paused, and the verify is not
	// touched again.
	*removed = nil
	paused, err := AutoPauseIfDue(out, true)
	if paused || err == nil || !strings.Contains(err.Error(), "nothing was paused") || strings.Join(*removed, ",") != name {
		t.Fatalf("an entry that was not running: paused=%v err=%v removed %v", paused, err, *removed)
	}

	// The scheduler can't answer (Windows' SchedulerUnavailable): never a pause, and the verify,
	// though installed, is left running with its backup.
	removed = recordRemovals(t, name, vname)
	queryLiveEntry = func(string) schedule.State { return schedule.SchedulerUnavailable }
	if paused, err := AutoPauseIfDue(out, true); paused || err == nil || strings.Join(*removed, ",") != name {
		t.Fatalf("a scheduler that can't answer: paused=%v err=%v removed %v", paused, err, *removed)
	}

	// No backup record of its own, the verify's record readable: nothing is touched at all.
	removed = recordRemovals(t, name, vname)
	if err := schedule.RemoveDescriptor(out); err != nil {
		t.Fatal(err)
	}
	if _, ok := schedule.RecordedVerifyName(out); !ok {
		t.Fatal("fixture: the verify's record does not bind")
	}
	if paused, err := AutoPauseIfDue(out, true); paused || err == nil || len(*removed) != 0 {
		t.Fatalf("no backup record, a verify record: paused=%v err=%v removed %v", paused, err, *removed)
	}
}

// covers: MA-890, R37
// The local job sends no second auto-pause email for an entry an earlier auto-pause already
// stopped (the part A re-verification): after the pause, running the job by hand while its source
// is still gone sends a failure email but no auto-pause email. Positive twin first: the first
// due run pauses and emails.
func TestTheLocalJobSendsNoAutoPauseForAnEntryAlreadyStopped(t *testing.T) {
	g := newLocalRunRig(t)
	name := schedule.DefaultNameFor(g.out)
	removed := recordRemovals(t, name)
	if err := schedule.WriteDescriptor(g.out, schedule.Descriptor{Name: name, Interval: "hourly", At: "00:10", Job: []string{"mailarchive", "-out", g.out}}); err != nil {
		t.Fatal(err)
	}
	gone := g.jobAt(filepath.Join(g.dir, "gone.json"), filepath.Join(g.dir, "gone-Inbox"))
	seedDueStreak(t, g.out)
	RunHeadless(gone)
	if !strings.HasSuffix(g.results(), "paused") || len(*removed) != 1 {
		t.Fatalf("the positive twin: sent %q, removed %v", g.results(), *removed)
	}
	g.sent = nil
	RunHeadless(gone)
	if strings.Contains(g.results(), "paused") {
		t.Errorf("a run by hand after the pause sent another auto-pause email: %q", g.results())
	}
}

// covers: MA-890, R37
// The local job never loops on an auto-pause that stopped nothing (the part A pass): with the
// source streak due and no descriptor of its own, two failed runs whose source is gone send one
// failure email and no auto-pause email. And after a re-enable, a run that fails for a reason that
// says nothing about the source (here an input folder with no mail in it) does not pause again on
// the old streak: nothing removed, no auto-pause email, the streak left as it was. Positive twin
// first: the same due streak with the source gone pauses (TestTheLocalJobAutoPausesLikeTheCapture).
func TestTheLocalJobPausesOnlyOnItsOwnUnreachableRun(t *testing.T) {
	g := newLocalRunRig(t)
	removed := recordRemovals(t)
	gone := g.jobAt(filepath.Join(g.dir, "gone.json"), filepath.Join(g.dir, "gone-Inbox"))
	seedDueStreak(t, g.out)
	RunHeadless(gone)
	RunHeadless(gone)
	if g.results() != "failed" || len(*removed) != 0 {
		t.Fatalf("no descriptor of its own: sent %q, removed %v; want one failure email and no auto-pause", g.results(), *removed)
	}

	name := schedule.DefaultNameFor(g.out)
	if err := schedule.WriteDescriptor(g.out, schedule.Descriptor{Name: name, Interval: "hourly", At: "00:10", Job: []string{"mailarchive", "-out", g.out}}); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(g.dir, "empty-folder")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	g.sent = nil
	seedDueStreak(t, g.out)
	before, _ := state.ReadSourceStreak(g.out)
	inconclusive := g.jobAt(filepath.Join(g.dir, "inconclusive.json"), empty)
	if RunHeadless(inconclusive) != 1 {
		t.Fatal("fixture: a run of an input folder with no mail in it did not fail")
	}
	after, _ := state.ReadSourceStreak(g.out)
	if after.ConsecutiveFails != before.ConsecutiveFails {
		t.Fatalf("fixture: the failure moved the source streak (%d -> %d), so it is not inconclusive", before.ConsecutiveFails, after.ConsecutiveFails)
	}
	if len(*removed) != 0 || strings.Contains(g.results(), "paused") {
		t.Fatalf("an inconclusive failure on an old streak paused: removed %v, sent %q", *removed, g.results())
	}
}
