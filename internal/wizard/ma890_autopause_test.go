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

// recordRemovals replaces the live-entry removal with a recorder for the test.
func recordRemovals(t *testing.T) *[]string {
	t.Helper()
	var removed []string
	orig := removeLiveEntry
	removeLiveEntry = func(s schedule.Spec) error { removed = append(removed, s.Name); return nil }
	t.Cleanup(func() { removeLiveEntry = orig })
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
	removed := recordRemovals(t)
	out := t.TempDir()
	name := schedule.DefaultNameFor(out)
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
