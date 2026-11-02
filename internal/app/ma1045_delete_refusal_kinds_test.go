package app

import (
	"errors"
	"fmt"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// ma1045LogUnreadable is a stand-in for archdelete's refusal of a deletion log it cannot read: it
// says so through LogUnreadable, as archdelete.Refusal does for its CaseLogUnreadable.
type ma1045LogUnreadable struct{ msg string }

func (e *ma1045LogUnreadable) Error() string       { return e.msg }
func (e *ma1045LogUnreadable) LogUnreadable() bool { return true }

// covers: MA-1045, R36, R37
// RefuseUnfinishedDelete names its refusal by what it is, never "archive_absent" (the G2 settle
// pass's MED-3): an encrypted archive's alert and notice say a kind's fixed sentence in place of
// the error, and archive_absent's is "the archive's drive or folder is not there — connect it",
// which told the person to connect a drive that is connected. A delete not finished is
// delete_unfinished, a deletion log that cannot be read deletion_log_unreadable, each with its own
// sentence that says what to do; a delete still running (its error matches lockfile.ErrHeld) is
// archive_busy. RefusedAsNoArchive counts the first two with archive_absent, so a scheduled run's
// callers still take back what they wrote into the folder. Positive twin first: nothing pending is
// no refusal, and an archive_absent refusal is still taken back.
func TestMA1045_AnUnfinishedDeleteRefusalIsNamedForWhatItIs(t *testing.T) {
	var answer error
	t.Cleanup(SetUnfinishedDeleteCheck(func(_, _ string) error { return answer }))
	assure.Reached(t, UnfinishedDeleteCheckInstalled(), "the stand-in check is installed")

	// Positive twins.
	if err := RefuseUnfinishedDelete("/some/folder", ""); err != nil {
		t.Fatalf("positive twin: nothing pending: %v", err)
	}
	absent := RequireExistingOut("/no/such/folder/ma1045")
	assure.Reached(t, FailureKind(absent) == state.FailArchiveAbsent && RefusedAsNoArchive(absent), "an archive_absent refusal is taken back")

	cases := []struct {
		name      string
		err       error
		kind      string
		takenBack bool
	}{
		{"a delete not finished", errors.New("Delete not finished: click Delete again."), state.FailDeleteUnfinished, true},
		{"a log that cannot be read", &ma1045LogUnreadable{"MailArchive could not read its deletion log"}, state.FailDeletionLogUnreadable, true},
		{"a delete still running", fmt.Errorf("This archive is being deleted: %w", lockfile.ErrHeld), state.FailArchiveBusy, false},
	}
	for _, c := range cases {
		answer = c.err
		err := RefuseUnfinishedDelete("/some/folder", "")
		// RefuseUnfinishedDelete only answers (it writes nothing); the engines' and entry points'
		// side effects are proven in MA-1044's tests.
		assure.Refused(t, rcOf(err), errString(err), assure.Code(1), assure.Names(c.err.Error()+" (Folder: /some/folder)"))
		if k := FailureKind(err); k != c.kind {
			t.Errorf("%s: kind %q, want %q", c.name, k, c.kind)
		}
		if RefusedAsNoArchive(err) != c.takenBack {
			t.Errorf("%s: taken back as no archive is %v, want %v", c.name, RefusedAsNoArchive(err), c.takenBack)
		}
		if t1 := state.FailureText(c.kind); t1 == state.FailureText(state.FailArchiveAbsent) || t1 == state.FailureText(state.FailRunFailed) {
			t.Errorf("%s: kind %s says another kind's sentence: %q", c.name, c.kind, t1)
		}
	}
	assure.Reached(t, len(state.FailureText(state.FailDeleteUnfinished)) > 0 &&
		state.FailureText(state.FailDeleteUnfinished) == "a delete of this archive did not finish, so it was not backed up — click Delete again on its row in MailArchive Desktop",
		"delete_unfinished's sentence")

	// With no check installed (a test of this package alone) nothing is refused, and the
	// accessor says so: each program's own test proves its check is installed.
	restore := SetUnfinishedDeleteCheck(nil)
	defer restore()
	assure.Reached(t, !UnfinishedDeleteCheckInstalled(), "no check installed reads as not installed")
}

func rcOf(err error) int {
	if err != nil {
		return 1
	}
	return 0
}
