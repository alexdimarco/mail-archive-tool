package wizard

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-946, R14
// What happens to a scheduled time the computer missed, as the wizard says it (truth and
// friction reviews of the "How to use MailArchive" guide, 2026-10-31). A Mac's backup is a
// LaunchAgent with StartCalendarInterval, which launchd runs on waking when the Mac slept
// through its time (launchd.plist(5)); a Windows task is written with StartWhenAvailable
// (schedule/xml.go), so a time slept through runs on waking too. Only a switched-off
// computer skips it there. Linux's cron skips a time the computer slept through, so its
// words keep "off or asleep". Positive twin first: the question keeps its own lead and the
// signed-in rule on every platform, and Windows's end note keeps "a night the PC is off is
// skipped".
func TestScheduleWordsSayWhatSleepDoes(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		q := scheduleQuestionFor(goos)
		assure.Reached(t, q, goos+": the schedule question")
		if !strings.HasPrefix(q, "Keep this archive current automatically?\n\nA scheduled run repeats exactly this export (incremental: only new mail and any content that was still missing). It runs only while you are signed in.") {
			t.Errorf("%s: the question's lead changed:\n%s", goos, q)
		}
	}
	if n := scheduleRunsNoteFor("windows"); !strings.Contains(n, "a night the PC is off is skipped and shown as such.") {
		t.Errorf("the positive twin: Windows's end note changed: %q", n)
	}

	const wake = "A time when the computer is switched off is skipped. If it was asleep, the backup runs when it wakes up."
	for _, goos := range []string{"darwin", "windows"} {
		if q := scheduleQuestionFor(goos); !strings.Contains(q, wake) || strings.Contains(q, "asleep is skipped") {
			t.Errorf("%s: the question does not say a time slept through runs on waking:\n%s", goos, q)
		}
	}
	if q := scheduleQuestionFor("linux"); !strings.Contains(q, "A time when the computer is off or asleep is skipped.") || strings.Contains(q, "wakes up") {
		t.Errorf("linux: the question does not say cron skips a time slept through:\n%s", q)
	}
	mac := scheduleRunsNoteFor("darwin")
	if want := "The backup runs only while you are signed in to this Mac. A night it is switched off is skipped; if it was asleep, the backup runs when it wakes up."; !strings.Contains(mac, want) || strings.Contains(mac, "asleep is skipped") {
		t.Errorf("the Mac end note is %q, want it to say %q", mac, want)
	}
	if n := scheduleRunsNoteFor("windows"); strings.Contains(n, "asleep is skipped") {
		t.Errorf("Windows's end note says a time slept through is skipped: %q", n)
	}
	if n := scheduleRunsNoteFor("linux"); n != "" {
		t.Errorf("linux's end note is %q, want none (as before)", n)
	}

	// Structural: offerSchedule's dialogs (zenity, not driven in a test) show these words.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, wired := range []string{"zenity.List(scheduleQuestionFor(runtime.GOOS),", "note := scheduleRunsNoteFor(runtime.GOOS)"} {
		if !strings.Contains(string(src), wired) {
			t.Errorf("offerSchedule does not show the words: %q is gone from wizard.go", wired)
		}
	}
}
