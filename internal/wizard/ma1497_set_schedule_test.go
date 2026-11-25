package wizard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// setScheduler stands in for the host scheduler (installEntry; and, since the adversarial pass's
// CON-01, its answer about an entry, queryLiveEntry, and its removal, removeLiveEntry) and for the
// program a schedule runs (scheduleProgram: a permanent place, so no volatile-folder question): it
// records each entry installed, and the entries named in fail are refused. The entries installed
// are setLive, which every setScheduler of one test shares, as one computer's scheduler keeps its
// entries from one run to the next.
type setScheduler struct {
	specs []schedule.Spec
	fail  map[string]bool
}

// setLive is the stood-in scheduler's entries while a test runs (nil between tests).
var setLive map[string]bool

func (r *setScheduler) install(t *testing.T) {
	t.Helper()
	oldI, oldP, oldQ, oldR := installEntry, scheduleProgram, queryLiveEntry, removeLiveEntry
	t.Cleanup(func() { installEntry, scheduleProgram, queryLiveEntry, removeLiveEntry = oldI, oldP, oldQ, oldR })
	if setLive == nil {
		setLive = map[string]bool{}
		t.Cleanup(func() { setLive = nil })
	}
	installEntry = func(s schedule.Spec) error {
		if r.fail[s.Name] {
			return errors.New("the scheduler said no")
		}
		r.specs = append(r.specs, s)
		setLive[s.Name] = true
		return nil
	}
	queryLiveEntry = func(name string) schedule.State {
		if setLive[name] {
			return schedule.Installed
		}
		return schedule.NotInstalled
	}
	removeLiveEntry = func(s schedule.Spec) error {
		delete(setLive, s.Name)
		return nil
	}
	scheduleProgram = func(bool) (string, schedule.Reason, error) {
		return "/opt/MailArchive/mailarchive", schedule.ReasonNone, nil
	}
}

// at is each installed entry's archive and time, in order.
func (r *setScheduler) at() []string {
	var out []string
	for _, s := range r.specs {
		out = append(out, filepath.Base(s.Out)+" "+string(s.Interval)+" "+s.At)
	}
	return out
}

// answering answers the set's schedule question (either form) with row, and asks nothing else.
func answering(row string) func(string) (string, bool, error) {
	return func(text string) (string, bool, error) {
		if strings.HasPrefix(text, "Keep these archives current automatically?") {
			return row, true, nil
		}
		return "", false, nil
	}
}

// jobOf is the job file of the archive folder out: its bytes, and whether it is there.
func jobOf(t *testing.T, out string) ([]byte, bool) {
	t.Helper()
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	return b, err == nil
}

