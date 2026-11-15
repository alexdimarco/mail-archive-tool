package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/util"
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
	// The bare system words, and (the last check's words-1) the plain write's own error as it
	// comes: the dialog shows only the system's words inside it, not the write's "temp file in …"
	// and its temp file's name.
	realPlain, realWords := ma1451PlainWriteError(t)
	for _, c := range []struct {
		plain error
		words string
	}{{errors.New("Access is denied."), "Access is denied"}, {realPlain, realWords}} {
		rec.refusals = nil
		got := instanceStartError(env, &instance.KeyFileError{Dir: dir, Protected: errors.New("The parameter is incorrect."), Plain: c.plain})
		want := "MailArchive Desktop cannot start: it could not save its private key file in " + dir + " (" + c.words + "). Do this: start MailArchive Desktop from the Start menu. If this message comes back, your Windows account cannot write to that folder: restart the computer and try again, and if it still comes back, ask whoever looks after this computer to check that folder."
		rc := 0
		if errors.Is(got, c.plain) && RefusalShown(got) {
			rc = 2
		}
		assure.Refused(t, rc, got.Error(), assure.Names(want), assure.Forbid("would not protect", "createsecret", "denied.)", "temp file in", ".tmp-", "panic", "goroutine"))
		if len(rec.refusals) != 1 || rec.refusals[0] != want {
			t.Fatalf("the refusal was shown as %q, want one dialog with %q", rec.refusals, want)
		}
	}
}

// ma1451PlainWriteError is the error of the plain write of a private key file into a folder the
// account cannot write, and the system's words the dialog should show for it (the last check's
// words-1). As an ordinary user on Unix it is the real util.WriteFileAtomic0600 into a 0500
// folder; elsewhere (Windows, where a folder's rights are not a mode, or root, who may write
// anyway) it is the chain that write returns, with Windows' words.
func ma1451PlainWriteError(t *testing.T) (error, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "readonly")
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		inner := &os.PathError{Op: "open", Path: filepath.Join(dir, ".tmp-0123456789"), Err: errors.New("Access is denied.")}
		return fmt.Errorf("temp file in %s: %w", dir, inner), "Access is denied"
	}
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	err := util.WriteFileAtomic0600(filepath.Join(dir, instance.FileName), []byte("{}\n"))
	if err == nil {
		t.Fatal("the plain write into a 0500 folder worked")
	}
	return err, syscall.EACCES.Error()
}
