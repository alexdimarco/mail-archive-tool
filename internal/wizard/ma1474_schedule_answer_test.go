package wizard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1474, R14
// "Keep this archive current automatically?" installs a schedule only for the daily or the weekly
// row, matched by the mailbox list's one row rule (rowChoice, which pickChoice calls too;
// docs/design-1.7.5-fixes.md §2). Positive twins first: the daily row gives a daily schedule at
// 02:00 and the weekly row a weekly one at 03:00. An empty answer (Linux's list allows OK with
// nothing chosen), and any text that is not a row, show MA-1465's note and the question again;
// ten of them end with the closing line and no schedule; Cancel and "No — just this once" install
// nothing. A mangled answer, on Linux and the BSDs, is read as its own row ("Yes ? every day at
// 2:00 AM" daily, "Yes ? every week, Sunday at 3:00 AM" weekly, "No ? just this once" nothing);
// on Windows and macOS, where answers are not transcoded, it is not a row and shows the note.
// Through offerSchedule with the scheduler and the dialogs stood in: no entry and no job file.
func TestOnlyTheDailyOrWeeklyRowInstallsASchedule(t *testing.T) {
	const no, daily, weekly = "No — just this once", "Yes — every day at 2:00 AM", "Yes — every week, Sunday at 3:00 AM"
	ask := func(t *testing.T, goos string, answers ...string) (schedule.Interval, string, bool, *pickScript, error) {
		t.Helper()
		s := &pickScript{answers: answers}
		s.install(t)
		iv, at, ok, err := askSchedule(goos)
		return iv, at, ok, s, err
	}
	for _, goos := range []string{"darwin", "windows", "linux"} {
		if iv, at, ok, s, err := ask(t, goos, daily); err != nil || !ok || iv != schedule.Daily || at != "02:00" || len(s.notes) != 0 {
			t.Fatalf("positive twin (%s): the daily row: %v %q %v %v, notes %q", goos, iv, at, ok, err, s.notes)
		}
		if iv, at, ok, _, err := ask(t, goos, weekly); err != nil || !ok || iv != schedule.Weekly || at != "03:00" {
			t.Fatalf("positive twin (%s): the weekly row: %v %q %v %v", goos, iv, at, ok, err)
		}
		if _, _, ok, s, err := ask(t, goos, no); err != nil || ok || len(s.notes) != 0 {
			t.Errorf("%s: No installs nothing: %v %v", goos, ok, err)
		}
		// An empty answer, and a text that is not a row: the note, then the question again.
		for _, odd := range []string{"", "Yes", "/etc/passwd", "Yes — every hour"} {
			iv, _, ok, s, err := ask(t, goos, odd, daily)
			if err != nil || !ok || iv != schedule.Daily || len(s.notes) != 1 || s.notes[0] != pickAgain || len(s.lists) != 2 {
				t.Errorf("%s: %q must show the note and ask again: %v %v %v, notes %q, lists %d", goos, odd, iv, ok, err, s.notes, len(s.lists))
			}
		}
		// The question is asked with its rows, No first and preselected, an empty pick refused.
		_, _, _, s, _ := ask(t, goos, no)
		if len(s.lists) != 1 || s.lists[0].text != scheduleQuestionFor(runtime.GOOS) || strings.Join(s.lists[0].items, "|") != no+"|"+daily+"|"+weekly {
			t.Errorf("%s: the question: %+v", goos, s.lists)
		}
	}
	// Mangled answers: their own rows on Linux and the BSDs, never a row elsewhere.
	for _, c := range []struct {
		answer string
		iv     schedule.Interval
		ok     bool
	}{{"Yes ? every day at 2:00 AM", schedule.Daily, true}, {"Yes ? every week, Sunday at 3:00 AM", schedule.Weekly, true}, {"No ? just this once", "", false}} {
		iv, _, ok, s, err := ask(t, "linux", c.answer)
		if err != nil || ok != c.ok || iv != c.iv || len(s.notes) != 0 {
			t.Errorf("linux: %q must be its own row: %v %v %v, notes %q", c.answer, iv, ok, err, s.notes)
		}
		for _, goos := range []string{"darwin", "windows"} {
			_, _, _, s, _ := ask(t, goos, c.answer, no)
			if len(s.notes) != 1 || s.notes[0] != pickAgain {
				t.Errorf("%s: %q is not a row there, so the note: %q", goos, c.answer, s.notes)
			}
		}
	}
	// Cancel ends with nothing.
	s := &pickScript{endErr: zenity.ErrCanceled}
	s.install(t)
	if _, _, ok, err := askSchedule("linux"); ok || err != nil {
		t.Errorf("Cancel installs nothing: %v %v", ok, err)
	}
	// A list that keeps answering nothing: ten questions, nine notes, then the closing line.
	_, _, ok, s, err := ask(t, "linux")
	if ok || err != nil || len(s.lists) != pickTries || len(s.notes) != pickTries || s.notes[pickTries-1] != noScheduleLine {
		t.Fatalf("ten empty answers: %v %v, lists %d, notes %q", ok, err, len(s.lists), s.notes)
	}

	// Through offerSchedule: the cases that install nothing leave no scheduler entry and no job file.
	// A settings folder of this test's own, so a job file written by any other test is not seen.
	home := t.TempDir()
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "AppData", "LOCALAPPDATA"} {
		t.Setenv(v, home)
	}
	installed := countInstalls(t)
	out := t.TempDir()
	jobPath, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	for name, answers := range map[string][]string{"ten empty answers": nil, "No": {no}} {
		s := &pickScript{answers: answers}
		s.install(t)
		err := offerSchedule(wizardChoice{out: out, auto: true})
		code, msg := 0, strings.Join(s.notes, "\n")
		if err == nil && len(s.lists) > 0 {
			code = 1
		}
		names := assure.Names("Keep this archive current")
		if name == "ten empty answers" {
			names = assure.Names(noScheduleLine)
			msg = strings.Join(s.notes, "\n")
		} else {
			msg = s.lists[0].text
		}
		assure.Refused(t, code, msg, assure.Code(1), names, assure.NoSideEffect(func() bool {
			_, serr := os.Stat(jobPath)
			jobs, _ := filepath.Glob(filepath.Join(filepath.Dir(jobPath), "*.json"))
			return len(*installed) == 0 && os.IsNotExist(serr) && len(jobs) == 0
		}))
	}
	if !strings.Contains(noScheduleLine, "No scheduled backup was set up") || !strings.Contains(noScheduleLine, "open Settings in MailArchive Desktop, or run the wizard again") {
		t.Errorf("the closing line: %q", noScheduleLine)
	}
	// The question's own call: through the seam, No preselected, an empty pick refused where the
	// dialog can (Windows, macOS), as the mailbox list's (MA-1465).
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "listDialog(scheduleQuestionFor(runtime.GOOS), rows, zenity.Title(appTitle), zenity.DefaultItems(scheduleNo), zenity.DisallowEmpty())") {
		t.Error("askSchedule no longer asks through the seam with No preselected and DisallowEmpty")
	}
}