// covers: MA-1497, MA-1474, R14, S90
// The schedule once for the set (docs/design-per-mailbox-archives.md §11, revision 2's C6, C11, C27,
// revision 2.1's F-10d and F-11). Positive twin first: a fresh parent, "Yes — every day at 2:00 AM":
// the set's question is asked once, in §11's words with the wake sentence on Windows and macOS, by
// MA-1474's rule and rows; each archive made gets its own job file (its one input, auto-discovery
// off, no Thunderbird sync) and its own entry, 02:00, 02:10, 02:20, 02:30 in the plan's order; the
// leftover saved once gets none; no Thunderbird sync question is asked; the end note is §11's. An
// install that fails does not stop the others (and the times stay 10 minutes apart from the chosen
// one), an archive that did not finish gets none, and last-archive.txt names the first scheduled
// archive, not the first finished one (C22). A re-run asks in F-10d's words: the archive made anew
// gets "Yes — every week" at 03:00, and the archives brought up to date keep their job files and
// entries byte for byte, one of them with a job file that cannot be read (F-11). Only an exact Yes
// row installs: No, Cancel and ten empty answers install nothing and write no job file.
func TestTheSetIsScheduledOnceWithOnlyAnExactYes(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	fresh := func() string {
		p := filepath.Join(t.TempDir(), "Mail archives")
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	scheduled := func(s *flowScript) []string {
		var out []string
		for _, l := range s.lists {
			if strings.HasPrefix(l, "Keep these archives current automatically?") || strings.HasPrefix(l, "Keep this archive current") {
				out = append(out, l)
			}
		}
		return out
	}

	// Positive twin: a fresh parent, every day.
	parent := fresh()
	r := &setScheduler{}
	r.install(t)
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: answering(scheduleDaily)}
	s.install(t)
	made, err := makeSet(rows, "darwin")
	if err != nil || len(made) != 5 {
		t.Fatalf("the set: %+v %v", made, err)
	}
	if q := scheduled(s); len(q) != 1 || q[0] != setScheduleQuestionFor(runtime.GOOS) {
		t.Fatalf("the schedule question, once in the set's words: %q", q)
	}
	if want := []string{"ann@qz-a.example daily 02:00", "bob@qz-b.example daily 02:10", "cy@qz-c.example daily 02:20", "Local Folders daily 02:30"}; !reflect.DeepEqual(r.at(), want) {
		t.Fatalf("the entries installed: %q", r.at())
	}
	for i, m := range made {
		b, ok := jobOf(t, m.Folder)
		if i == 4 {
			if m.Schedule != ScheduleNone || ok {
				t.Errorf("the leftover saved once was scheduled: %+v", m)
			}
			continue
		}
		j, rerr := job.Read(mustJobPath(t, m.Folder))
		if m.Schedule != ScheduleInstalled || !ok || rerr != nil || j.Auto || !reflect.DeepEqual(j.Inputs, []string{rows[i].path}) || j.Out != m.Folder || j.ThunderbirdSync != nil || bytes.Contains(b, []byte("thunderbird_sync")) {
			t.Errorf("%s: %+v, its job %+v %v", m.Name, m, j, rerr)
		}
		if r.specs[i].Name != schedule.DefaultNameFor(m.Folder) {
			t.Errorf("%s's entry is not named from its own path: %q", m.Name, r.specs[i].Name)
		}
	}
	for _, l := range s.lists {
		if strings.Contains(l, "Sync Thunderbird") {
			t.Errorf("a set was asked the Thunderbird sync question: %q", l)
		}
	}
	// Amended 2026-11-25 (the friction review's F1; additive): the end note's last line says how to
	// back up a mailbox added later; it ended at setNoteRows before.
	wantNote := "Scheduled: daily from 02:00, one archive every 10 minutes, for 4 archives in " + parent + ".\n" +
		setNoteLogs + "\n" +
		"Not scheduled: imap.old.example (not in Thunderbird now), saved once.\n" +
		setNoteRows + "\n" +
		"To back up a mailbox you add to Thunderbird later: click Make a new archive…, choose All of them, then One archive for each mailbox, and this same folder, " + parent + "."
	if note := s.notes[len(s.notes)-1]; note != wantNote {
		t.Errorf("the end note:\n got %q\nwant %q", note, wantNote)
	}
	if got := lastArchive(t); got != made[0].Folder {
		t.Errorf("last-archive.txt: %q", got)
	}
	firstParent := parent

	// An install that fails, and an archive that did not finish: the others go on, 10 minutes
	// apart from the chosen time; last-archive.txt names the first one scheduled.
	parent = fresh()
	r = &setScheduler{fail: map[string]bool{schedule.DefaultNameFor(filepath.Join(parent, rows[0].name)): true}}
	r.install(t)
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: answering(scheduleDaily)}
	s.install(t)
	ch, err := chooseSet(rows, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	gone := rows[1].path
	if err := os.Rename(gone, gone+".kept"); err != nil {
		t.Fatal(err)
	}
	made, err = runSet(ch, "darwin")
	if err := os.Rename(gone+".kept", gone); err != nil {
		t.Fatal(err)
	}
	if err != nil || made[0].Schedule != ScheduleFailed || made[0].ScheduleWhy != "could not install the schedule: the scheduler said no" || made[1].Outcome != MadeFailed || made[1].Schedule != ScheduleNone {
		t.Fatalf("a failed install and a failed archive: %+v %v", made, err)
	}
	if want := []string{"cy@qz-c.example daily 02:00", "Local Folders daily 02:10"}; !reflect.DeepEqual(r.at(), want) {
		t.Errorf("after a failed install: %q", r.at())
	}
	note := s.notes[len(s.notes)-1]
	for _, w := range []string{
		"Scheduled: daily from 02:00, one archive every 10 minutes, for 2 archives in " + parent + ".\n",
		"\nNot scheduled: ann@qz-a.example (" + made[0].ScheduleWhy + ").\n",
		"\nNot scheduled: bob@qz-b.example, which did not finish.\n",
	} {
		if !strings.Contains(note, w) || !strings.Contains(made[0].ScheduleWhy, "the scheduler said no") {
			t.Errorf("the end note lacks %q:\n%s", w, note)
		}
	}
	if _, ok := jobOf(t, made[1].Folder); ok {
		t.Errorf("the archive that did not finish has a job file")
	}
	if got := lastArchive(t); got != made[2].Folder {
		t.Errorf("last-archive.txt names %q, want the first scheduled archive %q (not the first finished)", got, made[2].Folder)
	}

	// A re-run into the first parent: one archive made anew, the others keep their own schedules,
	// one of them with a job file that cannot be read.
	parent = firstParent
	folder := func(i int) string { return filepath.Join(parent, rows[i].name) }
	if err := os.RemoveAll(folder(2)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustJobPath(t, folder(1)), []byte("{not a job"), 0o600); err != nil {
		t.Fatal(err)
	}
	kept := map[int][]byte{}
	for _, i := range []int{0, 1, 3} {
		kept[i], _ = jobOf(t, folder(i))
	}
	r = &setScheduler{}
	r.install(t)
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: answering(scheduleWeekly)}
	s.install(t)
	made, err = makeSet(rows, "darwin")
	if err != nil || len(made) != 4 {
		t.Fatalf("the re-run: %+v %v", made, err)
	}
	if q := scheduled(s); len(q) != 1 || q[0] != setScheduleRerunQuestionFor(runtime.GOOS) {
		t.Fatalf("the re-run's question, in F-10d's words: %q", q)
	}
	if want := []string{"cy@qz-c.example weekly 03:00"}; !reflect.DeepEqual(r.at(), want) {
		t.Errorf("the re-run installed: %q", r.at())
	}
	for i, want := range []MadeSchedule{ScheduleKept, ScheduleKeptUnreadable, ScheduleInstalled, ScheduleKept} {
		if made[i].Schedule != want || made[i].Outcome != MadeFinished {
			t.Errorf("made[%d] = %+v, want schedule %v", i, made[i], want)
		}
	}
	for i, b := range kept {
		if now, _ := jobOf(t, folder(i)); !bytes.Equal(now, b) {
			t.Errorf("%s's own job file was changed", rows[i].name)
		}
	}
	wantNote = "Scheduled: weekly on Sunday at 03:00, for 1 archive in " + parent + ".\n" +
		"ann@qz-a.example kept its own schedule.\n" +
		"bob@qz-b.example kept its own schedule (its settings could not be read).\n" +
		"Local Folders kept its own schedule.\n" +
		setNoteLogs + "\n" + setNoteRows + "\n" +
		fmt.Sprintf(setLaterMailbox, parent)
	// The second parent's archives, made above, are known now and back up these mailboxes too: their
	// lines (MA-1501) sit before the Desktop's line; the rest is §11's note.
	var rest, others []string
	for _, l := range strings.Split(s.notes[len(s.notes)-1], "\n") {
		if strings.Contains(l, " also backs up ") {
			others = append(others, l)
			continue
		}
		rest = append(rest, l)
	}
	if note := strings.Join(rest, "\n"); note != wantNote || len(others) == 0 {
		t.Errorf("the re-run's end note:\n got %q (and %q)\nwant %q", note, others, wantNote)
	}
	if got := lastArchive(t); got != folder(0) {
		t.Errorf("last-archive.txt after the re-run: %q", got)
	}

	// Only an exact Yes row installs: No, Cancel, and ten empty answers install nothing.
	cancel := func(text string) (string, bool, error) {
		if strings.HasPrefix(text, "Keep these archives current automatically?") {
			return "", true, zenity.ErrCanceled
		}
		return "", false, nil
	}
	for name, ans := range map[string]func(string) (string, bool, error){
		"No":     answering(scheduleNo),
		"Cancel": cancel,
		"empty":  answering(""),
	} {
		parent := fresh()
		r := &setScheduler{}
		r.install(t)
		s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled, answer: ans}
		s.install(t)
		made, err := makeSet(rows, "darwin")
		asked := len(scheduled(s))
		code, msg := 0, ""
		if err == nil && len(made) == 5 && asked >= 1 {
			code, msg = 1, strings.Join(s.notes, "\n")
		}
		names := assure.Names("")
		if name == "empty" {
			names = assure.Names(noScheduleLine)
			if asked != pickTries {
				t.Errorf("ten empty answers: asked %d times", asked)
			}
		}
		assure.Refused(t, code, msg, assure.Code(1), names, assure.Forbid("Scheduled:"), assure.NoSideEffect(func() bool {
			for _, m := range made {
				if _, ok := jobOf(t, m.Folder); ok || m.Schedule != ScheduleNone {
					return false
				}
			}
			return len(r.specs) == 0
		}))
	}

	// The question's own calls: through the seam, No preselected, an empty pick refused.
	src, err := os.ReadFile("setschedule.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"setScheduleRerunQuestionFor", "setScheduleQuestionFor"} {
		if !strings.Contains(string(src), "listDialog("+q+"(runtime.GOOS), rows, zenity.Title(appTitle), zenity.DefaultItems(scheduleNo), zenity.DisallowEmpty())") {
			t.Errorf("%s is no longer asked through the seam with No preselected and DisallowEmpty", q)
		}
	}
}

