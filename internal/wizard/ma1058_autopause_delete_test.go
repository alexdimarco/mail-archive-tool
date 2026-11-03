package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// ma1058Rig is newLocalRunRig's archive holding a delete marker this computer's deletion log knows
// nothing of (another computer or account began the delete), its local job named as the Desktop
// names it, and that job's schedule installed in a stand-in scheduler.
type ma1058Rig struct {
	*localRunRig
	logPath string
	name    string
	jobPath string
	removed *[]string
}

func newMA1058Rig(t *testing.T, live bool) *ma1058Rig {
	t.Helper()
	g := &ma1058Rig{localRunRig: newLocalRunRig(t)}
	g.logPath = ma1044DefaultLog(t) // MailArchive's own deletions.log: empty
	g.name = schedule.DefaultNameFor(g.out)
	g.jobPath = g.jobAt(filepath.Join(g.dir, g.name+".json"), g.mbox)
	if live {
		g.removed = recordRemovals(t, g.name)
	} else {
		g.removed = recordRemovals(t)
	}
	ma1048Plant(t, g.out)
	return g
}

func (g *ma1058Rig) countPath() string {
	return backupalert.DeleteRefusedPath(g.settings, backupalert.KeyFor(g.out))
}

// seed writes a count of refused runs whose first and latest were daysAgo days ago.
func (g *ma1058Rig) seed(daysAgo int) {
	g.t.Helper()
	then := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)
	b, err := json.Marshal(state.SourceStreak{Version: state.SourceStreakVersion, ConsecutiveFails: 1, FirstUnreachable: then, LastUnreachable: then})
	if err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(g.countPath(), b, 0o600); err != nil {
		g.t.Fatal(err)
	}
}

func (g *ma1058Rig) count() (state.SourceStreak, bool) {
	b, err := os.ReadFile(g.countPath())
	if err != nil {
		return state.SourceStreak{}, false
	}
	var s state.SourceStreak
	if json.Unmarshal(b, &s) != nil {
		return state.SourceStreak{}, false
	}
	return s, true
}

func (g *ma1058Rig) failLog() string {
	b, _ := os.ReadFile(filepath.Join(g.dir, g.name+".jobfail.log"))
	return string(b)
}

func (g *ma1058Rig) paused() *alert.Event {
	for i := range g.sent {
		if g.sent[i].Result == alert.ResultPaused {
			return &g.sent[i]
		}
	}
	return nil
}

// covers: MA-1058, R36
// A scheduled run-job refused for a delete this computer never began (only the folder's marker
// knows it; docs/design-archive-delete.md §15.10 slice 6, the owner's ruling Q5) counts toward an
// auto-pause by the source-loss rule: once the refused runs span 30 days, this computer's schedule
// (the job file's name) is stopped, the auto-pause email goes with its reason in the lead's words
// (what happened first, no "again", the remedy), the pause is told beside the job file, and the
// count ends. Nothing in the folder changes: no record, no seal, no log. Positive twin first:
// spanning 29 days the run counts and nothing is stopped.
func TestMA1058_RunJobAutoPausesAfter30DaysOfRefusalsForADeleteBegunElsewhere(t *testing.T) {
	g := newMA1058Rig(t, true)
	before := ma1044Files(t, g.out)

	// Positive twin: 29 days, counted, nothing stopped.
	g.seed(29)
	code := RunHeadless(g.jobPath)
	assure.Refused(t, code, g.failLog(), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+g.out+")"),
		assure.NoSideEffect(func() bool { return len(*g.removed) == 0 && ma1044SameFiles(before, ma1044Files(t, g.out)) }))
	s, ok := g.count()
	assure.Reached(t, ok && s.ConsecutiveFails == 2 && !s.AutoPauseDue(), "29 days: the refused run counted, not due")
	if g.paused() != nil {
		t.Fatalf("29 days: an auto-pause email went: %q", g.results())
	}

	// 31 days: this computer's schedule is stopped.
	g.seed(31)
	code = RunHeadless(g.jobPath)
	assure.Refused(t, code, g.failLog(), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+g.out+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, g.out)) }))
	if len(*g.removed) != 1 || (*g.removed)[0] != g.name {
		t.Fatalf("31 days: the job's own schedule was not stopped: removed %v", *g.removed)
	}
	ev := assure.Reached(t, g.paused(), "the auto-pause email")
	if ev.Error != AutoPauseDeleteWhy || ev.Archive != g.out {
		t.Errorf("the auto-pause email's reason:\n got %q\nwant %q", ev.Error, AutoPauseDeleteWhy)
	}
	for _, w := range []string{"again", "unreachable", "Repair"} {
		if strings.Contains(AutoPauseDeleteWhy, w) {
			t.Errorf("the words say %q: %s", w, AutoPauseDeleteWhy)
		}
	}
	assure.Reached(t, strings.Contains(g.failLog(), "auto-paused the scheduled backup: "+AutoPauseDeleteWhy), "the pause told beside the job file")
	if _, ok := g.count(); ok {
		t.Error("the auto-pause left the count behind")
	}
	if _, err := os.Lstat(backupalert.StreakPath(g.settings, backupalert.KeyFor(g.out))); !os.IsNotExist(err) {
		t.Error("the auto-pause left the alert streak behind")
	}
}

