package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1451, R23
// MailArchive Desktop that cannot write its private start file at all (Windows will not give it
// the label that keeps sandboxed programs from reading it, and writing it as 1.7.1 did failed too:
// MA-1453) says so in plain words with what to do, as a refusal the person sees: a dialog as well
// as stderr, since the Start menu's program has no console (the 1.7.2 friction review's F11 and
// its re-walk's G8). The system's error stays in the error's chain. Positive twin first: any other
// failure to write the file says what it always said and shows no dialog.
func TestADesktopThatCannotProtectItsStartFileSaysSo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "MailArchiveDesktop")
	rec := &recorder{}
	env := &launchEnv{ShowRefusal: rec.refuse}
	other := errors.New("disk full")
	if got := instanceStartError(env, fmt.Errorf("temp file in %s: %w", dir, other)); got.Error() != "could not record this instance: temp file in "+dir+": disk full" || !errors.Is(got, other) || len(rec.refusals) != 0 {
		t.Fatalf("positive twin: another failure says %q (dialogs %q)", got, rec.refusals)
	}
	cause := errors.New("The parameter is incorrect.")
	refused := fmt.Errorf("temp file in %s: %w; written without the label it failed too: access denied", dir, &os.PathError{Op: "createsecret", Path: filepath.Join(dir, ".tmp-0123456789abcdef"), Err: cause})
	got := instanceStartError(env, refused)
	want := "MailArchive Desktop cannot start: Windows would not protect its private start file in " + dir + " (The parameter is incorrect.). Do this: start MailArchive Desktop from the Start menu. If this message comes back, that folder is on a drive that cannot protect it, such as a network drive; it must be on this computer's own drive."
	rc := 0
	if errors.Is(got, cause) && RefusalShown(got) {
		rc = 2
	}
	assure.Refused(t, rc, got.Error(), assure.Names(want), assure.Forbid("createsecret", "temp file in", "panic", "goroutine"))
	if len(rec.refusals) != 1 || rec.refusals[0] != want {
		t.Fatalf("the refusal was shown as %q, want one dialog with %q", rec.refusals, want)
	}
}
