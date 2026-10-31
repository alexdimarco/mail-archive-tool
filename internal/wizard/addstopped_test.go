package wizard

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
)

// covers: MA-954
// What the Desktop's Make a new archive… hears back from the wizard's add flow says
// whether the copy finished (the friction review of the 1.5.2 rulings, 2026-10-31,
// finding 6). Positive twin first: a copy that finished returns its folder and no
// error, even when the person then closes a later window (the summary or the schedule
// question) instead of answering it. A person who backs out before choosing a folder
// adds nothing. A folder chosen but a copy that did not finish (Cancel on the progress
// window, or Cancel at the Outlook app's "Continue?") returns the folder with
// ErrStoppedEarly, never a plain success. Any other error is passed on as it was.
func TestAddArchiveSaysWhetherTheCopyFinished(t *testing.T) {
	boom := errors.New("could not install the schedule")
	for _, tc := range []struct {
		name     string
		out      string
		finished bool
		err      error
		wantOut  string
		wantErr  error
	}{
		{"finished", "/a", true, nil, "/a", nil},
		{"finished, then a later window closed", "/a", true, zenity.ErrCanceled, "/a", nil},
		{"finished, then the schedule failed", "/a", true, boom, "/a", boom},
		{"backed out before a folder", "", false, zenity.ErrCanceled, "", nil},
		{"failed before a folder", "", false, boom, "", boom},
		{"copy cancelled", "/a", false, nil, "/a", ErrStoppedEarly},
		{"copy cancelled, its message closed", "/a", false, zenity.ErrCanceled, "/a", ErrStoppedEarly},
		{"copy failed", "/a", false, boom, "/a", boom},
	} {
		out, err := addOutcome(tc.out, tc.finished, tc.err)
		if out != tc.wantOut || !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
			t.Errorf("%s: addOutcome = (%q, %v), want (%q, %v)", tc.name, out, err, tc.wantOut, tc.wantErr)
		}
	}

	// The copy's finish is marked in one place, right where the wizard notes the archive
	// as the one it last used: after a successful export, never before it or on a cancel.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	code := assure.Reached(t, string(src), "wizard.go")
	if n := strings.Count(code, "c.markFinished()"); n != 1 {
		t.Fatalf("c.markFinished() is called %d times in wizard.go, want once", n)
	}
	run := code[strings.Index(code, "func runExport(c wizardChoice) error {"):]
	run = run[:strings.Index(run, "\n}\n")]
	cancelled := strings.Index(run, "errors.Is(runErr, context.Canceled)")
	failed := strings.Index(run, "if runErr != nil {")
	marked := strings.Index(run, "rememberArchive(c.out)\n\tc.markFinished()")
	if cancelled < 0 || failed < 0 || marked < 0 || marked < cancelled || marked < failed {
		t.Errorf("runExport does not mark the copy finished right after rememberArchive, past the cancel and failure returns (cancel %d, failure %d, mark %d)", cancelled, failed, marked)
	}

	// markFinished calls the hook the add flow set, and is safe without one.
	called := false
	wizardChoice{finished: func() { called = true }}.markFinished()
	wizardChoice{}.markFinished()
	if !called {
		t.Error("markFinished did not call the add flow's hook")
	}
}
