package wizard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
)

// existingIn are the end note's lines about existing archives (MA-1501) in the notes shown.
func existingIn(notes []string) []string {
	var out []string
	for _, n := range notes {
		for _, l := range strings.Split(n, "\n") {
			if strings.Contains(l, " also backs up ") {
				out = append(out, l)
			}
		}
	}
	return out
}

// covers: MA-1501, S90
// The existing-archive lines (docs/design-per-mailbox-archives.md §11, revision 2's C1), in the Yes
// end note only. Positive twin first: an archive elsewhere with auto-discovery on whose inputs are
// all mailboxes of the plan, with every member scheduled, gives "Your archive PATH also backs up these
// mailboxes, all together." and the Pause advice. A one-input archive of a plan mailbox elsewhere
// gives "PATH also backs up NAME."; an auto job with no inputs, and an auto-off job with two inputs
// one of them outside the plan, give the "all together" line with no Pause advice; a paused, an
// encrypted and an Outlook-app archive, an unreadable job, a known folder that cannot be looked at,
// and a one-input job outside the plan give no line. None of it is in the summary, and a No gives no
// line. A member that failed, and one whose install failed, take the Pause advice away. A re-run into
// the same parent gives no line for any member, also for a member known by another spelling (a link
// to its folder), which only the plan's own exclusion passes over.
func TestTheEndNoteNamesArchivesThatBackUpTheSameMail(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	made, err := job.MadeListFile()
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	known := func(name string, j job.Job, body []byte) string {
		t.Helper()
		dir := filepath.Join(elsewhere, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		j.Name, j.Mode = "x", "incremental"
		setJobFile(t, dir, j, body)
		if err := job.RecordMade(made, dir, name); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	var all []string
	for _, r := range rows {
		all = append(all, r.path)
	}
	outside := t.TempDir()
	combined := known("Everything", job.Job{Auto: true, Inputs: all}, nil)
	autoBare := known("Auto with no inputs", job.Job{Auto: true}, nil)
	twoOne := known("Two, one outside", job.Job{Inputs: []string{rows[0].path, outside}}, nil)
	justBob := known("Just bob", job.Job{Inputs: []string{rows[1].path}}, nil)
	paused := known("Paused", job.Job{Auto: true}, nil)
	if err := seal.Seal(paused, "test"); err != nil {
		t.Fatal(err)
	}
	locked := known("Locked", job.Job{Auto: true}, nil)
	if err := os.WriteFile(filepath.Join(locked, ".mailarchive-vault.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	known("Outlook", job.Job{Outlook: true}, nil)
	known("Unreadable", job.Job{}, []byte("{not a job"))
	gone := known("Gone", job.Job{Auto: true}, nil)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	known("Outside only", job.Job{Inputs: []string{outside}}, nil)
	// Amended 2026-11-25 (the friction review's F3, the lead's call; additive): an "All of them" job
	// that still lists a removed account's folder, not on this computer now, which no set archive can
	// ever cover: its line names that folder and says its mail stays, and it gets no Pause advice, nor
	// "Keep it running until each … has its own scheduled backup" when every mailbox of the set is
	// scheduled. A member that failed now names itself in the advice ("Keep it running until NAME
	// finishes; then run this again into the same folder."; it read "Keep it running until each of
	// these mailboxes has its own scheduled backup." before).
	removed := filepath.Join(t.TempDir(), "ImapMail", "imap.removed.example")
	withGone := known("With a removed account", job.Job{Auto: true, Inputs: append(append([]string(nil), all...), removed)}, nil)
	gone1 := " It also lists " + removed + ", which is not on this computer now; its mail already saved stays in it."
	untilCy := " Keep it running until cy@qz-c.example finishes; then run this again into the same folder."

	together := func(p string) string { return "Your archive " + p + " also backs up these mailboxes, all together." }
	pause := " " + existingPause
	keep := " " + existingKeep
	run := func(parent string, answer string, sched *setScheduler, between func()) (*flowScript, []Made) {
		t.Helper()
		sched.install(t)
		s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: answering(answer)}
		s.install(t)
		ch, err := chooseSet(rows, "darwin")
		if err != nil {
			t.Fatal(err)
		}
		if between != nil {
			between()
		}
		m, err := runSet(ch, "darwin")
		if err != nil {
			t.Fatal(err)
		}
		return s, m
	}
	fresh := func() string {
		p := filepath.Join(t.TempDir(), "Mail archives")
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The lines about the archives made elsewhere here (each run's own set archives, in parents of
	// their own, are known too, and later runs name them: one-input archives of the same mailboxes).
	elsewhereIn := func(notes []string) []string {
		var out []string
		for _, l := range existingIn(notes) {
			if strings.Contains(l, elsewhere+string(filepath.Separator)) {
				out = append(out, l)
			}
		}
		return out
	}
	has := func(lines []string, want ...string) {
		t.Helper()
		if len(lines) != len(want) {
			t.Errorf("the existing-archive lines:\n%q\nwant\n%q", lines, want)
			return
		}
		for i := range want {
			if lines[i] != want[i] {
				t.Errorf("line %d:\n got %q\nwant %q", i, lines[i], want[i])
			}
		}
	}

	// Positive twin: every member scheduled.
	p1 := fresh()
	s, _ := run(p1, scheduleDaily, &setScheduler{}, nil)
	has(elsewhereIn(s.notes),
		together(combined)+pause,
		together(autoBare)+keep,
		together(twoOne)+keep,
		justBob+" also backs up bob@qz-b.example."+pause,
		together(withGone)+gone1)
	if l := existingIn([]string{s.summaryOf(t)}); len(l) != 0 {
		t.Errorf("the summary says an existing archive: %q", l)
	}

	// No: no line anywhere.
	s, _ = run(fresh(), scheduleNo, &setScheduler{}, nil)
	if l := existingIn(append(s.notes, s.questions...)); len(l) != 0 {
		t.Errorf("No gave existing-archive lines: %q", l)
	}

	// A member that failed (cy@qz-c.example, its mailbox gone after the plan): no Pause advice, also
	// for the archive of bob@qz-b.example alone.
	s, _ = run(fresh(), scheduleDaily, &setScheduler{}, func() {
		if err := os.Rename(rows[2].path, rows[2].path+".kept"); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Rename(rows[2].path+".kept", rows[2].path); err != nil {
		t.Fatal(err)
	}
	has(elsewhereIn(s.notes), together(combined)+untilCy, together(autoBare)+untilCy, together(twoOne)+untilCy, justBob+" also backs up bob@qz-b.example."+untilCy, together(withGone)+gone1+untilCy)
	// The first set's own archives, now elsewhere for this one, each back up one of its mailboxes.
	if l := strings.Join(existingIn(s.notes), "\n"); !strings.Contains(l, filepath.Join(p1, rows[0].name)+" also backs up ann@qz-a.example."+untilCy) {
		t.Errorf("the first set's archive of ann@qz-a.example is not named:\n%s", l)
	}

	// A member whose install failed: no Pause advice.
	p3 := fresh()
	s, _ = run(p3, scheduleDaily, &setScheduler{fail: map[string]bool{schedule.DefaultNameFor(filepath.Join(p3, rows[2].name)): true}}, nil)
	has(elsewhereIn(s.notes), together(combined)+keep, together(autoBare)+keep, together(twoOne)+keep, justBob+" also backs up bob@qz-b.example."+keep, together(withGone)+gone1+keep)

	// A re-run into the first parent: its members are passed over, also one known by another
	// spelling (a link to its folder, outside the parent), which only the plan's exclusion passes.
	if runtime.GOOS != "windows" {
		alias := filepath.Join(elsewhere, "ann by another name")
		if err := os.Symlink(filepath.Join(p1, rows[0].name), alias); err != nil {
			t.Fatal(err)
		}
		setJobFile(t, alias, job.Job{Name: "x", Mode: "incremental", Inputs: []string{rows[0].path}}, nil)
		if err := job.RecordMade(made, alias, "ann by another name"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(p1, rows[2].name)); err != nil {
		t.Fatal(err)
	}
	s, m := run(p1, scheduleDaily, &setScheduler{}, nil)
	if m[0].Schedule != ScheduleKept || m[2].Schedule != ScheduleInstalled {
		t.Fatalf("the re-run: %+v", m)
	}
	has(elsewhereIn(s.notes), together(combined)+pause, together(autoBare)+keep, together(twoOne)+keep, justBob+" also backs up bob@qz-b.example."+pause, together(withGone)+gone1)
	for _, l := range existingIn(s.notes) {
		if strings.Contains(l, p1) || strings.Contains(l, "another name") {
			t.Errorf("a member of the set is named: %q", l)
		}
	}
}