// covers: MA-1497, MA-946
// The set's question in §11's words on each system: the one-archive question's missed-time sentence
// (MA-946), then, on Windows and macOS, that after a night asleep the backups may all start together
// (C27); F-10d's re-run words in place of "so they do not all run at once". The times start 10
// minutes apart, around the clock.
func TestTheSetsScheduleQuestionSaysWhenTheyRun(t *testing.T) {
	head := "Keep these archives current automatically?\n\nEach archive gets its own scheduled backup. They start 10 minutes apart, from the time you choose, so they do not all run at once. A scheduled run repeats exactly this export (incremental: only new mail and any content that was still missing). It runs only while you are signed in. "
	for goos, want := range map[string]string{
		"windows": head + "A time when the computer is switched off is skipped. If it was asleep, the backup runs when it wakes up. After a night the computer was asleep, they may all start together when it wakes.",
		"darwin":  head + "A time when the computer is switched off is skipped. If it was asleep, the backup runs when it wakes up. After a night the computer was asleep, they may all start together when it wakes.",
		"linux":   head + "A time when the computer is off or asleep is skipped.",
	} {
		if got := setScheduleQuestionFor(goos); got != want {
			t.Errorf("%s:\n got %q\nwant %q", goos, got, want)
		}
		rerun := setScheduleRerunQuestionFor(goos)
		if !strings.Contains(rerun, "New archives start 10 minutes apart from the time you choose. A scheduled run") || strings.Contains(rerun, "do not all run at once") {
			t.Errorf("%s's re-run words: %q", goos, rerun)
		}
		if !strings.HasSuffix(scheduleQuestionFor(goos), scheduleMissedFor(goos)) {
			t.Errorf("%s: the one-archive question no longer ends with its missed-time sentence", goos)
		}
	}
	for _, c := range []struct {
		at   string
		slot int
		want string
	}{{"02:00", 0, "02:00"}, {"02:00", 3, "02:30"}, {"23:50", 2, "00:10"}, {"03:00", 7, "04:10"}} {
		if got := setStartAt(c.at, c.slot); got != c.want {
			t.Errorf("setStartAt(%s, %d) = %s, want %s", c.at, c.slot, got, c.want)
		}
	}
}

// mustJobPath is the job file's path for the archive folder out.
func mustJobPath(t *testing.T, out string) string {
	t.Helper()
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
