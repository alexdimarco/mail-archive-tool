package desktop

import (
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

// covers: MA-1424, MA-1427, R52
// An open choice window is busy for `quit` (the S1 fix round 3, C5): the person is in the
// middle of choosing, and the upgrade's quit would take the window away under their click.
// In-process over the real lock, listeners and hand-off; the choice is a dialog seam
// (MB-18). Positive twin first: a copy with nothing open (started with -background, so no
// choice shows) would quit (-check, exit 0) and keeps running. A copy whose choice is on
// the screen is asked -check and quit: both "busy", exit 3, naming Shut down, and it keeps
// running with its choice still showing. Once the person dismisses the choice, the copy
// ends on its own as MB-16 says (nothing ran, nothing was opened).
func TestMA1424_AnOpenChoiceIsBusyForQuit(t *testing.T) {
	s := installFrontSeams(t)

	idle := newDRig(t)
	idle.startInstance(idle.env(idle.rec), "-background")
	code, words := quitRun(t, idle, "-check")
	if code != QuitDone || !instance.LockIsHeld(idle.dir, "") {
		t.Fatalf("twin: -check on a copy with nothing open: %d %q, still running %v", code, words, instance.LockIsHeld(idle.dir, ""))
	}

	hold := make(chan struct{})
	s.mu.Lock()
	s.choiceHold = hold
	s.mu.Unlock()
	choosing := newDRig(t)
	choosing.startInstance(choosing.env(choosing.rec))
	waitUntil(t, "the first choice", func() bool { v, _, _, _ := s.snap(); return len(v) == 1 })
	stillChoosing := func() bool {
		select {
		case <-choosing.done:
			return false
		default:
		}
		v, _, _, _ := s.snap()
		return len(v) == 1 && instance.LockIsHeld(choosing.dir, "")
	}
	for _, args := range [][]string{{"-check"}, {"-wait", "2s"}} {
		code, words := quitRun(t, choosing, args...)
		assure.Refused(t, code, words, assure.Code(QuitBusy), assure.Names("is busy", "choice window", "keeps running", "Shut down"),
			assure.NoSideEffect(stillChoosing))
	}

	close(hold)
	select {
	case <-choosing.done:
		choosing.cancel = nil
	case <-time.After(15 * time.Second):
		t.Error("the dismissed first choice, with nothing run, did not end the copy (MB-16)")
	}
}