// covers: MA-1058, R36
// A delete this computer's deletion log knows never counts: by the folder (this computer began it;
// its row says "click Delete again"), or by the marker's run (begun here, the drive plugged in again
// under another name, DM-10). Such a refused run ends any count, and nothing is stopped however
// long it has gone on. A run that gets past the delete check ends the count too, and an auto-pause
// that finds no schedule of this job's running stops nothing and sends no email. Positive twin
// first: the delete only the marker knows counts.
func TestMA1058_ADeleteThisComputersLogKnowsNeverCounts(t *testing.T) {
	g := newMA1058Rig(t, true)
	g.seed(29)
	RunHeadless(g.jobPath)
	s, ok := g.count()
	assure.Reached(t, ok && s.ConsecutiveFails == 2, "positive twin: the delete only the marker knows counted")

	// The log knows the delete by the folder.
	ma1044Started(t, g.logPath, g.out)
	g.seed(31)
	if RunHeadless(g.jobPath) != 1 {
		t.Fatal("the run into a half-deleted folder was not refused")
	}
	if _, ok := g.count(); ok || len(*g.removed) != 0 || g.paused() != nil {
		t.Fatalf("a delete this computer's log knows counted: removed %v, sent %q", *g.removed, g.results())
	}

	// The marker's run was begun here, under another path.
	if err := os.WriteFile(g.logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "0123456789abcdef", "folder": filepath.Join(g.dir, "Old name"), "kind": "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(g.logPath, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	g.seed(31)
	if RunHeadless(g.jobPath) != 1 {
		t.Fatal("the run into a half-deleted folder was not refused")
	}
	if _, ok := g.count(); ok || len(*g.removed) != 0 || g.paused() != nil {
		t.Fatalf("a delete begun here counted: removed %v, sent %q", *g.removed, g.results())
	}

	// A run past the delete check ends the count.
	if err := os.WriteFile(g.logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	g.seed(31)
	if err := os.Remove(filepath.Join(g.out, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(g.jobPath); code != 0 {
		t.Fatalf("the run with the marker gone: exit %d %s", code, g.failLog())
	}
	if _, ok := g.count(); ok || len(*g.removed) != 0 {
		t.Fatalf("a run past the delete check left the count: removed %v", *g.removed)
	}

	// No schedule of this job's running: nothing is stopped, no email.
	n := newMA1058Rig(t, false)
	n.seed(31)
	RunHeadless(n.jobPath)
	if n.paused() != nil {
		t.Fatalf("an auto-pause email went for a pause that did not happen: %q", n.results())
	}
	assure.Reached(t, strings.Contains(n.failLog(), "auto-pause: auto-pause is due, but no live backup schedule was running under "+n.name), "the stop that found nothing, told beside the job file")
}
