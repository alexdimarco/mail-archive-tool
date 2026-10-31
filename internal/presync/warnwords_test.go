package presync

import (
	"strings"
	"testing"
)

// covers: MA-939, MA-567
// The sync's run warnings and notices say only what is true (the wording pass before
// 1.5.2). A command-line run is never attended, so its start check skips the sync while
// the keyboard or mouse was used in the last 2 minutes (session S4, defIdleAway): telling
// someone to "run `mailarchive -thunderbird-sync` by hand to check it starts" can check
// nothing, so no warning says it; a kickstart from Terminal is skipped the same way, so
// the macOS permission warning points at System Settings instead. Exited-early and
// successor-found are tagged as what happened (Thunderbird did start). Every warning
// calls it "the Thunderbird sync", never "pre-sync" or "settled". Positive twin first:
// each warning still says what happened, that the archive ran, and what to do.
func TestTheSyncWarningsSayOnlyWhatIsTrue(t *testing.T) {
	want := map[Outcome]string{
		OutcomeCannotStart:      "Thunderbird could not be started, so the archive ran without a fresh sync. Do this: open Thunderbird yourself to check that it starts; mail that arrived since the last sync may not be in this archive yet.",
		OutcomeDidNotStart:      "Thunderbird did not appear within 90 seconds of being started, so the archive ran without a confirmed sync. Do this: open Thunderbird yourself to check that it starts.",
		OutcomeNotQuiet:         "Thunderbird's mail files were still changing at the maximum wait (20 minutes), so the archive ran with the mail that had arrived by then. Some mail may still have been arriving. Do this: the next backup will pick it up; to archive it now, open Thunderbird, let its mail arrive, then run the backup again.",
		OutcomeExitedEarly:      "Thunderbird closed before its mail stopped arriving, so the sync may be incomplete. The archive ran anyway. Do this: the next backup will pick up this mail; to archive it now, open Thunderbird, let its mail arrive, then run the backup again.",
		OutcomeQuitNotPermitted: "Thunderbird was left open because macOS has not allowed this program to close it. Do this: open System Settings > Privacy & Security > Automation and allow the entry listed there to control Thunderbird. The archive ran anyway.",
		OutcomeStateUnreadable:  "The Thunderbird sync could not read its notes from the last run, so it started fresh. The archive ran anyway.",
		OutcomeInternalError:    "The Thunderbird sync failed unexpectedly; the archive ran anyway.",
	}
	// Positive twin: the warnings this pass leaves alone keep their what, why and do-this.
	if msg, ok := WarnText(OutcomeQuitTimeout); !ok || !strings.Contains(msg, "Do this:") || !strings.Contains(msg, "The archive ran anyway.") {
		t.Fatalf("positive twin: the quit-timeout warning: %q", msg)
	}
	for o, w := range want {
		if msg, ok := WarnText(o); !ok || msg != w {
			t.Errorf("%s: %q\n  want %q", o.Name, msg, w)
		}
	}
	for _, o := range Outcomes() {
		msg, ok := WarnText(o)
		if !ok {
			continue
		}
		for _, bad := range []string{"-thunderbird-sync` by hand", "launchctl kickstart", "pre-sync", "went quiet", "next scheduled run"} {
			if strings.Contains(msg, bad) {
				t.Errorf("%s: the warning says %q: %q", o.Name, bad, msg)
			}
		}
	}
	if got := SummaryLine(OutcomeOK); got != "Thunderbird sync: done — Thunderbird was started, its mail stopped arriving, and it was closed. Archiving now." {
		t.Errorf("the done line: %q", got)
	}
	for name, phrase := range map[string]string{
		OutcomeExitedEarly.Name:    "Thunderbird not synced: it closed before its mail stopped arriving",
		OutcomeSuccessorFound.Name: "Thunderbird not synced: the copy that was started went away",
	} {
		if got := TagPhrase(name); got != phrase {
			t.Errorf("%s: tagged %q, want %q", name, got, phrase)
		}
	}
}
