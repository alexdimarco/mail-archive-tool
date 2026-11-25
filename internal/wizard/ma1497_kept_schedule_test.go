package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1497, MA-1501, S90
// A kept schedule counts only when the scheduler runs it (the adversarial pass's CON-01, HIGH; the
// lead's fold: "A continued archive counts as scheduled only when the scheduler says its own entry is
// installed and its record is not 'Run now only'"). An archive brought up to date whose job file reads
// keeps its job and its entry as they are (C6), but its end-note line and the existing-archive lines'
// Pause advice (MA-1501) now ask the scheduler (queryLiveEntry, stood in) and read its record. Positive
// twin first: every member's entry installed, so a re-run says "cy@qz-c.example kept its own
// schedule." and gives the combined archive elsewhere the Pause advice. Then the three ways a job file
// is there with no live entry: an install that failed after the job file was written, an auto-pause
// (the real stopLiveBackup, through the stood-in scheduler) and "Run now only" (its entry taken out
// and its record written as the Desktop's Settings does): each says it has its own settings but no
// scheduled backup now, with its remedy, never "kept its own schedule"; none is installed over (its
// job file byte for byte, no entry); and no Pause advice is given for the combined archive.
func TestAKeptScheduleCountsOnlyWhenTheSchedulerRunsIt(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	madeList, err := job.MadeListFile()
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, r := range rows {
		all = append(all, r.path)
	}
	combined := filepath.Join(t.TempDir(), "Everything")
	if err := os.MkdirAll(combined, 0o700); err != nil {
		t.Fatal(err)
	}
	setJobFile(t, combined, job.Job{Name: "x", Mode: "incremental", Auto: true, Inputs: all}, nil)
	if err := job.RecordMade(madeList, combined, "Everything"); err != nil {
		t.Fatal(err)
	}
	together := "Your archive " + combined + " also backs up these mailboxes, all together."
	combinedLine := func(note string) string {
		for _, l := range strings.Split(note, "\n") {
			if strings.HasPrefix(l, together) {
				return l
			}
		}
		return ""
	}
	fresh := func() string {
		p := filepath.Join(t.TempDir(), "Mail archives")
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// run makes the set into parent with "Yes — every day", the scheduler stood in by r: its archives
	// and its end note.
	run := func(parent string, r *setScheduler) ([]Made, string) {
		t.Helper()
		r.install(t)
		s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: answering(scheduleDaily)}
		s.install(t)
		made, err := makeSet(rows, "darwin")
		if err != nil || len(made) < 4 {
			t.Fatalf("the set: %+v %v", made, err)
		}
		return made, s.notes[len(s.notes)-1]
	}
	// again runs the set into parent once more with Local Folders' archive taken away, so a Yes has a
	// new archive to install and the question and its end note come.
	again := func(parent string) ([]Made, string) {
		t.Helper()
		if err := os.RemoveAll(filepath.Join(parent, rows[3].name)); err != nil {
			t.Fatal(err)
		}
		return run(parent, &setScheduler{})
	}
	folder := func(parent string, i int) string { return filepath.Join(parent, rows[i].name) }
	entry := func(parent string, i int) string { return schedule.DefaultNameFor(folder(parent, i)) }

	// Positive twin: every member's entry is installed.
	p := fresh()
	run(p, &setScheduler{})
	made, note := again(p)
	for i := 0; i < 3; i++ {
		if made[i].Schedule != ScheduleKept {
			t.Fatalf("positive twin: %s is %v, want kept (%+v)", rows[i].name, made[i].Schedule, made)
		}
	}
	if !strings.Contains(note, "\n"+rows[2].name+setNoteKept+"\n") || combinedLine(note) != together+" "+existingPause {
		t.Fatalf("positive twin: the end note:\n%s", note)
	}

	// An install that failed after its job file was written: cy@qz-c.example has a job file and no
	// entry.
	q := fresh()
	made, _ = run(q, &setScheduler{fail: map[string]bool{entry(q, 2): true}})
	cyJob, ok := jobOf(t, folder(q, 2))
	if made[2].Schedule != ScheduleFailed || !ok {
		t.Fatalf("the failed install: %+v (job file there: %v)", made[2], ok)
	}
	made, note = again(q)
	code, msg := 0, ""
	if made[2].Schedule == ScheduleKeptOff && made[2].Outcome == MadeFinished {
		code, msg = 1, note
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(rows[2].name+" has its own settings but no scheduled backup now: in MailArchive Desktop, switch to it and click Repair on its Scheduled backup card.",
			together+" "+existingKeep),
		assure.Forbid(rows[2].name+setNoteKept, existingPause),
		assure.NoSideEffect(func() bool {
			now, _ := jobOf(t, folder(q, 2))
			return !setLive[entry(q, 2)] && bytes.Equal(now, cyJob)
		}))

	// An auto-pause (ann@qz-a.example's entry stopped by the auto-pause's own step, its job file and
	// record kept) and "Run now only" (bob@qz-b.example's entry taken out and its record written as
	// the Desktop's Settings does): back in the first parent, every entry installed before.
	if paused, err := stopLiveBackup(entry(p, 0), folder(p, 0)); !paused || err != nil {
		t.Fatalf("the auto-pause's step: %v %v", paused, err)
	}
	bob := folder(p, 1)
	if err := removeLiveEntry(schedule.Spec{Name: entry(p, 1), Out: bob}); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteDescriptor(bob, schedule.DescribeManual(schedule.Spec{Name: entry(p, 1), Out: bob, Interval: schedule.Daily, At: "02:00", Exe: "/opt/MailArchive/mailarchive", Args: []string{"-job", mustJobPath(t, bob)}})); err != nil {
		t.Fatal(err)
	}
	kept := map[int][]byte{}
	for _, i := range []int{0, 1} {
		kept[i], _ = jobOf(t, folder(p, i))
	}
	made, note = again(p)
	code, msg = 0, ""
	if made[0].Schedule == ScheduleKeptOff && made[1].Schedule == ScheduleKeptManual && made[2].Schedule == ScheduleKept {
		code, msg = 1, note
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(rows[0].name+setNoteKeptOff,
			rows[1].name+" has its own settings but no scheduled backup now: it is backed up only when you click Run now.",
			rows[2].name+setNoteKept, together+" "+existingKeep),
		assure.Forbid(rows[0].name+setNoteKept, rows[1].name+setNoteKept, existingPause),
		assure.NoSideEffect(func() bool {
			for i, b := range kept {
				if now, _ := jobOf(t, folder(p, i)); !bytes.Equal(now, b) || setLive[entry(p, i)] {
					return false
				}
			}
			return true
		}))
}
