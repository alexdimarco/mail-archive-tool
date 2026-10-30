package presyncjob

import (
	"testing"

	"mail-archive-tool/internal/presync"
)

// covers: MA-901
// B6 (A6): every side outcome a session hands back reaches the run's warnings,
// beside the main outcome's: an unreadable state record during a session that
// went on to sync (main outcome ok) becomes one presync_state_unreadable warning,
// with its what/why text. Twin: the same ok session with no side outcome adds no
// warning. A side outcome that repeats the main outcome's code is not doubled.
func TestWarningsCarrySideOutcomes(t *testing.T) {
	if w := warnings(presync.Result{Outcome: presync.OutcomeOK}); len(w) != 0 {
		t.Fatalf("twin: an ok session with no side outcome gave warnings %v", w)
	}

	w := warnings(presync.Result{Outcome: presync.OutcomeOK, Side: []presync.Outcome{presync.OutcomeStateUnreadable}})
	if len(w) != 1 || w[0].Code != presync.OutcomeStateUnreadable.Code || w[0].Message == "" {
		t.Errorf("warnings = %+v, want one presync_state_unreadable warning with its text", w)
	}

	w = warnings(presync.Result{Outcome: presync.OutcomeQuitNotPermitted, Side: []presync.Outcome{presync.OutcomeStateUnsaved}})
	if len(w) != 2 || w[0].Code != presync.OutcomeQuitNotPermitted.Code || w[1].Code != presync.OutcomeStateUnsaved.Code {
		t.Errorf("warnings = %+v, want the main warning then the side warning", w)
	}

	w = warnings(presync.Result{Outcome: presync.OutcomeStateUnsaved, Side: []presync.Outcome{presync.OutcomeStateUnsaved}})
	if len(w) != 1 {
		t.Errorf("warnings = %+v, want a repeated code given once", w)
	}
}
