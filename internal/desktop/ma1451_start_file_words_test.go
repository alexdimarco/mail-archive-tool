package desktop

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

// covers: MA-1451, R23
// MailArchive Desktop that cannot save its private key file at all (the protected write failed
// and writing it as 1.7.1 did failed too: MA-1453) says so in plain words with what to do, as a
// refusal the person sees: a dialog as well as stderr, since the Start menu's program has no
// console (the 1.7.2 friction review's F11, its re-walk's G8). Since the final check's wording-1
// the words name the error of the write that failed last, without Windows' own full stop, and say
// the account cannot write to the folder, not that Windows would not protect the file (a label
// Windows refuses alone never reaches this: the Desktop starts). The errors stay in the chain.
// Positive twin first: any other failure to write the file says what it always said and shows no
// dialog.
func TestADesktopThatCannotProtectItsStartFileSaysSo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "MailArchiveDesktop")
	rec := &recorder{}
	env := &launchEnv{ShowRefusal: rec.refuse}
	other := errors.New("disk full")
	if got := instanceStartError(env, fmt.Errorf("temp file in %s: %w", dir, other)); got.Error() != "could not record this instance: temp file in "+dir+": disk full" || !errors.Is(got, other) || len(rec.refusals) != 0 {
		t.Fatalf("positive twin: another failure says %q (dialogs %q)", got, rec.refusals)
	}
	plain := errors.New("Access is denied.")
	got := instanceStartError(env, &instance.KeyFileError{Dir: dir, Protected: errors.New("The parameter is incorrect."), Plain: plain})
	want := "MailArchive Desktop cannot start: it could not save its private key file in " + dir + " (Access is denied). Do this: start MailArchive Desktop from the Start menu. If this message comes back, your Windows account cannot write to that folder: restart the computer and try again, and if it still comes back, ask whoever looks after this computer to check that folder."
	rc := 0
	if errors.Is(got, plain) && RefusalShown(got) {
		rc = 2
	}
	assure.Refused(t, rc, got.Error(), assure.Names(want), assure.Forbid("would not protect", "createsecret", "denied.)", "panic", "goroutine"))
	if len(rec.refusals) != 1 || rec.refusals[0] != want {
		t.Fatalf("the refusal was shown as %q, want one dialog with %q", rec.refusals, want)
	}
}
