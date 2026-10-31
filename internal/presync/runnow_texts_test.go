package presync

import (
	"strings"
	"testing"
)

// covers: MA-915, MA-567
// The shared texts corrected in slice AS4 (design-presync-attended §7, C-31, C-52,
// C-62, C-84). SyncNotice keeps every X16 disclosure and no longer says a
// Thunderbird "you are using" is left alone; it says a scheduled backup waits until
// the person has been away. SyncNoticeFor adds that a backup started with Run now
// does not wait — only where the platform's attended start is allowed — and is
// otherwise SyncNotice itself. The Mac note never names Run now while macOS's
// attended close is not allowed, names no asking program before lab L-A2, and still
// says what macOS asks and where to answer it.
func TestRunNowSharedTexts(t *testing.T) {
	for _, want := range []string{"Outbox", "filters", "notifications", "on-exit", "Trash", "left alone", "profile", "stop arriving",
		"A Thunderbird you already have open is left alone — never started or closed.",
		"A scheduled backup also waits until you have been away from the computer"} {
		if !strings.Contains(SyncNotice, want) {
			t.Errorf("SyncNotice lacks %q", want)
		}
	}
	if strings.Contains(SyncNotice, "one you are using") || strings.Contains(SyncNotice, "Run now") {
		t.Errorf("SyncNotice still says a Thunderbird in use is left alone, or names Run now everywhere: %q", SyncNotice)
	}
	if SyncNoticeFor("linux") != SyncNotice {
		t.Errorf("Linux, where the sync never runs, shows more than SyncNotice: %q", SyncNoticeFor("linux"))
	}
	for _, goos := range []string{"darwin", "windows"} {
		for _, allowed := range []bool{true, false} {
			r := OverrideTablesForTest(nil, map[string]bool{goos: allowed})
			got := SyncNoticeFor(goos)
			r()
			says := strings.Contains(got, "a backup you start with Run now does not wait for that, and leaves Thunderbird open for you to quit.")
			if want := allowed; says != want {
				t.Errorf("%s, start allowed %v: the Run now clause shown %v, want %v: %q", goos, allowed, says, want, got)
			}
			if !says && got != SyncNotice {
				t.Errorf("%s, start allowed %v: not SyncNotice itself: %q", goos, allowed, got)
			}
			for _, want := range []string{"on-exit", "Trash", "left alone", "profile", "Outbox"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s: SyncNoticeFor dropped the disclosure %q", goos, want)
				}
			}
		}
	}

	if !AttendedQuitAllowed("darwin") && strings.Contains(MacAutomationNote, "Run now") || strings.Contains(MacAutomationNote, "Run-now") {
		t.Errorf("the Mac note names Run now while macOS's attended close is not allowed: %q", MacAutomationNote)
	}
	for _, forbid := range []string{"Mail Archive may", "allow Mail Archive", "Mail Archive to control"} {
		if strings.Contains(MacAutomationNote, forbid) {
			t.Errorf("the Mac note names the asking program before lab L-A2 (%q): %q", forbid, MacAutomationNote)
		}
	}
	for _, want := range []string{"scheduled backup closes Thunderbird", "macOS may ask for permission", "Privacy & Security > Automation", "leaves Thunderbird open"} {
		if !strings.Contains(MacAutomationNote, want) {
			t.Errorf("the Mac note lacks %q: %q", want, MacAutomationNote)
		}
	}
}

// covers: MA-915, MA-567
// The run warnings a Run now can now record (friction review of design-presync-attended,
// §14 gate 3): presyncjob saves WarnText's words for every session, attended ones
// included, and Status and the Desktop health view show them as written. So no warning
// a never-close Run now can end with may say Thunderbird was asked to quit, and the
// warning for a Thunderbird an earlier backup left open — which a cancelled or locked
// Run now can leave — names no "scheduled" sync and says the Thunderbird to quit may be
// hidden or minimized, not "switch to its window" (it was started out of sight); it
// stays platform-neutral, since it is shown as written on every platform
// (TestWarnTextActionable). Positive twin first: each still says what happened, that
// the archive ran, and what to do.
func TestWarnTextTrueForARunNow(t *testing.T) {
	notQuiet, ok := WarnText(OutcomeNotQuiet)
	if !ok || !strings.Contains(notQuiet, "still changing at the maximum wait (20 minutes)") || !strings.Contains(notQuiet, "Do this:") {
		t.Fatalf("twin: the not-quiet warning: %q", notQuiet)
	}
	if strings.Contains(notQuiet, "asked to quit") || strings.Contains(notQuiet, "closed") {
		t.Errorf("the not-quiet warning says Thunderbird was asked to quit, which a Run now never does: %q", notQuiet)
	}
	leftover, ok := WarnText(OutcomeLeftoverFound)
	if !ok || !strings.Contains(leftover, "Do this:") || !strings.Contains(leftover, "The archive ran anyway.") {
		t.Fatalf("twin: the leftover warning: %q", leftover)
	}
	if strings.Contains(leftover, "scheduled") || strings.Contains(leftover, "switch to that Thunderbird window") {
		t.Errorf("the leftover warning names only a scheduled sync, or a window that is out of sight: %q", leftover)
	}
	for _, want := range []string{"an earlier backup", "quit that Thunderbird", "hidden or minimized"} {
		if !strings.Contains(leftover, want) {
			t.Errorf("the leftover warning lacks %q: %q", want, leftover)
		}
	}
}

// covers: MA-928, MA-900, MA-908
// One program name in the run warnings this branch added (the friction review's O1,
// applied by the lead 2026-10-31; X17): start-blocked, quit-no-window, state-unsaved and
// not-set-up name the program "MailArchive", and the Desktop "MailArchive Desktop", as the
// rest of the Desktop and the other alert emails do — never "Mail Archive". Positive twin
// first: each is a warning that names the program at all. The older macOS Automation
// warning, which names the entry macOS shows, is not one of them.
func TestTheNewRunWarningsNameTheProgramOneWay(t *testing.T) {
	for o, want := range map[Outcome]string{
		OutcomeStartBlocked: "if you use Run now, start MailArchive Desktop from its shortcut (not from a terminal)",
		OutcomeQuitNoWindow: "so MailArchive did not try to close it",
		OutcomeStateUnsaved: "MailArchive's settings folder can be written to",
		OutcomeNotSetUp:     "in MailArchive Desktop, click Repair on the Scheduled backup card",
	} {
		msg, ok := WarnText(o)
		if !ok || !strings.Contains(msg, want) {
			t.Errorf("%s: the warning %q (%v) lacks %q", o.Name, msg, ok, want)
		}
		if strings.Contains(msg, "Mail Archive") {
			t.Errorf("%s: the warning names the program \"Mail Archive\": %q", o.Name, msg)
		}
	}
}
