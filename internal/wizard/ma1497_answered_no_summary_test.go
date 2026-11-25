package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1497, S90
// RV-03 (the re-verify of rounds A and B): CON-01's leftover when a re-run makes a new archive and the
// schedule question is answered No or Cancel. A member with its own settings but no scheduled backup
// now was never mentioned: the summary said so only when there was nothing new to schedule, and No or
// Cancel shows no end note. The summary, which comes before the question, now names each such member
// whenever any has that state, so every answer reaches it, in the end note's words: an install that
// failed after its job file was written, and "Run now only". Nothing is installed over (C6): each job
// file byte for byte, no entry; no Pause advice. Positive twin first: with every member's entry
// installed, the same re-run answered No says no such line.
func TestTheSummaryNamesAMemberWithNoScheduledBackupWhateverTheAnswer(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	const question = "Keep these archives current automatically?"
	notNow := " has its own settings but no scheduled backup now"
	cancel := func(text string) (string, bool, error) {
		if strings.HasPrefix(text, question) {
			return "", true, zenity.ErrCanceled
		}
		return "", false, nil
	}
	// run makes the set into parent, the scheduler stood in by r and the schedule question answered by
	// answer: its archives and its dialogs.
	run := func(parent string, r *setScheduler, answer func(string) (string, bool, error)) ([]Made, *flowScript) {
		t.Helper()
		r.install(t)
		s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: answer}
		s.install(t)
		made, err := makeSet(rows, "darwin")
		if err != nil || len(made) < 4 {
			t.Fatalf("the set: %+v %v", made, err)
		}
		return made, s
	}
	// again runs the set into parent once more with Local Folders' archive taken away, so the re-run
	// makes a new archive and the schedule question is asked.
	again := func(parent string, answer func(string) (string, bool, error)) ([]Made, *flowScript) {
		t.Helper()
		if err := os.RemoveAll(filepath.Join(parent, rows[3].name)); err != nil {
			t.Fatal(err)
		}
		return run(parent, &setScheduler{}, answer)
	}
	asked := func(s *flowScript) bool {
		for _, l := range s.lists {
			if strings.HasPrefix(l, question) {
				return true
			}
		}
		return false
	}
	folder := func(parent string, i int) string { return filepath.Join(parent, rows[i].name) }
	entry := func(parent string, i int) string { return schedule.DefaultNameFor(folder(parent, i)) }

	// Positive twin: every member's entry installed; a re-run with a new archive, answered No.
	p := setFresh(t)
	run(p, &setScheduler{}, answering(scheduleDaily))
	if _, s := again(p, answering(scheduleNo)); !asked(s) || strings.Contains(s.summaryOf(t), notNow) {
		t.Fatalf("positive twin: every entry installed, answered No: asked %v, summary:\n%s", asked(s), s.summaryOf(t))
	}

	// cy's install failed after its job file was written; bob is "Run now only".
	q := setFresh(t)
	run(q, &setScheduler{fail: map[string]bool{entry(q, 2): true}}, answering(scheduleDaily))
	bob := folder(q, 1)
	if err := removeLiveEntry(schedule.Spec{Name: entry(q, 1), Out: bob}); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteDescriptor(bob, schedule.DescribeManual(schedule.Spec{Name: entry(q, 1), Out: bob, Interval: schedule.Daily, At: "02:00", Exe: "/opt/MailArchive/mailarchive", Args: []string{"-job", mustJobPath(t, bob)}})); err != nil {
		t.Fatal(err)
	}
	kept := map[int][]byte{}
	for _, i := range []int{1, 2} {
		kept[i], _ = jobOf(t, folder(q, i))
	}
	// Answered No, then Cancel.
	for _, answer := range []func(string) (string, bool, error){answering(scheduleNo), cancel} {
		made, s := again(q, answer)
		code, msg := 0, ""
		if asked(s) && made[1].Schedule == ScheduleKeptManual && made[2].Schedule == ScheduleKeptOff && made[3].New {
			code, msg = 1, s.summaryOf(t)
		}
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names(rows[2].name+setNoteKeptOff, rows[1].name+setNoteKeptManual),
			assure.Forbid(rows[0].name+notNow, rows[2].name+setNoteKept, existingPause),
			assure.NoSideEffect(func() bool {
				for i, b := range kept {
					if now, _ := jobOf(t, folder(q, i)); !bytes.Equal(now, b) || setLive[entry(q, i)] {
						return false
					}
				}
				return true
			}))
	}
}
