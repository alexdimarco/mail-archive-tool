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
// MailArchive Desktop does not start when Windows will not create its private start file (the
// instance file, which holds the hand-off key) with the label that keeps sandboxed programs from
// reading it (MA-1427: a folder on a file system that keeps no labels). It then says so in plain
// words, naming the folder, the system's own words and what the folder must be (the 1.7.2
// friction review's F11), and the system's error stays in the chain. Positive twin first: any
// other failure to write the file says what it always said.
func TestADesktopThatCannotProtectItsStartFileSaysSo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "MailArchiveDesktop")
	other := errors.New("disk full")
	if got := instanceStartError(fmt.Errorf("temp file in %s: %w", dir, other)); got.Error() != "could not record this instance: temp file in "+dir+": disk full" || !errors.Is(got, other) {
		t.Fatalf("positive twin: another failure says %q", got)
	}
	cause := errors.New("The parameter is incorrect.")
	refused := fmt.Errorf("temp file in %s: %w", dir, &os.PathError{Op: "createsecret", Path: filepath.Join(dir, ".tmp-0123456789abcdef"), Err: cause})
	got := instanceStartError(refused)
	want := "MailArchive Desktop could not start: Windows would not protect its private start file in " + dir + " (The parameter is incorrect.). That folder must be on this computer's own drive."
	rc := 0
	if errors.Is(got, cause) {
		rc = 2
	}
	assure.Refused(t, rc, got.Error(), assure.Names(want), assure.Forbid("createsecret", "temp file in", "panic", "goroutine"))
}
