package presync

import (
	"strings"
	"testing"
)

// covers: MA-561
// Every WARN-class outcome has a what/why/do-this message (the text surfaced on
// the run record and in the health view); ok and note classes surface nothing.
// The consent message names the Automation remedy.
func TestWarnText(t *testing.T) {
	for _, o := range allOutcomes {
		msg, warn := WarnText(o)
		if o.Class == ClassWarn {
			if !warn || strings.TrimSpace(msg) == "" {
				t.Errorf("%s (WARN) has no message: warn=%v msg=%q", o.Name, warn, msg)
			}
			if !strings.Contains(msg, "archive ran") && !strings.Contains(msg, "no sync ran") && !strings.Contains(msg, "without") {
				t.Errorf("%s message should reassure the archive still ran: %q", o.Name, msg)
			}
		} else {
			if warn || msg != "" {
				t.Errorf("%s (%s) should surface no warning, got warn=%v msg=%q", o.Name, o.Class, warn, msg)
			}
		}
	}
	if msg, _ := WarnText(OutcomeQuitNotPermitted); !strings.Contains(msg, "Automation") {
		t.Errorf("quit-not-permitted message should name the Automation remedy: %q", msg)
	}
}

// covers: MA-561
// SummaryLine gives a one-line owner-facing result for Config.Log: a clean
// finish, a note's reason, or the warning text; nothing for a cancelled run.
func TestSummaryLine(t *testing.T) {
	if s := SummaryLine(OutcomeOK); !strings.Contains(s, "done") {
		t.Errorf("ok summary = %q, want a 'done' line", s)
	}
	if s := SummaryLine(OutcomeAlreadyRunning); !strings.Contains(s, "already open") {
		t.Errorf("already-running summary = %q", s)
	}
	if s := SummaryLine(OutcomeQuitNotPermitted); !strings.Contains(s, "Automation") {
		t.Errorf("warn summary = %q, want the warning text", s)
	}
	if s := SummaryLine(OutcomeCancelled); s != "" {
		t.Errorf("cancelled summary = %q, want empty (the run is aborting)", s)
	}
}

// covers: MA-567
// SyncNotice carries every X16 disclosure (docs/ux-contract.md X16) so the single
// shared paragraph the wizard, the Desktop card, the CLI note and the README all
// draw from can never omit one. It names the start-side effects (accounts, Outbox,
// filters, notifications) and the quit-side on-exit actions (emptying Trash), says
// an already-open or in-use Thunderbird is left alone, limits itself to one
// profile, and never claims the sync means a complete download.
func TestSyncNoticeDisclosures(t *testing.T) {
	for _, want := range []string{
		"Outbox",        // starting Thunderbird may send queued mail
		"filters",       // it runs message filters
		"notifications", // it may show notifications
		"on-exit",       // quitting runs the accounts' on-exit actions...
		"Trash",         // ...for example emptying Trash
		"left alone",    // an already-open / in-use Thunderbird is never touched
		"profile",       // only one profile is synced per schedule
		"stop arriving", // the non-over-promising wording for the wait
	} {
		if !strings.Contains(SyncNotice, want) {
			t.Errorf("SyncNotice must disclose %q (X16); not found in: %q", want, SyncNotice)
		}
	}
	for _, forbid := range []string{
		"finish downloading", // over-promises a complete download
		"complete download",
	} {
		if strings.Contains(SyncNotice, forbid) {
			t.Errorf("SyncNotice must not claim %q (X16: a sync is never a complete download)", forbid)
		}
	}
}
