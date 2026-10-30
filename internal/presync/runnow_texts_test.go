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
			says := strings.Contains(got, "a backup you start with Run now does not wait for that.")
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
