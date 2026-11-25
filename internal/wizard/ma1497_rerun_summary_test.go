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
// Round B's leftover of CON-01 (docs/design-per-mailbox-archives.md revision 3): a re-run that has
// nothing new to schedule asks no schedule question and shows no end note, so its summary says each
// continued archive that has its own settings but no scheduled backup now, in the end note's words:
// an install that failed after its job file was written ("… click Repair on its Scheduled backup
// card.") and "Run now only" ("… it is backed up only when you click Run now."). Nothing is installed
// over (C6): each job file byte for byte, no entry. Positive twin first: with every entry installed, a
// re-run's summary says no such line, and no question is asked.
func TestARerunWithNothingNewSaysWhatIsNotScheduled(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	fresh := func() string {
		p := filepath.Join(t.TempDir(), "Mail archives")
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// run makes the set into parent with "Yes — every day", the scheduler stood in by r: its summary
	// and every list it asked (the schedule question is one).
	run := func(parent string, r *setScheduler) (string, []string) {
		t.Helper()
		r.install(t)
		s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: answering(scheduleDaily)}
		s.install(t)
		if made, err := makeSet(rows, "darwin"); err != nil || len(made) < 4 {
			t.Fatalf("the set: %+v %v", made, err)
		}
		return s.summaryOf(t), s.lists
	}
	asked := func(lists []string) bool {
		for _, l := range lists {
			if strings.HasPrefix(l, "Keep these archives current automatically?") {
				return true
			}
		}
		return false
	}
	folder := func(parent string, i int) string { return filepath.Join(parent, rows[i].name) }
	entry := func(parent string, i int) string { return schedule.DefaultNameFor(folder(parent, i)) }
	notNow := " has its own settings but no scheduled backup now"

	// Positive twin: every entry installed; the re-run has nothing new.
	p := fresh()
	run(p, &setScheduler{})
	summary, lists := run(p, &setScheduler{})
	if strings.Contains(summary, notNow) || asked(lists) {
		t.Fatalf("positive twin: every entry installed, nothing new: no question and no such line:\n%s", summary)
	}

	// cy's install failed after its job file was written; bob is "Run now only".
	q := fresh()
	run(q, &setScheduler{fail: map[string]bool{entry(q, 2): true}})
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
	summary, lists = run(q, &setScheduler{})
	lines := assure.Reached(t, strings.Split(summary, "\n"), "the summary's lines")
	has := func(want string) bool {
		for _, l := range lines {
			if l == want {
				return true
			}
		}
		return false
	}
	// Restated 2026-11-25 (the re-verify of rounds A and B, RF-05, its words: the buttons named), beside
	// the old lines, kept here: "… now: in MailArchive Desktop, switch to it and click Repair on its
	// Scheduled backup card." and "… now: it is backed up only when you click Run now." Each still a
	// whole line of the summary.
	if !has(rows[2].name+" has its own settings but no scheduled backup now: in MailArchive Desktop, click Switch to this archive on its row, then Repair on its Scheduled backup card.") ||
		!has(rows[1].name+" has its own settings but no scheduled backup now: it is backed up only when you click Run now on its Scheduled backup card in MailArchive Desktop.") {
		t.Errorf("the re-run's summary must say each member with no scheduled backup now:\n%s", summary)
	}
	if strings.Contains(summary, rows[0].name+notNow) || asked(lists) {
		t.Errorf("a member whose entry is installed says nothing, and nothing new asks no question:\n%s", summary)
	}
	for i, b := range kept {
		if now, _ := jobOf(t, folder(q, i)); !bytes.Equal(now, b) || setLive[entry(q, i)] {
			t.Errorf("%s was installed over (C6)", rows[i].name)
		}
	}
}
